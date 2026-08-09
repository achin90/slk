package image

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/gammons/slk/internal/debuglog"
)

// ProbeKittyGraphics sends a tiny image upload with response requested
// and waits up to timeout for the OK reply. Returns true if the
// terminal acknowledges. Used at startup to downgrade ProtoKitty when
// the terminal claims kitty support but doesn't actually deliver
// (e.g., iTerm2's limited kitty implementation, or zellij / tmux with
// allow-passthrough=off swallowing the probe escape).
//
// Inputs:
//
//	w:       terminal writer (typically os.Stdout)
//	r:       terminal reader (typically os.Stdin in raw mode)
//	timeout: how long to wait for the reply
//
// Implementation note (issue #50): the production path uses pollProbe
// (poll(2) + read(2), see probe_unix.go) so the function is fully
// synchronous and spawns no goroutine. Earlier implementations spawned
// a goroutine that kept reading from r forever after the select-on-
// timeout returned. That leaked goroutine then raced bubbletea's input
// loop for every byte the user typed, discarding ~95% of keystrokes
// (most aren't 0x1b) and making slk unresponsive whenever the probe
// timed out -- which is exactly when the user is in zellij or in tmux
// with allow-passthrough=off, because the multiplexer swallows the
// probe escape and no reply ever arrives.
//
// The poll-based path needs r to be an *os.File (any Go file with a
// real fd works: os.Stdin, os.Pipe). For non-*os.File readers
// (blockingReader in tests), this falls back to the legacy goroutine-
// based probe; that path may leak but tests exit immediately so it
// doesn't matter.
func ProbeKittyGraphics(w io.Writer, r io.Reader, timeout time.Duration) bool {
	// Minimal valid 1x1 PNG.
	const tinyPNG = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNk+P+/HgAFhAJ/wlseKgAAAABJRU5ErkJggg=="
	const probeID = 9999
	header := fmt.Sprintf("a=T,f=100,t=d,i=%d,q=0", probeID)
	if err := writeKittySequence(w, fmt.Sprintf("\x1b_G%s;%s\x1b\\", header, tinyPNG)); err != nil {
		return false
	}

	start := time.Now()

	if f, ok := r.(*os.File); ok {
		ok, bytesRead, reason := pollProbe(int(f.Fd()), timeout)
		debuglog.ImgRender("probe: ok=%v reason=%s bytes_read=%d elapsed_ms=%d",
			ok, reason, bytesRead, time.Since(start).Milliseconds())
		return ok
	}

	// Test fallback for non-*os.File readers.
	return probeViaGoroutine(r, timeout)
}

// probeViaGoroutine is the legacy goroutine-based probe. Retained for
// tests that pass a non-*os.File reader. NOT used in production. The
// goroutine spawned here is intentionally not cleaned up on timeout;
// see the doc comment on ProbeKittyGraphics for why that's acceptable
// in test-only code paths.
func probeViaGoroutine(r io.Reader, timeout time.Duration) bool {
	type result struct{ ok bool }
	ch := make(chan result, 1)
	go func() {
		br := bufio.NewReader(r)
		for {
			b, err := br.ReadByte()
			if err != nil {
				ch <- result{false}
				return
			}
			if b != 0x1b {
				continue
			}
			next, err := br.ReadByte()
			if err != nil || next != '_' {
				if err != nil {
					ch <- result{false}
					return
				}
				continue
			}
			next, err = br.ReadByte()
			if err != nil || next != 'G' {
				if err != nil {
					ch <- result{false}
					return
				}
				continue
			}
			payload, err := br.ReadString(0x1b)
			if err != nil {
				ch <- result{false}
				return
			}
			ch <- result{strings.Contains(payload, ";OK")}
			return
		}
	}()
	select {
	case res := <-ch:
		return res.ok
	case <-time.After(timeout):
		return false
	}
}

// scanForOK returns (matched, ok). matched is true if a complete kitty
// graphics response (\x1b_G ... \x1b\\) is present in buf. ok is true
// when matched is true AND the payload contains ";OK". Used by both
// the poll-based and goroutine-based probe paths.
func scanForOK(buf []byte) (matched, ok bool) {
	i := bytes.Index(buf, []byte("\x1b_G"))
	if i < 0 {
		return false, false
	}
	tail := buf[i+3:] // skip past \x1b_G
	j := bytes.Index(tail, []byte("\x1b\\"))
	if j < 0 {
		return false, false
	}
	return true, bytes.Contains(tail[:j], []byte(";OK"))
}

// ProbeKittyCellPixels queries the kitty graphics protocol for the
// terminal's window size and calculates the per-cell pixel dimensions.
// This is more reliable than TIOCGWINSZ when running inside tmux,
// where the ioctl may report pixel dimensions that don't match what
// the kitty graphics protocol actually uses for rendering.
//
// Returns (cellW, cellH, true) on success. The caller should use these
// values to override the TIOCGWINSZ-derived cell pixels. Returns
// (0, 0, false) if the terminal doesn't respond or the response can't
// be parsed.
//
// Must be called BEFORE bubbletea takes over the terminal (same window
// as ProbeKittyGraphics), with stdin in raw mode.
func ProbeKittyCellPixels(w io.Writer, r io.Reader, timeout time.Duration) (int, int, bool) {
	// Query: a=q (query), i=31 (window size), s=1 (request width), v=1 (request height)
	hdr := "a=q,i=31,s=1,v=1"
	seq := fmt.Sprintf("\x1b_G%s\x1b\\", hdr)
	if err := writeKittySequence(w, seq); err != nil {
		return 0, 0, false
	}

	start := time.Now()

	if f, ok := r.(*os.File); ok {
		cellW, cellH, ok := pollKittyWindowSize(int(f.Fd()), timeout)
		debuglog.ImgRender("cell-pixel-probe: ok=%v cellW=%d cellH=%d elapsed_ms=%d",
			ok, cellW, cellH, time.Since(start).Milliseconds())
		return cellW, cellH, ok
	}

	// Test fallback for non-*os.File readers.
	return probeCellPixelsViaGoroutine(r, timeout)
}

// pollKittyWindowSize reads from fd up to timeout, looking for a kitty
// graphics response containing the window size fields (s, v, c, r).
// Returns (cellW, cellH, true) where cellW = s/c and cellH = v/r.
func pollKittyWindowSize(fd int, timeout time.Duration) (int, int, bool) {
	deadline := time.Now().Add(timeout)
	var collected []byte
	var buf [512]byte

	for {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return 0, 0, false
		}
		ms := int(remaining / time.Millisecond)
		if ms <= 0 {
			ms = 1
		}
		fds := []unixPollFd{{Fd: int32(fd), Events: unixPollIn}}
		n, err := unixPoll(fds, ms)
		if err != nil {
			if err == unixEINTR {
				continue
			}
			return 0, 0, false
		}
		if n == 0 {
			return 0, 0, false
		}
		if fds[0].Revents&(unixPollHUP|unixPollErr|unixPollNval) != 0 && fds[0].Revents&unixPollIn == 0 {
			return 0, 0, false
		}
		rn, err := unixRead(fd, buf[:])
		if err != nil {
			if err == unixEINTR || err == unixEAGAIN {
				continue
			}
			return 0, 0, false
		}
		if rn == 0 {
			return 0, 0, false
		}
		collected = append(collected, buf[:rn]...)
		if cellW, cellH, ok := parseWindowSizeResponse(collected); ok {
			return cellW, cellH, true
		}
	}
}

// parseWindowSizeResponse extracts cellW and cellH from a kitty
// graphics protocol response containing i=31,s=...,v=...,c=...,r=...
// Returns (cellW, cellH, true) where cellW = s/c and cellH = v/r.
func parseWindowSizeResponse(buf []byte) (int, int, bool) {
	i := bytes.Index(buf, []byte("\x1b_G"))
	if i < 0 {
		return 0, 0, false
	}
	tail := buf[i+3:] // skip past \x1b_G
	j := bytes.Index(tail, []byte("\x1b\\"))
	if j < 0 {
		return 0, 0, false
	}
	payload := string(tail[:j])

	// The response contains key=value pairs separated by commas.
	// We need s (window width pixels), v (window height pixels),
	// c (columns), and r (rows).
	var s, v, c, r int
	var foundS, foundV, foundC, foundR bool
	for _, field := range strings.Split(payload, ",") {
		parts := strings.SplitN(field, "=", 2)
		if len(parts) != 2 {
			continue
		}
		val, err := strconv.Atoi(parts[1])
		if err != nil {
			continue
		}
		switch parts[0] {
		case "s":
			s = val
			foundS = true
		case "v":
			v = val
			foundV = true
		case "c":
			c = val
			foundC = true
		case "r":
			r = val
			foundR = true
		}
	}
	if !foundS || !foundV || !foundC || !foundR || c <= 0 || r <= 0 {
		return 0, 0, false
	}
	cellW := s / c
	cellH := v / r
	if cellW <= 0 || cellH <= 0 {
		return 0, 0, false
	}
	return cellW, cellH, true
}

// probeCellPixelsViaGoroutine is the test fallback for non-*os.File
// readers. NOT used in production.
func probeCellPixelsViaGoroutine(r io.Reader, timeout time.Duration) (int, int, bool) {
	type result struct {
		cellW, cellH int
		ok           bool
	}
	ch := make(chan result, 1)
	go func() {
		br := bufio.NewReader(r)
		var collected []byte
		for {
			b, err := br.ReadByte()
			if err != nil {
				ch <- result{0, 0, false}
				return
			}
			collected = append(collected, b)
			if cellW, cellH, ok := parseWindowSizeResponse(collected); ok {
				ch <- result{cellW, cellH, true}
				return
			}
		}
	}()
	select {
	case res := <-ch:
		return res.cellW, res.cellH, res.ok
	case <-time.After(timeout):
		return 0, 0, false
	}
}

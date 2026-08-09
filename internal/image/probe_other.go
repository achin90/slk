//go:build !unix

package image

import (
	"errors"
	"time"
)

// pollProbe stub for non-unix platforms (Windows). The kitty graphics
// protocol is not used in any meaningful Windows terminal, so reaching
// this stub means the env-detect was wrong; safest behavior is to
// report failure so the caller downgrades to halfblock.
//
// Returns (false, 0, "unsupported_platform") unconditionally.
func pollProbe(fd int, timeout time.Duration) (bool, int, string) {
	_ = fd
	_ = timeout
	return false, 0, "unsupported_platform"
}

// Platform-specific stubs for non-unix platforms. See probe_unix.go
// for the real implementations.
type unixPollFd struct {
	Fd      int32
	Events  int16
	Revents int16
}

const (
	unixPollIn   = 0
	unixPollHUP  = 0
	unixPollErr  = 0
	unixPollNval = 0
)

var (
	unixEINTR  = errors.New("EINTR")
	unixEAGAIN = errors.New("EAGAIN")
)

func unixPoll(fds []unixPollFd, timeout int) (int, error) {
	return 0, errors.New("poll not supported on this platform")
}

func unixRead(fd int, buf []byte) (int, error) {
	return 0, errors.New("read not supported on this platform")
}

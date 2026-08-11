// internal/ui/reducer_download.go
//
// Attachment downloads: the tail of the `o` keybinding for file
// attachments (see openLinksOfSelected / openLinkItemCmd).
//
// Slack serves an attachment's real bytes only from its url_private,
// behind workspace auth, so "opening" a file means downloading it
// first. The bytes land next to the user's other downloads and then:
//
//   - media and PDFs are handed to the OS default application, which
//     is what "open this video" means in practice;
//   - everything else (zip, tarball, unknown types) is only saved,
//     with a toast naming the path. Handing a .zip to `open` would
//     unarchive it, which is not what the user asked for.
package ui

import (
	"context"
	"fmt"
	"mime"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	imgpkg "github.com/gammons/slk/internal/image"
)

// downloadResultMsg reports a finished (or failed) attachment
// download back to the UI loop.
type downloadResultMsg struct {
	Path string
	Mime string
	Err  error
}

var reduceDownload reducerFunc = func(a *App, msg tea.Msg) (tea.Cmd, bool) {
	switch m := msg.(type) {
	case DownloadFileMsg:
		if a.imageFetcher == nil {
			// No fetcher wired (headless tests): the permalink in the
			// browser is a worse but working answer.
			return a.browserOpener(m.URL), true
		}
		fetcher := a.imageFetcher
		url, name, mimeType := m.URL, m.Name, m.Mime
		a.statusbar.SetToast("Downloading " + downloadFilename(name, url, mimeType) + "…")
		return func() tea.Msg { return downloadAttachment(fetcher, url, name, mimeType) }, true

	case downloadResultMsg:
		if m.Err != nil {
			return toastWithClear(a, "Download failed: "+truncateReason(m.Err.Error(), 40), 4*time.Second), true
		}
		if opensInSystemApp(m.Mime, m.Path) {
			return tea.Batch(
				openInSystemViewerCmd(m.Path),
				toastWithClear(a, "Opened "+filepath.Base(m.Path), 3*time.Second),
			), true
		}
		return toastWithClear(a, "Saved to "+displayPath(m.Path), 5*time.Second), true
	}
	return nil, false
}

// downloadAttachment fetches rawURL and writes it to the download
// directory. Runs off the UI loop, so it returns a msg rather than
// touching App.
func downloadAttachment(f *imgpkg.Fetcher, rawURL, name, mimeType string) tea.Msg {
	// No context deadline here: the fetcher's own client bounds the
	// request, and a second, shorter deadline would only cut off large
	// downloads early.
	body, contentType, err := f.DownloadRaw(context.Background(), rawURL)
	if err != nil {
		return downloadResultMsg{Err: err}
	}
	if mimeType == "" {
		mimeType = contentType
	}
	dir, err := downloadDir()
	if err != nil {
		return downloadResultMsg{Err: err}
	}
	dest := uniquePath(filepath.Join(dir, downloadFilename(name, rawURL, mimeType)))
	if err := os.WriteFile(dest, body, 0o644); err != nil {
		return downloadResultMsg{Err: err}
	}
	return downloadResultMsg{Path: dest, Mime: mimeType}
}

// downloadDir is where attachments are saved: the user's Desktop when
// there is one, else the home directory. Deliberately does not create
// a Desktop directory on machines that don't have one.
func downloadDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	desktop := filepath.Join(home, "Desktop")
	if st, err := os.Stat(desktop); err == nil && st.IsDir() {
		return desktop, nil
	}
	return home, nil
}

// downloadFilename picks the on-disk name: Slack's filename when it
// gave us one, else the last path segment of the URL. Slack file
// "titles" can be extensionless, and the extension is what decides
// which application opens the file, so one is appended from the
// mimetype when missing.
func downloadFilename(name, rawURL, mimeType string) string {
	urlBase := ""
	if u, err := url.Parse(rawURL); err == nil {
		urlBase = sanitizeDownloadName(path.Base(u.Path))
	}
	base := sanitizeDownloadName(name)
	if base == "" {
		base = urlBase
	}
	if base == "" || base == "." || base == string(filepath.Separator) {
		base = "slk-download"
	}
	if filepath.Ext(base) == "" {
		if ext := filepath.Ext(urlBase); ext != "" {
			base += ext
		} else if ext := extForMime(mimeType); ext != "" {
			base += ext
		}
	}
	return base
}

// sanitizeDownloadName strips what would let a Slack-supplied filename
// escape the download directory, while keeping the spaces and the
// extension that make it recognizable. (sanitizeForFilename is not
// usable here: it is built for channel names and replaces the dot.)
func sanitizeDownloadName(s string) string {
	s = strings.Map(func(r rune) rune {
		switch {
		case r == '/' || r == '\\':
			return '-'
		case r < 0x20 || r == 0x7f:
			return -1
		}
		return r
	}, strings.TrimSpace(s))
	// Leading dots would make the download invisible in Finder, and
	// ".." would point at the parent directory.
	return strings.TrimSpace(strings.TrimLeft(s, "."))
}

// extForMime picks the extension for a mimetype. The stdlib returns
// every registered extension in alphabetical order, so video/mp4
// yields ".f4v" before ".mp4"; prefer the one that matches the
// subtype, which is the conventional spelling.
func extForMime(mimeType string) string {
	exts, err := mime.ExtensionsByType(mimeType)
	if err != nil || len(exts) == 0 {
		return ""
	}
	if _, subtype, found := strings.Cut(mimeType, "/"); found {
		want := "." + strings.TrimPrefix(strings.ToLower(strings.TrimSpace(subtype)), "x-")
		for _, e := range exts {
			if e == want {
				return e
			}
		}
	}
	return exts[0]
}

// uniquePath appends -2, -3, … before the extension until the path is
// free, so downloading the same file twice doesn't clobber the first.
func uniquePath(p string) string {
	if _, err := os.Stat(p); os.IsNotExist(err) {
		return p
	}
	ext := filepath.Ext(p)
	stem := strings.TrimSuffix(p, ext)
	for i := 2; i < 1000; i++ {
		candidate := fmt.Sprintf("%s-%d%s", stem, i, ext)
		if _, err := os.Stat(candidate); os.IsNotExist(err) {
			return candidate
		}
	}
	return p
}

// opensInSystemApp reports whether a downloaded file should be handed
// to the OS default application. Media and PDFs open in a player or
// viewer, which is the point of pressing `o` on a video. Archives and
// unknown types are left on disk: `open` on a .zip would unarchive it.
func opensInSystemApp(mimeType, p string) bool {
	m := strings.ToLower(strings.TrimSpace(mimeType))
	if i := strings.IndexByte(m, ';'); i >= 0 {
		m = strings.TrimSpace(m[:i]) // strip "; charset=..."
	}
	if m == "" || m == "application/octet-stream" {
		// Slack didn't say, or said nothing useful; the extension is
		// the better signal.
		m = strings.ToLower(mime.TypeByExtension(filepath.Ext(p)))
	}
	switch {
	case strings.HasPrefix(m, "video/"), strings.HasPrefix(m, "audio/"), strings.HasPrefix(m, "image/"):
		return true
	case m == "application/pdf":
		return true
	}
	return false
}

// displayPath shortens a path under the home directory to ~/… so the
// toast fits the status bar.
func displayPath(p string) string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return p
	}
	if rel, err := filepath.Rel(home, p); err == nil && !strings.HasPrefix(rel, "..") {
		return filepath.Join("~", rel)
	}
	return p
}

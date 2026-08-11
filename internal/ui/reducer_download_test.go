package ui

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDownloadFilename(t *testing.T) {
	tests := []struct {
		name     string
		attName  string
		url      string
		mime     string
		expected string
	}{
		{
			name:     "slack filename wins",
			attName:  "WhatsApp Video 2026-08-10.mp4",
			url:      "https://files.slack.com/files-pri/T1-F1/download/abc.mp4",
			mime:     "video/mp4",
			expected: "WhatsApp Video 2026-08-10.mp4",
		},
		{
			name:     "falls back to url basename",
			attName:  "",
			url:      "https://files.slack.com/files-pri/T1-F1/download/report.zip",
			mime:     "application/zip",
			expected: "report.zip",
		},
		{
			name:     "extensionless title gets one from the mimetype",
			attName:  "Demo Recording",
			url:      "https://files.slack.com/files-pri/T1-F1/download/x.mp4",
			mime:     "video/mp4",
			expected: "Demo Recording.mp4",
		},
		{
			name:     "no name and no url path",
			attName:  "",
			url:      "",
			mime:     "",
			expected: "slk-download",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := downloadFilename(tt.attName, tt.url, tt.mime); got != tt.expected {
				t.Errorf("downloadFilename(%q, %q, %q) = %q, want %q",
					tt.attName, tt.url, tt.mime, got, tt.expected)
			}
		})
	}
}

func TestOpensInSystemApp(t *testing.T) {
	tests := []struct {
		mime     string
		path     string
		expected bool
	}{
		{"video/mp4", "/tmp/a.mp4", true},
		{"audio/mpeg", "/tmp/a.mp3", true},
		{"image/png", "/tmp/a.png", true},
		{"application/pdf", "/tmp/a.pdf", true},
		{"video/mp4; charset=binary", "/tmp/a.mp4", true},
		// The user's zip case: saved, never handed to `open`, which
		// would unarchive it.
		{"application/zip", "/tmp/a.zip", false},
		{"application/x-tar", "/tmp/a.tar", false},
		{"text/plain", "/tmp/a.txt", false},
		// Slack said nothing useful; fall back to the extension.
		{"", "/tmp/a.mp4", true},
		{"application/octet-stream", "/tmp/a.zip", false},
		{"", "/tmp/a.zip", false},
	}
	for _, tt := range tests {
		if got := opensInSystemApp(tt.mime, tt.path); got != tt.expected {
			t.Errorf("opensInSystemApp(%q, %q) = %v, want %v", tt.mime, tt.path, got, tt.expected)
		}
	}
}

func TestUniquePath_AvoidsClobber(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "clip.mp4")

	if got := uniquePath(p); got != p {
		t.Fatalf("free path changed: got %q, want %q", got, p)
	}
	if err := os.WriteFile(p, []byte("first"), 0o644); err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(dir, "clip-2.mp4")
	if got := uniquePath(p); got != want {
		t.Errorf("uniquePath after collision = %q, want %q", got, want)
	}
	if err := os.WriteFile(want, []byte("second"), 0o644); err != nil {
		t.Fatal(err)
	}
	want3 := filepath.Join(dir, "clip-3.mp4")
	if got := uniquePath(p); got != want3 {
		t.Errorf("uniquePath after two collisions = %q, want %q", got, want3)
	}
	// The original is untouched.
	b, err := os.ReadFile(p)
	if err != nil || string(b) != "first" {
		t.Errorf("original file modified: %q, %v", b, err)
	}
}

package image

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// DownloadRaw accepts payloads the image path is required to reject.
func TestDownloadRaw_AcceptsNonImagePayloads(t *testing.T) {
	const payload = "PK\x03\x04not-really-a-zip"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/zip")
		_, _ = w.Write([]byte(payload))
	}))
	defer srv.Close()

	f := NewFetcher(nil, srv.Client())

	body, ct, err := f.DownloadRaw(context.Background(), srv.URL)
	if err != nil {
		t.Fatalf("DownloadRaw: %v", err)
	}
	if string(body) != payload {
		t.Errorf("body = %q, want %q", body, payload)
	}
	if ct != "application/zip" {
		t.Errorf("content type = %q", ct)
	}

	// The image path must still reject it, or a zip could end up in
	// the image cache.
	if _, _, err := f.download(context.Background(), srv.URL); err == nil {
		t.Error("image download accepted a zip payload")
	}
}

// Slack answers an unauthenticated file request with its login page
// under a 200, so HTML must read as an auth failure rather than as a
// successful download.
func TestDownloadRaw_RejectsHTMLLoginPage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte("<html>sign in</html>"))
	}))
	defer srv.Close()

	f := NewFetcher(nil, srv.Client())
	if _, _, err := f.DownloadRaw(context.Background(), srv.URL); err == nil {
		t.Error("expected an error for an HTML response")
	}
}

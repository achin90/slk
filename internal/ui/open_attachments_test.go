package ui

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/gammons/slk/internal/ui/messages"
)

// A file attachment with a url_private downloads rather than opening
// the permalink in a browser.
func TestOpenLinkKey_AttachmentWithPrivateURL_DispatchesDownload(t *testing.T) {
	app := NewApp()
	app.focusedPanel = PanelMessages
	app.messagepane.SetMessages([]messages.MessageItem{{
		TS:   "1700000001.000100",
		Text: "here's the recording",
		Attachments: []messages.Attachment{{
			Kind:        "file",
			Name:        "demo.mp4",
			Mime:        "video/mp4",
			URL:         "https://team.slack.com/files/U1/F1/demo.mp4",
			FallbackURL: "https://files.slack.com/files-pri/T1-F1/download/demo.mp4",
		}},
	}})

	cmd := pressO(app)
	if cmd == nil {
		t.Fatal("expected a command")
	}
	msg, ok := cmd().(DownloadFileMsg)
	if !ok {
		t.Fatalf("expected DownloadFileMsg, got %T", cmd())
	}
	if msg.URL != "https://files.slack.com/files-pri/T1-F1/download/demo.mp4" {
		t.Errorf("expected the url_private, got %q", msg.URL)
	}
	if msg.Name != "demo.mp4" || msg.Mime != "video/mp4" {
		t.Errorf("name/mime not carried: %q %q", msg.Name, msg.Mime)
	}
}

// Without a url_private (older cached messages) the permalink still
// opens in the browser, as it did before downloads existed.
func TestOpenLinkKey_AttachmentWithoutPrivateURL_FallsBackToOpenLink(t *testing.T) {
	app := NewApp()
	app.focusedPanel = PanelMessages
	app.messagepane.SetMessages([]messages.MessageItem{{
		TS:   "1700000001.000100",
		Text: "here's the recording",
		Attachments: []messages.Attachment{{
			Kind: "file",
			Name: "demo.mp4",
			URL:  "https://team.slack.com/files/U1/F1/demo.mp4",
		}},
	}})

	cmd := pressO(app)
	if cmd == nil {
		t.Fatal("expected a command")
	}
	msg, ok := cmd().(OpenLinkMsg)
	if !ok {
		t.Fatalf("expected OpenLinkMsg, got %T", cmd())
	}
	if msg.URL != "https://team.slack.com/files/U1/F1/demo.mp4" {
		t.Errorf("unexpected url %q", msg.URL)
	}
}

// In the picker, an attachment row is marked for download and is never
// treated as an in-app permalink.
func TestLinkPicker_AttachmentRow_ChoosingItDownloads(t *testing.T) {
	app := NewApp()
	app.focusedPanel = PanelMessages
	app.messagepane.SetMessages([]messages.MessageItem{{
		TS:   "1700000001.000100",
		Text: "see <https://example.com|the docs> and the clip",
		Attachments: []messages.Attachment{{
			Kind:        "file",
			Name:        "clip.mp4",
			Mime:        "video/mp4",
			URL:         "https://team.slack.com/files/U1/F1/clip.mp4",
			FallbackURL: "https://files.slack.com/files-pri/T1-F1/download/clip.mp4",
		}},
	}})

	pressO(app)
	if app.mode != ModeLinkPicker {
		t.Fatalf("expected the picker, got mode %v", app.mode)
	}
	items := app.linkPicker.Items()
	if len(items) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(items))
	}
	if items[0].Download {
		t.Error("text link marked as a download")
	}
	att := items[1]
	if !att.Download || att.InApp {
		t.Errorf("attachment row: Download=%v InApp=%v, want true/false", att.Download, att.InApp)
	}

	// Row 0 is the text link; move down to the attachment and choose it.
	app.handleKey(tea.KeyPressMsg{Code: 'j', Text: "j"})
	cmd := app.handleKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("expected a command from enter")
	}
	msg, ok := cmd().(DownloadFileMsg)
	if !ok {
		t.Fatalf("expected DownloadFileMsg, got %T", cmd())
	}
	if msg.Name != "clip.mp4" {
		t.Errorf("unexpected name %q", msg.Name)
	}
}

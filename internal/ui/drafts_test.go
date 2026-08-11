package ui

import (
	"testing"

	"github.com/gammons/slk/internal/ui/compose"
	"github.com/gammons/slk/internal/ui/messages"
	"github.com/gammons/slk/internal/ui/wintree"
)

func TestChannelSwitch_StashesDraftAndClearsBox(t *testing.T) {
	app := NewApp()
	app.activeChannelID = "C1"
	app.compose.SetValue("half written thought")

	app.Update(ChannelSelectedMsg{ID: "C2"})

	if got := app.compose.Value(); got != "" {
		t.Fatalf("compose after switch = %q; want empty (draft must not follow)", got)
	}
}

func TestChannelSwitch_RestoresDraftOnReturn(t *testing.T) {
	app := NewApp()
	app.activeChannelID = "C1"
	app.compose.SetValue("half written thought")

	app.Update(ChannelSelectedMsg{ID: "C2"})
	app.Update(ChannelSelectedMsg{ID: "C1"})

	if got := app.compose.Value(); got != "half written thought" {
		t.Errorf("compose back in C1 = %q; want the stashed draft", got)
	}
}

func TestChannelSwitch_DraftsAreIndependentPerChannel(t *testing.T) {
	app := NewApp()
	app.activeChannelID = "C1"
	app.compose.SetValue("for C1")

	app.Update(ChannelSelectedMsg{ID: "C2"})
	app.compose.SetValue("for C2")
	app.Update(ChannelSelectedMsg{ID: "C1"})

	if got := app.compose.Value(); got != "for C1" {
		t.Errorf("compose in C1 = %q; want %q", got, "for C1")
	}

	app.Update(ChannelSelectedMsg{ID: "C2"})
	if got := app.compose.Value(); got != "for C2" {
		t.Errorf("compose in C2 = %q; want %q", got, "for C2")
	}
}

// A sent draft must not come back. restore deletes the entry it applies,
// so once the box has been emptied there is nothing left to resurrect.
func TestChannelSwitch_SentDraftDoesNotResurrect(t *testing.T) {
	app := NewApp()
	app.activeChannelID = "C1"
	app.compose.SetValue("about to send")

	app.Update(ChannelSelectedMsg{ID: "C2"})
	app.Update(ChannelSelectedMsg{ID: "C1"})
	app.compose.Reset() // stands in for a successful send
	app.Update(ChannelSelectedMsg{ID: "C2"})
	app.Update(ChannelSelectedMsg{ID: "C1"})

	if got := app.compose.Value(); got != "" {
		t.Errorf("compose after send = %q; want empty", got)
	}
}

func TestChannelSwitch_AttachmentsTravelWithDraft(t *testing.T) {
	app := NewApp()
	app.activeChannelID = "C1"
	app.compose.SetValue("caption")
	app.compose.AddAttachment(compose.PendingAttachment{Filename: "clip.mp4", Path: "/tmp/clip.mp4"})

	app.Update(ChannelSelectedMsg{ID: "C2"})
	if len(app.compose.Attachments()) != 0 {
		t.Fatalf("attachments followed to C2: %d", len(app.compose.Attachments()))
	}

	app.Update(ChannelSelectedMsg{ID: "C1"})
	atts := app.compose.Attachments()
	if len(atts) != 1 {
		t.Fatalf("attachments back in C1 = %d; want 1", len(atts))
	}
	if atts[0].Filename != "clip.mp4" {
		t.Errorf("filename = %q; want clip.mp4", atts[0].Filename)
	}
}

func TestThreadDrafts_KeyedPerThread(t *testing.T) {
	app := NewApp()
	app.activeChannelID = "C1"

	app.openThreadPanel(messages.MessageItem{TS: "100.0"}, "C1", "100.0")
	app.threadCompose.SetValue("reply to first")
	app.CloseThread()

	app.openThreadPanel(messages.MessageItem{TS: "200.0"}, "C1", "200.0")
	if got := app.threadCompose.Value(); got != "" {
		t.Fatalf("second thread compose = %q; want empty", got)
	}
	app.CloseThread()

	app.openThreadPanel(messages.MessageItem{TS: "100.0"}, "C1", "100.0")
	if got := app.threadCompose.Value(); got != "reply to first" {
		t.Errorf("first thread compose = %q; want the stashed reply", got)
	}
}

// Window focus retargets the active channel without going through
// ChannelSelectedMsg, so ctrl+w between two panes on different channels is
// a second path that could carry a draft into the wrong channel.
func TestWindowFocusSwitch_SwapsDrafts(t *testing.T) {
	app := newWideTestApp(t)
	first := app.focusedWin

	app.Update(ChannelSelectedMsg{ID: "C1", Name: "one"})
	app.compose.SetValue("for C1")

	if cmd := app.splitWindow(wintree.SplitSideBySide); cmd != nil {
		t.Fatal("split should not toast in a wide test app")
	}
	second := app.focusedWin
	app.Update(ChannelSelectedMsg{ID: "C2", Name: "two"})
	app.compose.SetValue("for C2")

	app.focusWindow(first)
	if got := app.compose.Value(); got != "for C1" {
		t.Errorf("after focusing C1's window = %q; want %q", got, "for C1")
	}

	app.focusWindow(second)
	if got := app.compose.Value(); got != "for C2" {
		t.Errorf("after focusing C2's window = %q; want %q", got, "for C2")
	}
}

func TestDraftStore_EmptyBoxClearsPreviousEntry(t *testing.T) {
	store := newDraftStore()
	c := compose.New("general")

	c.SetValue("something")
	store.stash("K", &c)
	c.Reset()
	store.stash("K", &c) // revisited, typed nothing

	store.restore("K", &c)
	if got := c.Value(); got != "" {
		t.Errorf("restored %q; want empty (stale draft resurrected)", got)
	}
}

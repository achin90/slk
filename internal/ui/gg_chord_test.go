// internal/ui/gg_chord_test.go
//
// gg (jump-to-top) chord tests, mirroring the ctrl+w chord tests in
// windows_chord_test.go. The first `g` arms a pending sub-state in
// normal mode; the second `g` jumps to top. A non-g key cancels
// silently and falls through; any mode change disarms (SetMode).
package ui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

func TestChord_GG_JumpsToTop(t *testing.T) {
	a := newTestAppWithMessages(t)
	a.focusedPanel = PanelMessages
	// Start at the bottom (last of 2 messages).
	_ = handleNormalMode(a, tea.KeyPressMsg{Code: 'G', Text: "G", Mod: tea.ModShift})
	if a.messagepane.SelectedIndex() != 1 {
		t.Fatalf("setup: expected selection at bottom 1, got %d", a.messagepane.SelectedIndex())
	}

	// gg should jump to top.
	_ = press(a, 'g')
	if !a.pendingG {
		t.Fatal("first g should arm the pending gg state")
	}
	_ = press(a, 'g')
	if a.pendingG {
		t.Fatal("second g should disarm the pending state")
	}
	if a.messagepane.SelectedIndex() != 0 {
		t.Fatalf("gg: expected selection at top 0, got %d", a.messagepane.SelectedIndex())
	}
}

func TestChord_GG_NonGCancelsAndFallsThrough(t *testing.T) {
	a := newTestAppWithMessages(t)
	a.focusedPanel = PanelMessages
	// Start at the bottom.
	_ = handleNormalMode(a, tea.KeyPressMsg{Code: 'G', Text: "G", Mod: tea.ModShift})
	if a.messagepane.SelectedIndex() != 1 {
		t.Fatalf("setup: expected selection at bottom 1, got %d", a.messagepane.SelectedIndex())
	}

	// First g arms; a following j (down) cancels the chord and moves
	// the selection down — it must NOT jump to top.
	_ = press(a, 'g')
	if !a.pendingG {
		t.Fatal("first g should arm the pending gg state")
	}
	_ = press(a, 'j')
	if a.pendingG {
		t.Fatal("non-g key should cancel the pending state")
	}
	if a.messagepane.SelectedIndex() == 0 {
		t.Fatal("non-g key must not jump to top")
	}
}

func TestChord_GG_HintLifecycle(t *testing.T) {
	a := newTestAppWithMessages(t)
	hint := func() string { return ansi.Strip(a.statusbar.View(200)) }
	if !strings.Contains(hint(), "? for keybindings") {
		t.Fatalf("precondition: default hint missing, got %q", hint())
	}

	// Armed: prefix hint shown.
	_ = press(a, 'g')
	if !strings.Contains(hint(), "g …") {
		t.Fatalf("armed: want %q hint, got %q", "g …", hint())
	}

	// Completed chord restores the default hint.
	_ = press(a, 'g')
	if strings.Contains(hint(), "g …") {
		t.Fatalf("after chord: prefix hint must clear, got %q", hint())
	}
	if !strings.Contains(hint(), "? for keybindings") {
		t.Fatalf("after chord: default hint must be restored, got %q", hint())
	}

	// SetMode disarm restores the default hint too.
	_ = press(a, 'g')
	a.SetMode(ModeConfirm)
	if strings.Contains(hint(), "g …") {
		t.Fatalf("after SetMode disarm: prefix hint must clear, got %q", hint())
	}
	if !strings.Contains(hint(), "? for keybindings") {
		t.Fatalf("after SetMode disarm: default hint must be restored, got %q", hint())
	}
}

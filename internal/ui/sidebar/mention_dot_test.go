package sidebar

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/gammons/slk/internal/cache"
	"github.com/gammons/slk/internal/ui/styles"
)

// rowFor returns the rendered sidebar line containing name.
func rowFor(t *testing.T, m Model, name string) string {
	t.Helper()
	view := m.View(10, 30)
	for _, l := range strings.Split(view, "\n") {
		if strings.Contains(l, name) {
			return l
		}
	}
	t.Fatalf("%s row not rendered:\n%s", name, view)
	return ""
}

// A channel whose read state carries mentions renders its unread dot in
// Error (red) instead of Primary (blue). MentionCount is incremented by
// the same ShouldNotify predicate that feeds SLK_MENTIONS, so a message
// that raises the tmux mention badge must also redden the channel dot —
// they are two views of one number.
func TestMentionDot_UsesErrorColor(t *testing.T) {
	m := New([]ChannelItem{
		{ID: "C1", Name: "mentioned", Type: "channel"},
		{ID: "C2", Name: "plainunread", Type: "channel"},
	})
	m.SetReadStateReader(func() map[string]cache.ReadState {
		return map[string]cache.ReadState{
			"C1": {HasUnread: true, MentionCount: 1},
			"C2": {HasUnread: true, MentionCount: 0},
		}
	})
	m.ToggleCollapse("Channels")

	mentionDot := lipgloss.NewStyle().Foreground(styles.Error).Render("●")
	plainDot := lipgloss.NewStyle().Foreground(styles.Primary).Render("●")
	if mentionDot == plainDot {
		t.Skip("color profile renders no ANSI; dot colors indistinguishable")
	}

	mentionRow := rowFor(t, m, "mentioned")
	if !strings.Contains(mentionRow, mentionDot) {
		t.Errorf("channel with mentions did not get the red dot:\n%q", mentionRow)
	}

	plainRow := rowFor(t, m, "plainunread")
	if !strings.Contains(plainRow, plainDot) {
		t.Errorf("plain unread channel lost its blue dot:\n%q", plainRow)
	}
	if strings.Contains(plainRow, mentionDot) {
		t.Errorf("plain unread channel got the red mention dot:\n%q", plainRow)
	}
}

// A muted channel with mentions still shows no dot at all: mute wins
// over the mention coloring, matching IsVisiblyUnread.
func TestMentionDot_SuppressedWhenMuted(t *testing.T) {
	m := New([]ChannelItem{
		{ID: "C1", Name: "noisy", Type: "channel", IsMuted: true},
	})
	m.SetReadStateReader(func() map[string]cache.ReadState {
		return map[string]cache.ReadState{"C1": {HasUnread: true, MentionCount: 3}}
	})
	m.ToggleCollapse("Channels")

	if row := rowFor(t, m, "noisy"); strings.Contains(row, "●") {
		t.Errorf("muted channel with mentions rendered a dot:\n%q", row)
	}
}

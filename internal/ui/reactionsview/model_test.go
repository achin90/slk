package reactionsview

import (
	"strings"
	"testing"
)

func sampleGroups() []ReactionGroup {
	return []ReactionGroup{
		{Emoji: "thumbsup", Users: []string{"Alice", "Bob", "You (you)"}, Count: 3},
		{Emoji: "eyes", Users: []string{"Carol"}, Count: 1},
	}
}

func TestOpenCloseVisibility(t *testing.T) {
	m := New()
	if m.IsVisible() {
		t.Fatal("new model should not be visible")
	}
	m.Open(sampleGroups())
	if !m.IsVisible() {
		t.Fatal("Open should make the model visible")
	}
	m.Close()
	if m.IsVisible() {
		t.Fatal("Close should hide the model")
	}
}

func TestHandleKeyEscapeCloses(t *testing.T) {
	m := New()
	m.Open(sampleGroups())
	m.HandleKey("esc")
	if m.IsVisible() {
		t.Fatal("esc should close the modal")
	}
}

func manyGroups(n int) []ReactionGroup {
	groups := make([]ReactionGroup, 0, n)
	for i := 0; i < n; i++ {
		groups = append(groups, ReactionGroup{
			Emoji: "thumbsup",
			Users: []string{"User"},
		})
	}
	return groups
}

func TestHandleKeyScrollClamps(t *testing.T) {
	m := New()
	// 10 groups -> 20 content lines (header + one user each). This overflows a
	// short terminal (maxOff > 0) but fits entirely in a tall one.
	m.Open(manyGroups(10))
	// A render must happen for maxOff to be computed; use a short height.
	m.ViewOverlay(80, 14, strings.Repeat("\n", 14))

	// Scrolling up at the top stays at 0.
	m.HandleKey("up")
	if m.Offset() != 0 {
		t.Fatalf("offset should clamp to 0 at top, got %d", m.Offset())
	}

	// Scrolling down far past the end clamps at the (positive) max offset.
	for i := 0; i < 500; i++ {
		m.HandleKey("down")
		m.ViewOverlay(80, 14, strings.Repeat("\n", 14)) // re-render recomputes maxOff
	}
	maxed := m.Offset()
	if maxed <= 0 {
		t.Fatalf("expected a positive clamped offset for overflowing content, got %d", maxed)
	}
	// One more down must not move past the clamp.
	m.HandleKey("down")
	if m.Offset() != maxed {
		t.Fatalf("offset should stay clamped at %d, got %d", maxed, m.Offset())
	}

	// A resize to a tall terminal (all 20 lines fit) re-clamps offset to 0.
	m.ViewOverlay(80, 40, strings.Repeat("\n", 40))
	if m.Offset() != 0 {
		t.Fatalf("offset should re-clamp to 0 when all content fits, got %d", m.Offset())
	}
}

func TestCountLabelKnownVsTotal(t *testing.T) {
	// Equal known/total -> plain number.
	if got := countLabel(3, 3); got != "3" {
		t.Fatalf("countLabel(3,3) = %q, want \"3\"", got)
	}
	// Unset total (0) falls back to known.
	if got := countLabel(3, 0); got != "3" {
		t.Fatalf("countLabel(3,0) = %q, want \"3\"", got)
	}
	// Slack reported more reactors than we have cached -> known/total.
	if got := countLabel(3, 8); got != "3/8" {
		t.Fatalf("countLabel(3,8) = %q, want \"3/8\"", got)
	}
}

func TestViewOverlayRendersTruncatedCount(t *testing.T) {
	m := New()
	// Slack says 8 reacted but only 2 user IDs are cached.
	m.Open([]ReactionGroup{
		{Emoji: "thumbsup", Users: []string{"Alice", "Bob"}, Count: 8},
	})
	out := m.ViewOverlay(80, 24, strings.Repeat("\n", 24))
	if !strings.Contains(out, "2/8") {
		t.Fatalf("expected header to show known/total \"2/8\", got:\n%s", out)
	}
}

func TestViewOverlayEdgeCasesDoNotPanic(t *testing.T) {
	// Empty groups must render without panicking and still show the title.
	m := New()
	m.Open(nil)
	out := m.ViewOverlay(80, 24, strings.Repeat("\n", 24))
	if !strings.Contains(out, "Reactions") {
		t.Fatalf("empty modal should still render the title, got:\n%s", out)
	}

	// A 1-row terminal must not panic.
	m.Open(sampleGroups())
	_ = m.ViewOverlay(80, 1, "\n")

	// A very narrow terminal must not panic.
	_ = m.ViewOverlay(2, 24, strings.Repeat("\n", 24))
}

// The shortcode label is what makes an unfamiliar emoji identifiable,
// so it renders beside the emoji — but not when the emoji already fell
// back to :shortcode: text, which would print the name twice.
func TestViewOverlayRendersShortcodeLabel(t *testing.T) {
	m := New()
	m.Open([]ReactionGroup{
		{Emoji: "thumbsup", Users: []string{"Alice"}, Count: 1},
		{Emoji: "definitely-not-a-real-emoji", Users: []string{"Bob"}, Count: 1},
	})
	out := m.ViewOverlay(80, 24, strings.Repeat("\n", 24))

	if !strings.Contains(out, ":thumbsup:") {
		t.Errorf("glyph emoji should be labeled with its shortcode, got:\n%s", out)
	}
	if strings.Count(out, ":definitely-not-a-real-emoji:") != 1 {
		t.Errorf("unresolvable emoji renders as its own shortcode and must not be labeled twice, got:\n%s", out)
	}
}

func TestViewOverlayRendersNamesAndCounts(t *testing.T) {
	m := New()
	out := m.ViewOverlay(80, 24, "background")
	if out != "background" {
		t.Fatal("hidden modal should return background unchanged")
	}

	m.Open(sampleGroups())
	out = m.ViewOverlay(80, 24, strings.Repeat("\n", 24))
	for _, want := range []string{"Reactions", "Alice", "Bob", "Carol", "You (you)", "(3)", "(1)"} {
		if !strings.Contains(out, want) {
			t.Fatalf("rendered modal missing %q\n%s", want, out)
		}
	}
}

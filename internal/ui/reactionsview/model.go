// Package reactionsview provides a read-only modal overlay that lists the
// reactions on a message, grouped by emoji, with the display names of the
// users who reacted with it. Data is supplied by the App (assembled from the
// cached per-user reaction data); the modal does not fetch anything itself.
package reactionsview

import (
	"image/color"
	"io"
	"strconv"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/muesli/reflow/truncate"

	slkemoji "github.com/gammons/slk/internal/emoji"
	imgpkg "github.com/gammons/slk/internal/image"
	"github.com/gammons/slk/internal/ui/messages"
	"github.com/gammons/slk/internal/ui/overlay"
	"github.com/gammons/slk/internal/ui/styles"
)

// ReactionGroup is one emoji and the resolved display names of the users who
// reacted with it. The current user's name is expected to already carry a
// "(you)" suffix when assembled by the caller.
//
// Count is the authoritative reaction count reported by Slack. It can exceed
// len(Users) because Slack truncates the per-reaction users list in
// conversations.history responses (we are cache-only by design). When they
// differ the header renders "known/total" so the modal never silently
// under-reports.
type ReactionGroup struct {
	Emoji string
	Users []string
	Count int
}

// EmojiContext bundles the emoji-image rendering dependencies for the
// reactions view. Set once at startup; updated again when the
// CustomEmojisLoadedMsg arrives via SetEmojiCustoms. Mirrors
// reactionpicker.EmojiContext.
type EmojiContext struct {
	PlaceCtx slkemoji.PlaceContext
	Cells    int               // 1 or 2; 0 falls back to 2
	Customs  map[string]string // workspace custom emoji map; nil = empty
}

// Model is the reactions-list overlay state.
type Model struct {
	groups   []ReactionGroup
	visible  bool
	offset   int // scroll offset in rendered content lines
	maxOff   int // last computed maximum offset (set during render)
	emojiCtx EmojiContext
}

// New creates an empty, hidden modal.
func New() *Model { return &Model{} }

// SetEmojiContext configures emoji-image rendering for the reactions
// view. Mirrors reactionpicker.Model.SetEmojiContext.
func (m *Model) SetEmojiContext(ctx EmojiContext) {
	if ctx.Cells != 1 && ctx.Cells != 2 {
		ctx.Cells = 2
	}
	m.emojiCtx = ctx
}

// SetEmojiCustoms updates the customs map without changing PlaceCtx
// or Cells. Called from App.SetCustomEmoji when the workspace's
// custom emoji list arrives.
func (m *Model) SetEmojiCustoms(customs map[string]string) {
	m.emojiCtx.Customs = customs
}

// Open shows the modal for the given reaction groups and resets scroll.
func (m *Model) Open(groups []ReactionGroup) {
	m.groups = groups
	m.offset = 0
	m.maxOff = 0
	m.visible = true
}

// Close hides the modal and clears state.
func (m *Model) Close() {
	m.visible = false
	m.groups = nil
	m.offset = 0
	m.maxOff = 0
}

// IsVisible reports whether the modal is showing.
func (m *Model) IsVisible() bool { return m.visible }

// Offset returns the current scroll offset (exported for tests).
func (m *Model) Offset() int { return m.offset }

// HandleKey processes a key for the modal. esc/q/L closes it; up/down and j/k
// scroll. Scroll is clamped to [0, maxOff], where maxOff is recomputed on each
// render; before the first render maxOff is 0 so scrolling is inert.
func (m *Model) HandleKey(keyStr string) {
	switch keyStr {
	case "esc", "escape", "q", "L":
		m.Close()
	case "up", "k":
		if m.offset > 0 {
			m.offset--
		}
	case "down", "j":
		if m.offset < m.maxOff {
			m.offset++
		}
	}
}

// emojiKind describes how resolveEmoji rendered an emoji. The header
// prints the :shortcode: label beside the emoji so ambiguous art is
// identifiable — except for emojiShortcode, which already *is* the
// label and would otherwise print twice.
type emojiKind int

const (
	emojiImage     emojiKind = iota // kitty image placement
	emojiGlyph                      // Unicode character
	emojiShortcode                  // ":name:" literal text
)

// resolveEmoji renders an emoji name as either a kitty image placement
// (when image mode is active and the URL resolves) or the legacy
// Unicode/shortcode fallback. The flush callback is non-nil only on the
// image path's cold path.
func (m *Model) resolveEmoji(name string) (string, func(io.Writer) error, emojiKind) {
	imageOK := slkemoji.ImageModeActive() && m.emojiCtx.PlaceCtx.Fetcher != nil
	if imageOK {
		if url, ok := slkemoji.URLForShortcode(name, m.emojiCtx.Customs); ok {
			cells := m.emojiCtx.Cells
			if cells <= 0 {
				cells = 2
			}
			if placement, flush, ok := slkemoji.Place(m.emojiCtx.PlaceCtx, url, cells); ok {
				return placement, flush, emojiImage
			}
		}
	}
	// Legacy fallback: strip skin tone for glyph rendering, then try
	// Unicode, then fall back to :shortcode: text.
	legacyName := slkemoji.StripSkinTone(name)
	resolved := slkemoji.Sprint(":" + legacyName + ":")
	if slkemoji.ShouldRenderUnicode(resolved) {
		return resolved, nil, emojiGlyph
	}
	return ":" + legacyName + ":", nil, emojiShortcode
}

// contentLines builds the full (unwindowed) list of rendered content lines:
// an emoji header per group followed by one indented line per user.
// Flush callbacks for kitty image uploads are collected and fired by
// renderBox after the visible window is determined.
func (m *Model) contentLines(bg color.Color, innerWidth int, flushes *[]func(io.Writer) error) []string {
	headerStyle := lipgloss.NewStyle().Background(bg).Foreground(styles.Primary).Bold(true)
	userStyle := lipgloss.NewStyle().Background(bg).Foreground(styles.TextPrimary)

	var lines []string
	for _, g := range m.groups {
		emojiStr, flush, kind := m.resolveEmoji(g.Emoji)
		if flush != nil {
			*flushes = append(*flushes, flush)
		}
		header := emojiStr
		if kind != emojiShortcode {
			header += "  :" + g.Emoji + ":"
		}
		header += "  (" + countLabel(len(g.Users), g.Count) + ")"
		lines = append(lines, headerStyle.Width(innerWidth).Render(fit(header, innerWidth)))
		for _, u := range g.Users {
			lines = append(lines, userStyle.Width(innerWidth).Render(fit("  "+u, innerWidth)))
		}
	}
	return lines
}

// countLabel renders the header count for a reaction group. It shows the plain
// number of listed reactors when that matches the authoritative total, and
// "known/total" when Slack reported a higher count than the users we have
// cached (truncated history responses), so the modal never silently
// under-reports. A zero/unset total falls back to the known count.
func countLabel(known, total int) string {
	if total > known {
		return strconv.Itoa(known) + "/" + strconv.Itoa(total)
	}
	return strconv.Itoa(known)
}

// fit truncates s with an ellipsis tail when it is wider than width, so a long
// display name cannot wrap and throw off the modal's line accounting. Matches
// the truncation discipline of the help/reactionpicker modals.
func fit(s string, width int) string {
	if width <= 0 || lipgloss.Width(s) <= width {
		return s
	}
	return truncate.StringWithTail(s, uint(width), "\u2026")
}

// ViewOverlay composites the modal onto background. Returns background
// unchanged when hidden.
func (m *Model) ViewOverlay(termWidth, termHeight int, background string) string {
	if !m.visible {
		return background
	}
	box := m.renderBox(termWidth, termHeight)
	if box == "" {
		return background
	}
	result := overlay.DimmedOverlay(termWidth, termHeight, background, box, 0.5)
	lines := strings.Split(result, "\n")
	if len(lines) > termHeight {
		lines = lines[:termHeight]
	}
	return strings.Join(lines, "\n")
}

func (m *Model) renderBox(termWidth, termHeight int) string {
	if !m.visible {
		return ""
	}

	overlayWidth := termWidth * 6 / 10
	if overlayWidth < 30 {
		overlayWidth = 30
	}
	if overlayWidth > 60 {
		overlayWidth = 60
	}
	if overlayWidth > termWidth-2 {
		overlayWidth = termWidth - 2
	}
	innerWidth := overlayWidth - 4 // border + padding

	bg := styles.Background

	title := lipgloss.NewStyle().
		Bold(true).
		Background(bg).
		Foreground(styles.Primary).
		Render("Reactions")

	var pendingFlushes []func(io.Writer) error
	all := m.contentLines(bg, innerWidth, &pendingFlushes)

	// Visible window: leave headroom for title, blank, footer (~6 lines).
	maxVisible := termHeight - 8
	if maxVisible < 3 {
		maxVisible = 3
	}
	if maxVisible > 24 {
		maxVisible = 24
	}

	m.maxOff = len(all) - maxVisible
	if m.maxOff < 0 {
		m.maxOff = 0
	}
	if m.offset > m.maxOff {
		m.offset = m.maxOff
	}

	end := m.offset + maxVisible
	if end > len(all) {
		end = len(all)
	}
	window := all[m.offset:end]

	footer := lipgloss.NewStyle().
		Background(bg).
		Foreground(styles.TextMuted).
		Render("\u2191/\u2193 scroll   esc close")

	content := title + "\n\n" + strings.Join(window, "\n") + "\n\n" + footer

	// Re-paint modal bg+fg after every ANSI reset so trailing/unstyled cells
	// don't leak the dimmed app behind the overlay (same as help/picker).
	content = messages.ReapplyBgAfterResets(content, messages.BgANSI()+messages.FgANSI())

	// Fire any kitty image upload callbacks the Place calls produced.
	// Most are no-ops (the messages pane already triggered the upload
	// via the shared Registry); the reactions view still owns the fire
	// to handle the case where it's the first/only surface to reference
	// a given emoji this session. Same pattern as the reaction picker.
	for _, fl := range pendingFlushes {
		_ = fl(imgpkg.KittyOutput)
	}

	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(styles.Primary).
		BorderBackground(bg).
		Background(bg).
		Padding(1, 1).
		Width(overlayWidth).
		Render(content)
}

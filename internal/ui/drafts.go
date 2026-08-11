package ui

import (
	"strings"

	"github.com/gammons/slk/internal/ui/compose"
)

// Unsent compose text is per-conversation state, but there is only one
// compose model per pane. Switching channels or threads therefore has to
// park the outgoing text somewhere and repaint the box for the incoming
// conversation — otherwise a half-written message follows the user into
// the next channel and can be sent to the wrong place.
//
// Drafts live in memory only: they are gone on restart. Slack persists
// them server-side; doing the same here means a schema migration and is
// deliberately out of scope.

// draft is the unsent contents of a compose box. Attachments travel with
// the text because a dropped file and its caption are one unit of work.
type draft struct {
	text string
	atts []compose.PendingAttachment
}

// draftStore holds drafts for conversations that are not on screen.
//
// Invariant: the store never holds a draft for the compose box currently
// being displayed. restore deletes the entry it applies, so the visible
// box is the only copy of text the user is editing and no stale draft can
// resurrect after a send.
type draftStore struct {
	drafts map[string]draft
}

func newDraftStore() *draftStore {
	return &draftStore{drafts: map[string]draft{}}
}

// stash saves the compose box's contents under key. An empty box stores
// nothing and clears any previous entry, so visiting a channel and typing
// nothing does not resurrect an older draft.
func (d *draftStore) stash(key string, c *compose.Model) {
	if key == "" || c == nil {
		return
	}
	text := c.Value()
	atts := c.Attachments()
	if strings.TrimSpace(text) == "" && len(atts) == 0 {
		delete(d.drafts, key)
		return
	}
	d.drafts[key] = draft{text: text, atts: atts}
}

// restore repaints the compose box for key: always cleared first, then
// filled from the stored draft if there is one. The unconditional clear is
// the whole point — a channel with no draft must show an empty box, not
// whatever the previous channel left behind.
func (d *draftStore) restore(key string, c *compose.Model) {
	if c == nil {
		return
	}
	c.Reset()
	if key == "" {
		return
	}
	dr, ok := d.drafts[key]
	if !ok {
		return
	}
	delete(d.drafts, key)
	c.SetValue(dr.text)
	for _, a := range dr.atts {
		c.AddAttachment(a)
	}
}

// channelDraftKey identifies a channel's draft. The team ID is part of the
// key because channel switching spans workspaces. DMs and group DMs need
// no special casing: they are channels with D/G IDs on the same path.
func (a *App) channelDraftKey(channelID string) string {
	if channelID == "" {
		return ""
	}
	return a.activeTeamID + "\x00" + channelID
}

// threadDraftKey identifies a thread reply draft, keyed by parent TS so
// two half-written replies in the same channel do not collide.
func (a *App) threadDraftKey(channelID, threadTS string) string {
	if channelID == "" || threadTS == "" {
		return ""
	}
	return a.activeTeamID + "\x00" + channelID + "\x00" + threadTS
}

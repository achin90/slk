package messages

import (
	"github.com/gammons/slk/internal/ui/messages/blockkit"
)

// Messages from Slack apps (GitHub, Linear, and friends) keep almost
// nothing in the top-level text field: notification bodies arrive as a
// rich_text block, and unfurl cards arrive as legacy attachments with
// Block Kit blocks nested inside them. Their URLs render as OSC 8
// hyperlinks because the renderer walks those structures — so any link
// collector that reads only MessageItem.Text finds nothing to open in
// exactly the messages that are hardest to click.
//
// MessageLinks walks everything a message can carry. Scope is limited to
// links the user can already see: mrkdwn links in text, section and
// context bodies, attachment pretext/text/fields/footer, plus each card's
// title_link. Image, thumbnail, and footer-icon URLs are deliberately
// skipped — they are avatars and service logos, and would pad the picker
// with rows nobody chooses. Button URLs are dropped at parse time
// (blockkit.ActionElement keeps only Kind and Label), so they are out of
// reach without widening that type.

// MessageLinks returns every openable link in a message, in the order a
// reader encounters it: body text first, then card contents, then file
// attachments. Deduplicated by URL, keeping the first label seen, which
// is the one closest to the message body.
func MessageLinks(msg MessageItem) []Link {
	c := linkCollector{seen: map[string]bool{}}
	// MessageTextSource prefers a rich_text block's reconstructed mrkdwn
	// over the lossy flattened Text field, matching what the renderer
	// displays.
	c.mrkdwn(MessageTextSource(msg))
	c.blocks(msg.Blocks)
	for _, att := range msg.LegacyAttachments {
		c.legacyAttachment(att)
	}
	// File attachments last: they render below the message body, and
	// AppendAttachmentLinks handles the download-vs-open distinction.
	return AppendAttachmentLinks(c.links, msg.Attachments)
}

type linkCollector struct {
	links []Link
	seen  map[string]bool
}

func (c *linkCollector) add(url, label string) {
	if url == "" || c.seen[url] {
		return
	}
	c.seen[url] = true
	c.links = append(c.links, Link{URL: url, Label: label})
}

// mrkdwn harvests <url> and <url|label> tokens from a mrkdwn string.
func (c *linkCollector) mrkdwn(s string) {
	for _, l := range ExtractLinks(s) {
		c.add(l.URL, l.Label)
	}
}

func (c *linkCollector) blocks(blocks []blockkit.Block) {
	for _, b := range blocks {
		switch v := b.(type) {
		case blockkit.SectionBlock:
			c.mrkdwn(v.Text)
			for _, f := range v.Fields {
				c.mrkdwn(f)
			}
		case blockkit.ContextBlock:
			for _, e := range v.Elements {
				c.mrkdwn(e.Text)
			}
		case blockkit.RichTextBlock:
			c.mrkdwn(blockkit.RichTextToMrkdwn(v))
		}
	}
}

func (c *linkCollector) legacyAttachment(att blockkit.LegacyAttachment) {
	// The card title is a hyperlink in the render, so it is the most
	// likely thing the user is reaching for. Collect it first.
	c.add(att.TitleLink, att.Title)
	c.mrkdwn(att.Pretext)
	c.mrkdwn(att.Text)
	for _, f := range att.Fields {
		c.mrkdwn(f.Title)
		c.mrkdwn(f.Value)
	}
	c.mrkdwn(att.Footer)
	// Slack's newer unfurl shape puts all visible content here while the
	// classic fields sit empty — this is the Linear/GitHub card path.
	c.blocks(att.Blocks)
}

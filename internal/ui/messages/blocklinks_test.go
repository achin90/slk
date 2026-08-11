package messages

import (
	"testing"

	"github.com/slack-go/slack"

	"github.com/gammons/slk/internal/ui/messages/blockkit"
)

// The shape Linear and GitHub actually send: a legacy attachment whose
// Title/Text are empty and whose content lives in nested blocks.
func TestMessageLinks_LegacyAttachmentNestedBlocks(t *testing.T) {
	msg := MessageItem{
		TS: "1.0",
		LegacyAttachments: []blockkit.LegacyAttachment{{
			Blocks: []blockkit.Block{
				blockkit.SectionBlock{
					Text: "<https://github.com/corner-health/internal_tools/pull/6021|Migrate direct Slack calls>",
				},
			},
		}},
	}

	links := MessageLinks(msg)
	if len(links) != 1 {
		t.Fatalf("len(links) = %d; want 1: %+v", len(links), links)
	}
	if links[0].URL != "https://github.com/corner-health/internal_tools/pull/6021" {
		t.Errorf("URL = %q", links[0].URL)
	}
	if links[0].Label != "Migrate direct Slack calls" {
		t.Errorf("Label = %q; want the mrkdwn label", links[0].Label)
	}
}

func TestMessageLinks_AttachmentTitleLink(t *testing.T) {
	msg := MessageItem{
		TS: "1.0",
		LegacyAttachments: []blockkit.LegacyAttachment{{
			Title:     "CH-2745 Follow-up workflow from charting note",
			TitleLink: "https://linear.app/corner/issue/CH-2745",
		}},
	}

	links := MessageLinks(msg)
	if len(links) != 1 {
		t.Fatalf("len(links) = %d; want 1: %+v", len(links), links)
	}
	if links[0].URL != "https://linear.app/corner/issue/CH-2745" {
		t.Errorf("URL = %q", links[0].URL)
	}
	if links[0].Label != "CH-2745 Follow-up workflow from charting note" {
		t.Errorf("Label = %q; want the card title", links[0].Label)
	}
}

// GitHub review-request notifications arrive as a rich_text block; the
// flattened Text field is lossy, so MessageLinks reads the block.
func TestMessageLinks_RichTextBlock(t *testing.T) {
	msg := MessageItem{
		TS: "1.0",
		Blocks: []blockkit.Block{
			blockkit.RichTextBlock{
				Elements: []slack.RichTextElement{
					slack.NewRichTextSection(
						slack.NewRichTextSectionLinkElement(
							"https://github.com/corner-health/internal_tools/pull/6129",
							"Add conversations to internal_raw",
							nil,
						),
					),
				},
			},
		},
	}

	links := MessageLinks(msg)
	if len(links) != 1 {
		t.Fatalf("len(links) = %d; want 1: %+v", len(links), links)
	}
	if links[0].URL != "https://github.com/corner-health/internal_tools/pull/6129" {
		t.Errorf("URL = %q", links[0].URL)
	}
}

func TestMessageLinks_ContextAndFieldsAndFooter(t *testing.T) {
	msg := MessageItem{
		TS: "1.0",
		Blocks: []blockkit.Block{
			blockkit.ContextBlock{Elements: []blockkit.ContextElement{
				{Text: "opened by <https://github.com/dan|Dan>"},
			}},
			blockkit.SectionBlock{Fields: []string{"<https://ci.example/build/9|build 9>"}},
		},
		LegacyAttachments: []blockkit.LegacyAttachment{{
			Pretext: "<https://a.example/pre|pre>",
			Text:    "<https://a.example/body|body>",
			Fields:  []blockkit.LegacyField{{Title: "Status", Value: "<https://a.example/field|field>"}},
			Footer:  "<https://a.example/footer|footer>",
		}},
	}

	if got := len(MessageLinks(msg)); got != 6 {
		t.Errorf("len(links) = %d; want 6: %+v", got, MessageLinks(msg))
	}
}

// Avatars and service logos would pad the picker with rows nobody picks.
func TestMessageLinks_SkipsImageURLs(t *testing.T) {
	msg := MessageItem{
		TS: "1.0",
		Blocks: []blockkit.Block{
			blockkit.ImageBlock{URL: "https://cdn.example/logo.png", Title: "logo"},
			blockkit.ContextBlock{Elements: []blockkit.ContextElement{
				{ImageURL: "https://cdn.example/avatar.png", AltText: "avatar"},
			}},
		},
		LegacyAttachments: []blockkit.LegacyAttachment{{
			ImageURL:   "https://cdn.example/card.png",
			ThumbURL:   "https://cdn.example/thumb.png",
			FooterIcon: "https://cdn.example/icon.png",
		}},
	}

	if links := MessageLinks(msg); len(links) != 0 {
		t.Errorf("len(links) = %d; want 0: %+v", len(links), links)
	}
}

func TestMessageLinks_DedupesAcrossTextAndCard(t *testing.T) {
	const url = "https://github.com/corner-health/internal_tools/pull/6021"
	msg := MessageItem{
		TS:   "1.0",
		Text: "<" + url + "|from text>",
		LegacyAttachments: []blockkit.LegacyAttachment{{
			Title:     "from card",
			TitleLink: url,
		}},
	}

	links := MessageLinks(msg)
	if len(links) != 1 {
		t.Fatalf("len(links) = %d; want 1 (deduped): %+v", len(links), links)
	}
	if links[0].Label != "from text" {
		t.Errorf("Label = %q; want the label nearest the message body", links[0].Label)
	}
}

// Reading order: body text, then card contents, then file attachments.
func TestMessageLinks_OrdersBodyThenCardThenFiles(t *testing.T) {
	msg := MessageItem{
		TS:   "1.0",
		Text: "see <https://a.example/body|body>",
		LegacyAttachments: []blockkit.LegacyAttachment{{
			Title:     "card",
			TitleLink: "https://a.example/card",
		}},
		Attachments: []Attachment{{
			Kind:        "file",
			Name:        "clip.mp4",
			URL:         "https://slack.example/permalink",
			FallbackURL: "https://files.slack.com/clip.mp4",
		}},
	}

	links := MessageLinks(msg)
	if len(links) != 3 {
		t.Fatalf("len(links) = %d; want 3: %+v", len(links), links)
	}
	want := []string{
		"https://a.example/body",
		"https://a.example/card",
		"https://files.slack.com/clip.mp4",
	}
	for i, w := range want {
		if links[i].URL != w {
			t.Errorf("links[%d].URL = %q; want %q", i, links[i].URL, w)
		}
	}
	if !links[2].Download {
		t.Error("file attachment should still be marked for download")
	}
}

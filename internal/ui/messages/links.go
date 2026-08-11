// internal/ui/messages/links.go
//
// ExtractLinks pulls http(s)/mailto links out of a message's mrkdwn
// text using the same regexes the renderer uses (render.go), so the
// "open link" keybinding sees exactly the links the user sees.
package messages

import (
	"sort"
	"strings"
)

// Link is one link found in a message's text.
type Link struct {
	URL   string
	Label string // empty for bare <url> links
}

// ExtractLinks returns the links in text in order of appearance,
// deduplicated by URL (first occurrence wins). Returns nil when text
// has no links.
func ExtractLinks(text string) []Link {
	type posLink struct {
		start int
		link  Link
	}
	var found []posLink
	for _, m := range linkWithLabelRe.FindAllStringSubmatchIndex(text, -1) {
		found = append(found, posLink{
			start: m[0],
			link:  Link{URL: text[m[2]:m[3]], Label: text[m[4]:m[5]]},
		})
	}
	for _, m := range linkBareRe.FindAllStringSubmatchIndex(text, -1) {
		url := text[m[2]:m[3]]
		// linkBareRe also matches the labeled form (its [^>]+ spans
		// the "|label" part); those were already captured above.
		if strings.Contains(url, "|") {
			continue
		}
		found = append(found, posLink{start: m[0], link: Link{URL: url}})
	}
	sort.SliceStable(found, func(i, j int) bool { return found[i].start < found[j].start })
	var out []Link
	seen := make(map[string]bool, len(found))
	for _, f := range found {
		if seen[f.link.URL] {
			continue
		}
		seen[f.link.URL] = true
		out = append(out, f.link)
	}
	return out
}

// AppendAttachmentLinks appends one Link per attachment to links and
// returns the combined slice, deduplicated by URL against the links
// already present (text links win, since they carry the user's own
// label).
//
// Attachment URLs live on the Attachment struct rather than in the
// message text, so ExtractLinks alone can't see them — a message whose
// only "link" is an uploaded video or PDF would otherwise report "no
// links" even though the file is rendered right there in the pane.
// Attachments sort after text links because they render below the
// message body.
func AppendAttachmentLinks(links []Link, atts []Attachment) []Link {
	if len(atts) == 0 {
		return links
	}
	seen := make(map[string]bool, len(links)+len(atts))
	for _, l := range links {
		seen[l.URL] = true
	}
	out := links
	for _, att := range atts {
		if att.URL == "" || seen[att.URL] {
			continue
		}
		seen[att.URL] = true
		out = append(out, Link{URL: att.URL, Label: attachmentLinkLabel(att)})
	}
	return out
}

// attachmentLinkLabel is the picker label for an attachment: its
// filename when Slack supplied one, otherwise the same generic marker
// the message pane renders ("[Image]" / "[File]").
func attachmentLinkLabel(att Attachment) string {
	if name := strings.TrimSpace(att.Name); name != "" {
		return name
	}
	if att.Kind == "image" {
		return "[Image]"
	}
	return "[File]"
}

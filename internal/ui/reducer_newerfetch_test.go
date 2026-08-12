// internal/ui/reducer_newerfetch_test.go
//
// Forward backfill: a buffer replaced by a jump window (permalink,
// search hit, `gp`) must be able to page back to live.
package ui

import (
	"errors"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/gammons/slk/internal/ids"
	"github.com/gammons/slk/internal/ui/messages"
)

func jumpWindowApp(t *testing.T) *App {
	t.Helper()
	app := NewApp()
	app.activeChannelID = "C1"
	app.focusedPanel = PanelMessages
	app.view = ViewChannels
	app.bootstrap.loading = false
	app.messagepane.SetMessages([]messages.MessageItem{
		{TS: "1700000900.000000", Text: "live tail"},
	})
	app.Update(MessagesAroundLoadedMsg{
		ChannelID: "C1",
		TargetTS:  "1700000020.000000",
		Messages: []messages.MessageItem{
			{TS: "1700000019.000000", Text: "window a"},
			{TS: "1700000020.000000", Text: "window b"},
		},
	})
	return app
}

func messageTSes(app *App) []string {
	var out []string
	for _, m := range app.messagepane.Messages() {
		out = append(out, m.TS)
	}
	return out
}

// A jump window is not live: it stops at the fetched window's newest
// message, so the pane must be anchored for the forward bridge.
func TestMessagesAroundLoaded_AnchorsForForwardFetch(t *testing.T) {
	app := jumpWindowApp(t)
	if got := app.messagepane.NewerAnchorTS(); got != "1700000020.000000" {
		t.Errorf("NewerAnchorTS = %q, want the window's newest ts", got)
	}
}

// A normally-loaded channel already ends at the head, so scrolling to
// the bottom must not fire a pointless history request.
func TestMaybeFetchNewerHistory_NoOpOnLiveBuffer(t *testing.T) {
	app := NewApp()
	app.activeChannelID = "C1"
	app.messagepane.SetMessages([]messages.MessageItem{{TS: "1700000001.000000"}})
	app.setNewerMessagesFetcherForTest(func(ids.ChannelID, ids.MessageTS) tea.Msg {
		t.Fatal("forward bridge fired on a live buffer")
		return nil
	})
	if cmd := app.maybeFetchNewerHistory(true); cmd != nil {
		t.Fatal("expected nil cmd for a live buffer")
	}
}

// Scrolling to the bottom of a jump window fetches the messages just
// after it, keyed to the window's newest ts.
func TestScrollToBottom_BridgesJumpWindowForward(t *testing.T) {
	app := jumpWindowApp(t)
	var gotChannel ids.ChannelID
	var gotAnchor ids.MessageTS
	app.setNewerMessagesFetcherForTest(func(channelID ids.ChannelID, anchorTS ids.MessageTS) tea.Msg {
		gotChannel, gotAnchor = channelID, anchorTS
		return nil
	})

	cmd := app.maybeFetchNewerHistory(true)
	if cmd == nil {
		t.Fatal("expected the bottom of a jump window to dispatch the forward bridge")
	}
	drainBatch(cmd)
	if gotChannel != "C1" || gotAnchor != "1700000020.000000" {
		t.Errorf("fetch keyed to (%q, %q), want (C1, 1700000020.000000)", gotChannel, gotAnchor)
	}
	if !app.fetchingNewer["C1"] {
		t.Error("expected fetchingNewer[C1]=true after dispatch")
	}
	// Second trigger while the first is in flight must not re-fetch.
	if cmd := app.maybeFetchNewerHistory(true); cmd != nil {
		t.Error("in-flight bridge did not gate a second fetch")
	}
}

// The whole point: once the bridge lands, the channel is live again —
// the anchor is gone, so nothing re-fetches and appends behave normally.
func TestNewerMessagesLoaded_MergesAndResumesLive(t *testing.T) {
	app := jumpWindowApp(t)
	app.fetchingNewer["C1"] = true

	// A WebSocket message arriving while the window is stale is glued
	// onto the end with a hole in front of it.
	app.Update(NewMessageMsg{
		ChannelID: "C1",
		Message:   messages.MessageItem{TS: "1700000900.000000", Text: "live"},
	})

	// The bridge fills the hole and covers the glued-on message too.
	app.Update(NewerMessagesLoadedMsg{
		ChannelID: "C1",
		AnchorTS:  "1700000020.000000",
		Messages: []messages.MessageItem{
			{TS: "1700000021.000000", Text: "bridge a"},
			{TS: "1700000900.000000", Text: "live"},
		},
		ReachedHead: true,
	})

	want := []string{"1700000019.000000", "1700000020.000000", "1700000021.000000", "1700000900.000000"}
	got := messageTSes(app)
	if len(got) != len(want) {
		t.Fatalf("buffer = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("buffer = %v, want %v", got, want)
		}
	}
	if app.messagepane.NewerAnchorTS() != "" {
		t.Error("anchor not cleared: channel did not resume live behavior")
	}
	if app.fetchingNewer["C1"] {
		t.Error("fetchingNewer not reset after the bridge landed")
	}

	// Live again: a new message appends and stays appended.
	app.Update(NewMessageMsg{
		ChannelID: "C1",
		Message:   messages.MessageItem{TS: "1700000901.000000", Text: "after live"},
	})
	if got := app.messagepane.NewestTS(); got != "1700000901.000000" {
		t.Errorf("NewestTS = %q, want the newly appended message", got)
	}
}

// Anchor validation, mirroring OlderMessagesLoadedMsg: a bridge keyed to
// a window that has since been replaced must not splice into the new
// buffer.
func TestNewerMessagesLoaded_StaleAnchorDropped(t *testing.T) {
	app := jumpWindowApp(t)
	app.fetchingNewer["C1"] = true

	// A second jump replaces the buffer mid-flight.
	app.Update(MessagesAroundLoadedMsg{
		ChannelID: "C1",
		TargetTS:  "1700000500.000000",
		Messages: []messages.MessageItem{
			{TS: "1700000500.000000", Text: "second window"},
		},
	})

	app.Update(NewerMessagesLoadedMsg{
		ChannelID: "C1",
		AnchorTS:  "1700000020.000000",
		Messages: []messages.MessageItem{
			{TS: "1700000021.000000", Text: "stale bridge"},
		},
		ReachedHead: true,
	})

	if got := messageTSes(app); len(got) != 1 || got[0] != "1700000500.000000" {
		t.Errorf("stale bridge was spliced in: buffer = %v", got)
	}
	if app.messagepane.NewerAnchorTS() != "1700000500.000000" {
		t.Error("second window's anchor was cleared by a stale bridge")
	}
	if app.fetchingNewer["C1"] {
		t.Error("fetchingNewer not reset on stale-anchor drop")
	}
}

// A failed bridge keeps the anchor so scrolling down retries.
func TestNewerMessagesLoaded_ErrorKeepsAnchor(t *testing.T) {
	app := jumpWindowApp(t)
	app.fetchingNewer["C1"] = true

	_, cmd := app.Update(NewerMessagesLoadedMsg{
		ChannelID: "C1",
		AnchorTS:  "1700000020.000000",
		Err:       errors.New("boom"),
	})
	if app.messagepane.NewerAnchorTS() != "1700000020.000000" {
		t.Error("anchor cleared on a failed bridge; forward paging would dead-end")
	}
	if app.fetchingNewer["C1"] {
		t.Error("fetchingNewer not reset after a failed bridge")
	}
	if !hasToast(drainBatch(cmd)) {
		t.Error("expected a toast for the failed bridge")
	}
}

// G is a one-shot "take me to now", so it refetches instead of paging
// through the gap — otherwise a jump far enough back would need one
// keypress per page to reach live.
func TestGoToBottom_OnJumpWindowRefetchesChannel(t *testing.T) {
	app := jumpWindowApp(t)
	app.setChannelLookupFuncForTest(func(ids.ChannelID) (string, string, bool) {
		return "general", "channel", true
	})
	fetched := ""
	app.setChannelFetcherForTest(func(channelID ids.ChannelID, channelName string) tea.Msg {
		fetched = channelName
		return nil
	})
	bridged := false
	app.setNewerMessagesFetcherForTest(func(ids.ChannelID, ids.MessageTS) tea.Msg {
		bridged = true
		return nil
	})

	cmd := handleNormalMode(app, tea.KeyPressMsg{Code: 'G', Text: "G", Mod: tea.ModShift})
	if cmd != nil {
		cmd()
	}
	if fetched != "general" {
		t.Errorf("channel refetched %q; want general", fetched)
	}
	if bridged {
		t.Error("G paged the gap; it should refetch the channel instead")
	}
}

// A budget-exhausted bridge still splices its block in and re-anchors
// on it, so the next scroll continues from there instead of refetching
// the same page or dead-ending.
func TestNewerMessagesLoaded_PartialBlockReAnchors(t *testing.T) {
	app := jumpWindowApp(t)
	app.fetchingNewer["C1"] = true

	app.Update(NewerMessagesLoadedMsg{
		ChannelID: "C1",
		AnchorTS:  "1700000020.000000",
		Messages: []messages.MessageItem{
			{TS: "1700000021.000000", Text: "bridge a"},
			{TS: "1700000022.000000", Text: "bridge b"},
		},
		ReachedHead: false,
	})

	if got := app.messagepane.NewerAnchorTS(); got != "1700000022.000000" {
		t.Errorf("anchor = %q; want the block's newest message", got)
	}
}

func hasToast(msgs []tea.Msg) bool {
	for _, m := range msgs {
		if _, ok := m.(ToastMsg); ok {
			return true
		}
	}
	return false
}

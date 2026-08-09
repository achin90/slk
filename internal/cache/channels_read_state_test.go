package cache

import (
	"testing"
)

func newRSChannel(t *testing.T, db *DB, id, workspaceID string) {
	t.Helper()
	if err := db.UpsertWorkspace(Workspace{ID: workspaceID, Name: "ws"}); err != nil {
		t.Fatalf("UpsertWorkspace: %v", err)
	}
	if err := db.UpsertChannel(Channel{ID: id, WorkspaceID: workspaceID, Name: id, Type: "channel", IsMember: true}); err != nil {
		t.Fatalf("UpsertChannel: %v", err)
	}
}

func TestUpdateChannelReadState_WritesBothColumns(t *testing.T) {
	db, err := New(":memory:")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer db.Close()
	newRSChannel(t, db, "C1", "T1")

	if err := db.UpdateChannelReadState("C1", "1700000000.000001", true); err != nil {
		t.Fatalf("UpdateChannelReadState: %v", err)
	}
	state, err := db.GetChannelReadState("C1")
	if err != nil {
		t.Fatalf("GetChannelReadState: %v", err)
	}
	if state.LastReadTS != "1700000000.000001" {
		t.Errorf("LastReadTS = %q, want %q", state.LastReadTS, "1700000000.000001")
	}
	if !state.HasUnread {
		t.Errorf("HasUnread = false, want true")
	}
}

func TestUpdateChannelReadState_EmptyTSPreservesExisting(t *testing.T) {
	db, err := New(":memory:")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer db.Close()
	newRSChannel(t, db, "C1", "T1")

	if err := db.UpdateChannelReadState("C1", "1700000000.000001", false); err != nil {
		t.Fatalf("first update: %v", err)
	}
	if err := db.UpdateChannelReadState("C1", "", true); err != nil {
		t.Fatalf("second update: %v", err)
	}
	state, err := db.GetChannelReadState("C1")
	if err != nil {
		t.Fatalf("GetChannelReadState: %v", err)
	}
	if state.LastReadTS != "1700000000.000001" {
		t.Errorf("LastReadTS = %q, want preserved %q", state.LastReadTS, "1700000000.000001")
	}
	if !state.HasUnread {
		t.Errorf("HasUnread = false, want true")
	}
}

func TestUpdateChannelReadState_Idempotent(t *testing.T) {
	db, err := New(":memory:")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer db.Close()
	newRSChannel(t, db, "C1", "T1")

	for i := 0; i < 3; i++ {
		if err := db.UpdateChannelReadState("C1", "1700000000.000001", true); err != nil {
			t.Fatalf("iter %d: %v", i, err)
		}
	}
	state, _ := db.GetChannelReadState("C1")
	if state.LastReadTS != "1700000000.000001" || !state.HasUnread {
		t.Errorf("state = %+v after 3 writes", state)
	}
}

func TestBatchUpdateChannelReadState_WritesAll(t *testing.T) {
	db, err := New(":memory:")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer db.Close()
	newRSChannel(t, db, "C1", "T1")
	newRSChannel(t, db, "C2", "T1")
	newRSChannel(t, db, "C3", "T1")

	updates := []ChannelReadStateUpdate{
		{ChannelID: "C1", LastReadTS: "1.0001", HasUnread: true},
		{ChannelID: "C2", LastReadTS: "1.0002", HasUnread: false},
		{ChannelID: "C3", LastReadTS: "", HasUnread: true},
	}
	if err := db.BatchUpdateChannelReadState(updates); err != nil {
		t.Fatalf("BatchUpdateChannelReadState: %v", err)
	}
	s1, _ := db.GetChannelReadState("C1")
	if s1.LastReadTS != "1.0001" || !s1.HasUnread {
		t.Errorf("C1 = %+v", s1)
	}
	s2, _ := db.GetChannelReadState("C2")
	if s2.LastReadTS != "1.0002" || s2.HasUnread {
		t.Errorf("C2 = %+v", s2)
	}
	s3, _ := db.GetChannelReadState("C3")
	if s3.LastReadTS != "" || !s3.HasUnread {
		t.Errorf("C3 = %+v (LastReadTS should be preserved empty)", s3)
	}
}

func TestBatchUpdateChannelReadState_Transactional(t *testing.T) {
	db, err := New(":memory:")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer db.Close()
	newRSChannel(t, db, "C1", "T1")

	// Seed C1
	if err := db.UpdateChannelReadState("C1", "1.0", false); err != nil {
		t.Fatalf("seed: %v", err)
	}

	// Empty batch is a no-op and returns nil.
	if err := db.BatchUpdateChannelReadState(nil); err != nil {
		t.Errorf("nil batch: %v", err)
	}
	if err := db.BatchUpdateChannelReadState([]ChannelReadStateUpdate{}); err != nil {
		t.Errorf("empty batch: %v", err)
	}
	// Original state untouched
	s, _ := db.GetChannelReadState("C1")
	if s.LastReadTS != "1.0" || s.HasUnread {
		t.Errorf("after empty batch C1 = %+v", s)
	}
}

func TestGetWorkspaceReadState_ReturnsAllChannels(t *testing.T) {
	db, err := New(":memory:")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer db.Close()
	newRSChannel(t, db, "C1", "T1")
	newRSChannel(t, db, "C2", "T1")
	newRSChannel(t, db, "C3", "T1")
	newRSChannel(t, db, "C4", "T2") // different workspace

	_ = db.UpdateChannelReadState("C1", "1.0001", true)
	_ = db.UpdateChannelReadState("C2", "1.0002", false)
	// C3 untouched — defaults

	got, err := db.GetWorkspaceReadState("T1")
	if err != nil {
		t.Fatalf("GetWorkspaceReadState: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("got %d entries, want 3 (C3 must be included with defaults): %+v", len(got), got)
	}
	if got["C1"].LastReadTS != "1.0001" || !got["C1"].HasUnread {
		t.Errorf("C1 = %+v", got["C1"])
	}
	if got["C2"].LastReadTS != "1.0002" || got["C2"].HasUnread {
		t.Errorf("C2 = %+v", got["C2"])
	}
	if got["C3"].LastReadTS != "" || got["C3"].HasUnread {
		t.Errorf("C3 default = %+v", got["C3"])
	}
	if _, ok := got["C4"]; ok {
		t.Errorf("C4 from other workspace should not be returned")
	}
}

func TestWorkspacesWithUnreads(t *testing.T) {
	db, err := New(":memory:")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer db.Close()
	newRSChannel(t, db, "C1", "T1")
	newRSChannel(t, db, "C2", "T2")
	newRSChannel(t, db, "C3", "T3")

	_ = db.UpdateChannelReadState("C1", "1.0", true)
	_ = db.UpdateChannelReadState("C3", "1.0", true)
	// C2/T2 has no unreads

	got, err := db.WorkspacesWithUnreads()
	if err != nil {
		t.Fatalf("WorkspacesWithUnreads: %v", err)
	}
	want := map[string]bool{"T1": true, "T3": true}
	if len(got) != 2 {
		t.Fatalf("got %d ids, want 2: %v", len(got), got)
	}
	for _, id := range got {
		if !want[id] {
			t.Errorf("unexpected workspace %q", id)
		}
	}
}

func TestReplaceWorkspaceReadState_ClearsChannelsAbsentFromSnapshot(t *testing.T) {
	db, err := New(":memory:")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer db.Close()
	newRSChannel(t, db, "C1", "T1")
	newRSChannel(t, db, "C2", "T1")
	newRSChannel(t, db, "C3", "T1")
	newRSChannel(t, db, "D1", "T2") // different workspace

	// Prior-session state: C1, C2 unread; D1 (other workspace) unread.
	_ = db.UpdateChannelReadState("C1", "1.0001", true)
	_ = db.UpdateChannelReadState("C2", "1.0002", true)
	_ = db.UpdateChannelReadState("D1", "1.0003", true)

	// Fresh authoritative snapshot for T1: only C3 is unread now.
	// C1 was read in the official client while slk was closed and is
	// therefore ABSENT from the snapshot; it must be cleared. C2 is
	// explicitly reported read. C3 becomes unread.
	updates := []ChannelReadStateUpdate{
		{ChannelID: "C2", LastReadTS: "1.0100", HasUnread: false},
		{ChannelID: "C3", LastReadTS: "1.0101", HasUnread: true},
	}
	if err := db.ReplaceWorkspaceReadState("T1", updates); err != nil {
		t.Fatalf("ReplaceWorkspaceReadState: %v", err)
	}

	s1, _ := db.GetChannelReadState("C1")
	if s1.HasUnread {
		t.Errorf("C1 HasUnread = true; absent-from-snapshot channel must be cleared (Symptom 2)")
	}
	// last_read_ts of an absent channel is left untouched (no fresh value to apply).
	if s1.LastReadTS != "1.0001" {
		t.Errorf("C1 LastReadTS = %q, want preserved %q", s1.LastReadTS, "1.0001")
	}
	s2, _ := db.GetChannelReadState("C2")
	if s2.HasUnread || s2.LastReadTS != "1.0100" {
		t.Errorf("C2 = %+v, want {1.0100 false}", s2)
	}
	s3, _ := db.GetChannelReadState("C3")
	if !s3.HasUnread || s3.LastReadTS != "1.0101" {
		t.Errorf("C3 = %+v, want {1.0101 true}", s3)
	}

	// Other workspace is untouched by a T1 replace.
	d1, _ := db.GetChannelReadState("D1")
	if !d1.HasUnread || d1.LastReadTS != "1.0003" {
		t.Errorf("D1 (other workspace) = %+v, want {1.0003 true} untouched", d1)
	}
}

func TestReplaceWorkspaceReadState_EmptySnapshotClearsAll(t *testing.T) {
	db, err := New(":memory:")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer db.Close()
	newRSChannel(t, db, "C1", "T1")
	newRSChannel(t, db, "C2", "T1")
	_ = db.UpdateChannelReadState("C1", "1.0", true)
	_ = db.UpdateChannelReadState("C2", "1.0", true)

	// A successful client.counts call that reports zero unreads means
	// everything is read.
	if err := db.ReplaceWorkspaceReadState("T1", nil); err != nil {
		t.Fatalf("ReplaceWorkspaceReadState: %v", err)
	}
	for _, id := range []string{"C1", "C2"} {
		s, _ := db.GetChannelReadState(id)
		if s.HasUnread {
			t.Errorf("%s HasUnread = true after empty snapshot; want cleared", id)
		}
	}
}

func TestUpsertChannel_DoesNotClobberReadState(t *testing.T) {
	db, err := New(":memory:")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer db.Close()
	newRSChannel(t, db, "C1", "T1")

	// Set read state.
	if err := db.UpdateChannelReadState("C1", "1700000000.000001", true); err != nil {
		t.Fatalf("UpdateChannelReadState: %v", err)
	}

	// Re-upsert the channel with zero-value LastReadTS/UnreadCount.
	// This mirrors what bootstrap (upsertChannelInDB) does today.
	if err := db.UpsertChannel(Channel{
		ID:          "C1",
		WorkspaceID: "T1",
		Name:        "renamed",
		Type:        "channel",
	}); err != nil {
		t.Fatalf("re-upsert: %v", err)
	}

	state, err := db.GetChannelReadState("C1")
	if err != nil {
		t.Fatalf("GetChannelReadState: %v", err)
	}
	if state.LastReadTS != "1700000000.000001" {
		t.Errorf("LastReadTS = %q, want preserved %q (clobber regression!)", state.LastReadTS, "1700000000.000001")
	}
	if !state.HasUnread {
		t.Errorf("HasUnread = false after upsert (clobber regression!)")
	}
}

func TestIncrementChannelMentionCount(t *testing.T) {
	db, err := New(":memory:")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer db.Close()
	newRSChannel(t, db, "C1", "T1")
	newRSChannel(t, db, "C2", "T1")

	// Increment C1 twice, C2 once.
	if err := db.IncrementChannelMentionCount("C1", 1); err != nil {
		t.Fatalf("increment C1: %v", err)
	}
	if err := db.IncrementChannelMentionCount("C1", 1); err != nil {
		t.Fatalf("increment C1 again: %v", err)
	}
	if err := db.IncrementChannelMentionCount("C2", 1); err != nil {
		t.Fatalf("increment C2: %v", err)
	}

	// Verify per-channel counts.
	s1, _ := db.GetChannelReadState("C1")
	if s1.MentionCount != 2 {
		t.Errorf("C1 MentionCount = %d want 2", s1.MentionCount)
	}
	if !s1.HasUnread {
		t.Errorf("C1 HasUnread = false after mention increment")
	}
	s2, _ := db.GetChannelReadState("C2")
	if s2.MentionCount != 1 {
		t.Errorf("C2 MentionCount = %d want 1", s2.MentionCount)
	}

	// Workspace total.
	total, err := db.MentionCountForWorkspace("T1")
	if err != nil {
		t.Fatalf("MentionCountForWorkspace: %v", err)
	}
	if total != 3 {
		t.Errorf("workspace mention total = %d want 3", total)
	}

	// Reading C1 resets its mention_count to 0.
	if err := db.UpdateChannelReadState("C1", "1.0", false); err != nil {
		t.Fatalf("UpdateChannelReadState read: %v", err)
	}
	s1, _ = db.GetChannelReadState("C1")
	if s1.MentionCount != 0 {
		t.Errorf("C1 MentionCount after read = %d want 0", s1.MentionCount)
	}
	total, _ = db.MentionCountForWorkspace("T1")
	if total != 1 {
		t.Errorf("workspace mention total after reading C1 = %d want 1", total)
	}
}

func TestReplaceWorkspaceReadState_WritesMentionCount(t *testing.T) {
	db, err := New(":memory:")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer db.Close()
	newRSChannel(t, db, "C1", "T1")
	newRSChannel(t, db, "C2", "T1")

	// Bootstrap snapshot with mention counts.
	updates := []ChannelReadStateUpdate{
		{ChannelID: "C1", LastReadTS: "1.0", HasUnread: true, MentionCount: 3},
		{ChannelID: "C2", LastReadTS: "2.0", HasUnread: true, MentionCount: 0},
	}
	if err := db.ReplaceWorkspaceReadState("T1", updates); err != nil {
		t.Fatalf("ReplaceWorkspaceReadState: %v", err)
	}

	s1, _ := db.GetChannelReadState("C1")
	if s1.MentionCount != 3 {
		t.Errorf("C1 MentionCount = %d want 3", s1.MentionCount)
	}

	// Total should be 3 (C1 has 3, C2 has 0).
	total, _ := db.MentionCountForWorkspace("T1")
	if total != 3 {
		t.Errorf("workspace mention total = %d want 3", total)
	}

	// A second replace resets everything and only sets C1.
	updates2 := []ChannelReadStateUpdate{
		{ChannelID: "C1", LastReadTS: "1.5", HasUnread: true, MentionCount: 5},
	}
	if err := db.ReplaceWorkspaceReadState("T1", updates2); err != nil {
		t.Fatalf("ReplaceWorkspaceReadState 2: %v", err)
	}
	s1, _ = db.GetChannelReadState("C1")
	if s1.MentionCount != 5 {
		t.Errorf("C1 MentionCount after second replace = %d want 5", s1.MentionCount)
	}
	// C2 was absent from updates2, so it's reset.
	s2, _ := db.GetChannelReadState("C2")
	if s2.HasUnread {
		t.Errorf("C2 should be read after replace without it in updates")
	}
	if s2.MentionCount != 0 {
		t.Errorf("C2 MentionCount = %d want 0 (reset by replace)", s2.MentionCount)
	}
}

func TestBatchUpdateChannelReadState_PreservesMentionCount(t *testing.T) {
	db, err := New(":memory:")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer db.Close()
	newRSChannel(t, db, "C1", "T1")

	// Set initial mention_count via a batch with explicit count.
	if err := db.BatchUpdateChannelReadState([]ChannelReadStateUpdate{
		{ChannelID: "C1", LastReadTS: "1.0", HasUnread: true, MentionCount: 3},
	}); err != nil {
		t.Fatalf("batch 1: %v", err)
	}
	s, _ := db.GetChannelReadState("C1")
	if s.MentionCount != 3 {
		t.Fatalf("initial MentionCount = %d want 3", s.MentionCount)
	}

	// Batch with MentionCount=-1 preserves the existing count.
	if err := db.BatchUpdateChannelReadState([]ChannelReadStateUpdate{
		{ChannelID: "C1", LastReadTS: "2.0", HasUnread: true, MentionCount: -1},
	}); err != nil {
		t.Fatalf("batch 2: %v", err)
	}
	s, _ = db.GetChannelReadState("C1")
	if s.MentionCount != 3 {
		t.Errorf("C1 MentionCount after preserve = %d want 3 (preserved)", s.MentionCount)
	}
	if s.LastReadTS != "2.0" {
		t.Errorf("C1 LastReadTS = %q want 2.0 (updated)", s.LastReadTS)
	}
}

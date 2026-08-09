package cache

import (
	"reflect"
	"testing"
)

func TestSaveAndLoadCustomEmojis(t *testing.T) {
	db := setupDBWithWorkspace(t)
	defer db.Close()

	emojis := map[string]string{
		"partyparrot":  "https://emoji.example.com/partyparrot.gif",
		"shipit":       "alias:rocket",
		"custom_thing": "https://emoji.example.com/custom.png",
	}
	if err := db.SaveCustomEmojis("T1", emojis); err != nil {
		t.Fatalf("SaveCustomEmojis: %v", err)
	}

	loaded, err := db.LoadCustomEmojis("T1")
	if err != nil {
		t.Fatalf("LoadCustomEmojis: %v", err)
	}
	if !reflect.DeepEqual(loaded, emojis) {
		t.Errorf("loaded = %#v; want %#v", loaded, emojis)
	}
}

func TestLoadCustomEmojisEmpty(t *testing.T) {
	db := setupDBWithWorkspace(t)
	defer db.Close()

	loaded, err := db.LoadCustomEmojis("T1")
	if err != nil {
		t.Fatalf("LoadCustomEmojis: %v", err)
	}
	if len(loaded) != 0 {
		t.Errorf("expected empty map, got %d entries", len(loaded))
	}
}

func TestSaveCustomEmojisReplacesExisting(t *testing.T) {
	db := setupDBWithWorkspace(t)
	defer db.Close()

	first := map[string]string{
		"emoji_a": "https://example.com/a.png",
		"emoji_b": "https://example.com/b.png",
	}
	if err := db.SaveCustomEmojis("T1", first); err != nil {
		t.Fatalf("SaveCustomEmojis (first): %v", err)
	}

	second := map[string]string{
		"emoji_c": "https://example.com/c.png",
	}
	if err := db.SaveCustomEmojis("T1", second); err != nil {
		t.Fatalf("SaveCustomEmojis (second): %v", err)
	}

	loaded, err := db.LoadCustomEmojis("T1")
	if err != nil {
		t.Fatalf("LoadCustomEmojis: %v", err)
	}
	if !reflect.DeepEqual(loaded, second) {
		t.Errorf("loaded = %#v; want %#v (old entries should be gone)", loaded, second)
	}
}

func TestSaveCustomEmojisPerWorkspace(t *testing.T) {
	db := setupDBWithWorkspace(t)
	defer db.Close()
	db.UpsertWorkspace(Workspace{ID: "T2", Name: "Other", Domain: "other"})

	if err := db.SaveCustomEmojis("T1", map[string]string{"shared": "url_t1"}); err != nil {
		t.Fatalf("SaveCustomEmojis T1: %v", err)
	}
	if err := db.SaveCustomEmojis("T2", map[string]string{"shared": "url_t2"}); err != nil {
		t.Fatalf("SaveCustomEmojis T2: %v", err)
	}

	t1, err := db.LoadCustomEmojis("T1")
	if err != nil {
		t.Fatalf("LoadCustomEmojis T1: %v", err)
	}
	if t1["shared"] != "url_t1" {
		t.Errorf("T1[shared] = %q; want url_t1", t1["shared"])
	}

	t2, err := db.LoadCustomEmojis("T2")
	if err != nil {
		t.Fatalf("LoadCustomEmojis T2: %v", err)
	}
	if t2["shared"] != "url_t2" {
		t.Errorf("T2[shared] = %q; want url_t2", t2["shared"])
	}
}

func TestEmojiCacheTSRoundTrip(t *testing.T) {
	db := setupDBWithWorkspace(t)
	defer db.Close()

	// Empty before any save.
	ts, err := db.GetEmojiCacheTS("T1")
	if err != nil {
		t.Fatalf("GetEmojiCacheTS (initial): %v", err)
	}
	if ts != "" {
		t.Errorf("expected empty ts initially, got %q", ts)
	}

	want := "17833375330191742"
	if err := db.SaveEmojiCacheTS("T1", want); err != nil {
		t.Fatalf("SaveEmojiCacheTS: %v", err)
	}

	got, err := db.GetEmojiCacheTS("T1")
	if err != nil {
		t.Fatalf("GetEmojiCacheTS: %v", err)
	}
	if got != want {
		t.Errorf("got %q; want %q", got, want)
	}

	// Upsert overwrites.
	updated := "17999999999999999"
	if err := db.SaveEmojiCacheTS("T1", updated); err != nil {
		t.Fatalf("SaveEmojiCacheTS (update): %v", err)
	}
	got, err = db.GetEmojiCacheTS("T1")
	if err != nil {
		t.Fatalf("GetEmojiCacheTS (after update): %v", err)
	}
	if got != updated {
		t.Errorf("got %q; want %q", got, updated)
	}
}

func TestGetEmojiCacheTSMissingWorkspace(t *testing.T) {
	db := setupDBWithWorkspace(t)
	defer db.Close()

	ts, err := db.GetEmojiCacheTS("NOPE")
	if err != nil {
		t.Fatalf("GetEmojiCacheTS: %v", err)
	}
	if ts != "" {
		t.Errorf("expected empty ts for unknown workspace, got %q", ts)
	}
}

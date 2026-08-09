package cache

import (
	"database/sql"
	"fmt"
)

// SaveCustomEmojis replaces the full custom-emoji set for a workspace
// in a single transaction. Callers pass the authoritative map returned
// by Slack's emoji.list (name -> URL or "alias:target").
func (db *DB) SaveCustomEmojis(workspaceID string, emojis map[string]string) error {
	tx, err := db.conn.Begin()
	if err != nil {
		return fmt.Errorf("beginning custom_emoji tx: %w", err)
	}
	defer tx.Rollback()

	if _, err := tx.Exec(`DELETE FROM custom_emoji WHERE workspace_id = ?`, workspaceID); err != nil {
		return fmt.Errorf("clearing custom_emoji: %w", err)
	}

	stmt, err := tx.Prepare(`INSERT INTO custom_emoji (workspace_id, name, value) VALUES (?, ?, ?)`)
	if err != nil {
		return fmt.Errorf("preparing custom_emoji insert: %w", err)
	}
	defer stmt.Close()

	for name, value := range emojis {
		if name == "" {
			continue
		}
		if _, err := stmt.Exec(workspaceID, name, value); err != nil {
			return fmt.Errorf("inserting custom_emoji %q: %w", name, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("committing custom_emoji tx: %w", err)
	}
	return nil
}

// LoadCustomEmojis reads the persisted custom-emoji map for a workspace.
// Returns an empty (non-nil) map when no rows exist.
func (db *DB) LoadCustomEmojis(workspaceID string) (map[string]string, error) {
	rows, err := db.conn.Query(`
		SELECT name, value FROM custom_emoji WHERE workspace_id = ?
	`, workspaceID)
	if err != nil {
		return nil, fmt.Errorf("querying custom_emoji: %w", err)
	}
	defer rows.Close()

	out := make(map[string]string)
	for rows.Next() {
		var name, value string
		if err := rows.Scan(&name, &value); err != nil {
			return nil, fmt.Errorf("scanning custom_emoji row: %w", err)
		}
		out[name] = value
	}
	return out, rows.Err()
}

// GetEmojiCacheTS returns the persisted emoji_cache_ts token for a
// workspace, or "" if never saved. This token is echoed back to Slack
// on subsequent emoji.list calls to get an incremental response.
func (db *DB) GetEmojiCacheTS(workspaceID string) (string, error) {
	var ts string
	err := db.conn.QueryRow(`
		SELECT emoji_cache_ts FROM custom_emoji_meta WHERE workspace_id = ?
	`, workspaceID).Scan(&ts)
	if err == sql.ErrNoRows {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("querying emoji_cache_ts: %w", err)
	}
	return ts, nil
}

// SaveEmojiCacheTS upserts the emoji_cache_ts token for a workspace.
func (db *DB) SaveEmojiCacheTS(workspaceID, ts string) error {
	_, err := db.conn.Exec(`
		INSERT INTO custom_emoji_meta (workspace_id, emoji_cache_ts)
		VALUES (?, ?)
		ON CONFLICT(workspace_id) DO UPDATE SET emoji_cache_ts = excluded.emoji_cache_ts
	`, workspaceID, ts)
	if err != nil {
		return fmt.Errorf("saving emoji_cache_ts: %w", err)
	}
	return nil
}

package cache

import (
	"database/sql"
	"fmt"
)

// ReadState captures the per-channel read-state values that drive the
// unread dot and "new messages" line. It is the canonical type for
// passing read state across package boundaries.
type ReadState struct {
	LastReadTS   string
	HasUnread    bool
	MentionCount int
}

// ChannelReadStateUpdate is one entry in a batched read-state write.
// LastReadTS == "" means "preserve the existing last_read_ts" (used by
// events that update has_unread only, e.g. new-message arrivals).
// MentionCount == -1 means "preserve the existing mention_count" (used
// by events that only touch has_unread/last_read_ts, e.g.
// new-message arrivals that set has_unread=true without knowing the
// mention count).
type ChannelReadStateUpdate struct {
	ChannelID    string
	LastReadTS   string
	HasUnread    bool
	MentionCount int
}

// UpdateChannelReadState atomically updates the per-channel read state.
// If lastReadTS == "", the existing last_read_ts is preserved. This is
// the ONLY function permitted to modify read state after bootstrap.
//
// When hasUnread is false (channel is being marked read), mention_count
// is also reset to 0 — reading a channel clears its mentions. When
// hasUnread is true, mention_count is preserved (the caller does not
// know the new mention count in this path; use
// IncrementChannelMentionCount for that).
func (db *DB) UpdateChannelReadState(channelID, lastReadTS string, hasUnread bool) error {
	var q string
	var args []any
	if lastReadTS == "" {
		if hasUnread {
			q = `UPDATE channels SET has_unread = ? WHERE id = ?`
			args = []any{boolToInt(hasUnread), channelID}
		} else {
			q = `UPDATE channels SET has_unread = ?, mention_count = 0 WHERE id = ?`
			args = []any{boolToInt(hasUnread), channelID}
		}
	} else {
		if hasUnread {
			q = `UPDATE channels SET last_read_ts = ?, has_unread = ? WHERE id = ?`
			args = []any{lastReadTS, boolToInt(hasUnread), channelID}
		} else {
			q = `UPDATE channels SET last_read_ts = ?, has_unread = ?, mention_count = 0 WHERE id = ?`
			args = []any{lastReadTS, boolToInt(hasUnread), channelID}
		}
	}
	if _, err := db.conn.Exec(q, args...); err != nil {
		return fmt.Errorf("updating channel read state: %w", err)
	}
	return nil
}

// IncrementChannelMentionCount atomically increments the mention_count
// for a channel by delta. Used by OnMessage when an incoming message
// triggers a notification (mention, DM, keyword hit). Also sets
// has_unread=1 since a mentioning message is by definition unread.
func (db *DB) IncrementChannelMentionCount(channelID string, delta int) error {
	if delta == 0 {
		return nil
	}
	if _, err := db.conn.Exec(
		`UPDATE channels SET mention_count = mention_count + ?, has_unread = 1 WHERE id = ?`,
		delta, channelID,
	); err != nil {
		return fmt.Errorf("incrementing mention count for %s: %w", channelID, err)
	}
	return nil
}

// BatchUpdateChannelReadState writes multiple updates in a single
// transaction. Used by bootstrap and reconnect catch-up paths.
// MentionCount == -1 in an update means "preserve existing mention_count";
// any other value (including 0) is written.
func (db *DB) BatchUpdateChannelReadState(updates []ChannelReadStateUpdate) error {
	if len(updates) == 0 {
		return nil
	}
	tx, err := db.conn.Begin()
	if err != nil {
		return fmt.Errorf("begin batch read-state tx: %w", err)
	}
	// stmtFull: last_read_ts + has_unread + mention_count
	stmtFull, err := tx.Prepare(`UPDATE channels SET last_read_ts = ?, has_unread = ?, mention_count = ? WHERE id = ?`)
	if err != nil {
		tx.Rollback()
		return fmt.Errorf("prepare full: %w", err)
	}
	defer stmtFull.Close()
	// stmtFullPreserve: last_read_ts + has_unread, preserve mention_count
	stmtFullPreserve, err := tx.Prepare(`UPDATE channels SET last_read_ts = ?, has_unread = ? WHERE id = ?`)
	if err != nil {
		tx.Rollback()
		return fmt.Errorf("prepare full-preserve: %w", err)
	}
	defer stmtFullPreserve.Close()
	// stmtFlag: has_unread + mention_count, preserve last_read_ts
	stmtFlag, err := tx.Prepare(`UPDATE channels SET has_unread = ?, mention_count = ? WHERE id = ?`)
	if err != nil {
		tx.Rollback()
		return fmt.Errorf("prepare flag: %w", err)
	}
	defer stmtFlag.Close()
	// stmtFlagPreserve: has_unread only, preserve last_read_ts + mention_count
	stmtFlagPreserve, err := tx.Prepare(`UPDATE channels SET has_unread = ? WHERE id = ?`)
	if err != nil {
		tx.Rollback()
		return fmt.Errorf("prepare flag-preserve: %w", err)
	}
	defer stmtFlagPreserve.Close()

	for _, u := range updates {
		preserveMention := u.MentionCount == -1
		if u.LastReadTS == "" {
			if preserveMention {
				if _, err := stmtFlagPreserve.Exec(boolToInt(u.HasUnread), u.ChannelID); err != nil {
					tx.Rollback()
					return fmt.Errorf("batch flag-preserve for %s: %w", u.ChannelID, err)
				}
			} else {
				if _, err := stmtFlag.Exec(boolToInt(u.HasUnread), u.MentionCount, u.ChannelID); err != nil {
					tx.Rollback()
					return fmt.Errorf("batch flag for %s: %w", u.ChannelID, err)
				}
			}
		} else {
			if preserveMention {
				if _, err := stmtFullPreserve.Exec(u.LastReadTS, boolToInt(u.HasUnread), u.ChannelID); err != nil {
					tx.Rollback()
					return fmt.Errorf("batch full-preserve for %s: %w", u.ChannelID, err)
				}
			} else {
				if _, err := stmtFull.Exec(u.LastReadTS, boolToInt(u.HasUnread), u.MentionCount, u.ChannelID); err != nil {
					tx.Rollback()
					return fmt.Errorf("batch full for %s: %w", u.ChannelID, err)
				}
			}
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit batch read-state: %w", err)
	}
	return nil
}

// ReplaceWorkspaceReadState applies an authoritative full snapshot of
// unread state for a workspace. In a single transaction it first
// resets has_unread=0 and mention_count=0 for EVERY channel in the
// workspace, then applies the given updates (has_unread + last_read_ts
// + mention_count). Channels absent from updates are therefore treated
// as read with zero mentions.
//
// This is the boot/bootstrap path. Unlike BatchUpdateChannelReadState
// (which only touches rows named in the batch), the reset step clears
// stale unread flags for channels the user read in another client
// while slk was closed — channels that Slack's client.counts response
// omits entirely (the unread-counts endpoint need not list read
// channels). last_read_ts of an absent channel is left untouched:
// there is no fresh value to apply and has_unread=0 already suppresses
// the dot.
//
// MentionCount == -1 in an update means "preserve existing
// mention_count"; any other value is written. Bootstrap uses the
// actual mention_count from client.counts, so -1 is rarely needed
// here.
//
// Callers MUST only invoke this with a snapshot they trust to list
// every currently-unread channel (i.e. a successful client.counts
// call). On fetch failure, do NOT call this — a reset with no data
// would wrongly clear every dot.
func (db *DB) ReplaceWorkspaceReadState(workspaceID string, updates []ChannelReadStateUpdate) error {
	if workspaceID == "" {
		return fmt.Errorf("ReplaceWorkspaceReadState: workspaceID required")
	}
	tx, err := db.conn.Begin()
	if err != nil {
		return fmt.Errorf("begin replace read-state tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	if _, err := tx.Exec(
		`UPDATE channels SET has_unread = 0, mention_count = 0 WHERE workspace_id = ?`,
		workspaceID,
	); err != nil {
		return fmt.Errorf("reset workspace unread: %w", err)
	}

	// stmtFull: last_read_ts + has_unread + mention_count
	stmtFull, err := tx.Prepare(`UPDATE channels SET last_read_ts = ?, has_unread = ?, mention_count = ? WHERE id = ?`)
	if err != nil {
		return fmt.Errorf("prepare full: %w", err)
	}
	defer stmtFull.Close()
	// stmtFullPreserve: last_read_ts + has_unread, preserve mention_count
	stmtFullPreserve, err := tx.Prepare(`UPDATE channels SET last_read_ts = ?, has_unread = ? WHERE id = ?`)
	if err != nil {
		return fmt.Errorf("prepare full-preserve: %w", err)
	}
	defer stmtFullPreserve.Close()
	// stmtFlag: has_unread + mention_count, preserve last_read_ts
	stmtFlag, err := tx.Prepare(`UPDATE channels SET has_unread = ?, mention_count = ? WHERE id = ?`)
	if err != nil {
		return fmt.Errorf("prepare flag: %w", err)
	}
	defer stmtFlag.Close()
	// stmtFlagPreserve: has_unread only, preserve last_read_ts + mention_count
	stmtFlagPreserve, err := tx.Prepare(`UPDATE channels SET has_unread = ? WHERE id = ?`)
	if err != nil {
		return fmt.Errorf("prepare flag-preserve: %w", err)
	}
	defer stmtFlagPreserve.Close()

	for _, u := range updates {
		preserveMention := u.MentionCount == -1
		if u.LastReadTS == "" {
			if preserveMention {
				if _, err := stmtFlagPreserve.Exec(boolToInt(u.HasUnread), u.ChannelID); err != nil {
					return fmt.Errorf("replace flag-preserve for %s: %w", u.ChannelID, err)
				}
			} else {
				if _, err := stmtFlag.Exec(boolToInt(u.HasUnread), u.MentionCount, u.ChannelID); err != nil {
					return fmt.Errorf("replace flag for %s: %w", u.ChannelID, err)
				}
			}
		} else {
			if preserveMention {
				if _, err := stmtFullPreserve.Exec(u.LastReadTS, boolToInt(u.HasUnread), u.ChannelID); err != nil {
					return fmt.Errorf("replace full-preserve for %s: %w", u.ChannelID, err)
				}
			} else {
				if _, err := stmtFull.Exec(u.LastReadTS, boolToInt(u.HasUnread), u.MentionCount, u.ChannelID); err != nil {
					return fmt.Errorf("replace full for %s: %w", u.ChannelID, err)
				}
			}
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit replace read-state: %w", err)
	}
	return nil
}

// GetChannelReadState returns the read state for a single channel.
// A missing row yields a zero-valued ReadState and a nil error.
func (db *DB) GetChannelReadState(channelID string) (ReadState, error) {
	var lastReadTS string
	var hasUnread, mentionCount int
	err := db.conn.QueryRow(
		`SELECT last_read_ts, has_unread, mention_count FROM channels WHERE id = ?`,
		channelID,
	).Scan(&lastReadTS, &hasUnread, &mentionCount)
	if err == sql.ErrNoRows {
		return ReadState{}, nil
	}
	if err != nil {
		return ReadState{}, fmt.Errorf("getting channel read state: %w", err)
	}
	return ReadState{LastReadTS: lastReadTS, HasUnread: hasUnread == 1, MentionCount: mentionCount}, nil
}

// GetWorkspaceReadState returns channelID -> ReadState for every
// channel in the workspace. Single batched query. Called by the
// sidebar View() at render time.
func (db *DB) GetWorkspaceReadState(workspaceID string) (map[string]ReadState, error) {
	rows, err := db.conn.Query(
		`SELECT id, last_read_ts, has_unread, mention_count FROM channels WHERE workspace_id = ?`,
		workspaceID,
	)
	if err != nil {
		return nil, fmt.Errorf("query workspace read state: %w", err)
	}
	defer rows.Close()
	out := make(map[string]ReadState)
	for rows.Next() {
		var id, lastRead string
		var hasUnread, mentionCount int
		if err := rows.Scan(&id, &lastRead, &hasUnread, &mentionCount); err != nil {
			return nil, fmt.Errorf("scan workspace read state: %w", err)
		}
		out[id] = ReadState{LastReadTS: lastRead, HasUnread: hasUnread == 1, MentionCount: mentionCount}
	}
	return out, rows.Err()
}

// WorkspacesWithUnreads returns the set of workspace IDs with at least
// one has_unread=true channel. Used by the workspace rail.
func (db *DB) WorkspacesWithUnreads() ([]string, error) {
	rows, err := db.conn.Query(
		`SELECT DISTINCT workspace_id FROM channels WHERE has_unread = 1`,
	)
	if err != nil {
		return nil, fmt.Errorf("query workspaces with unreads: %w", err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan workspace id: %w", err)
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// MentionCountForWorkspace returns the total mention_count across all
// channels in the workspace that have has_unread=1. This is the
// "dock badge number" — the count of DMs, @mentions, @here/@channel,
// and keyword hits that need a response. Muted channels are excluded,
// matching Slack's behavior.
func (db *DB) MentionCountForWorkspace(workspaceID string) (int, error) {
	var total int
	err := db.conn.QueryRow(
		`SELECT COALESCE(SUM(mention_count), 0) FROM channels WHERE workspace_id = ? AND has_unread = 1 AND is_member = 1`,
		workspaceID,
	).Scan(&total)
	if err != nil {
		return 0, fmt.Errorf("query mention count for workspace %s: %w", workspaceID, err)
	}
	return total, nil
}

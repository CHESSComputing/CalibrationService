package main

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	_ "github.com/jackc/pgx/v5/stdlib" // registers the "pgx" database/sql driver
)

// Sentinel errors surfaced to the handler layer.
var (
	ErrOverlap  = errors.New("interval of validity overlaps an existing active IOV for this tag/channel")
	ErrNotFound = errors.New("not found")
)

// Store wraps a database/sql handle with the calibration-specific queries.
type Store struct {
	db *sql.DB
}

// NewStore opens (and pings) a Postgres connection pool.
func NewStore(dsn string) (*Store, error) {
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}
	db.SetMaxOpenConns(20)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(30 * time.Minute)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		return nil, fmt.Errorf("ping db: %w", err)
	}
	return &Store{db: db}, nil
}

func (s *Store) Close() error { return s.db.Close() }

func checksum(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// isExclusionViolation reports whether err is a Postgres exclusion-constraint
// violation (SQLSTATE 23P01) - i.e. the "no_overlap" constraint on iovs fired.
func isExclusionViolation(err error) bool {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code == "23P01"
	}
	return false
}

// GetOrCreateTag returns the id of an existing tag, creating it if absent.
func (s *Store) GetOrCreateTag(ctx context.Context, name, description string) (int64, error) {
	var id int64
	err := s.db.QueryRowContext(ctx, `SELECT id FROM tags WHERE name = $1`, name).Scan(&id)
	if err == nil {
		return id, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return 0, fmt.Errorf("lookup tag: %w", err)
	}
	err = s.db.QueryRowContext(ctx,
		`INSERT INTO tags (name, description) VALUES ($1, $2) RETURNING id`,
		name, description,
	).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("insert tag: %w", err)
	}
	return id, nil
}

// InsertCalibration writes a new payload and a new IOV row for it in a single
// transaction. The "no_overlap" exclusion constraint on iovs is what actually
// prevents two active, overlapping validity ranges for the same tag+channel;
// a violation is translated into ErrOverlap.
func (s *Store) InsertCalibration(ctx context.Context, req CreateCalibrationRequest) (*IOV, error) {
	if req.Since >= req.Till {
		return nil, fmt.Errorf("since (%d) must be < till (%d)", req.Since, req.Till)
	}

	tagID, err := s.GetOrCreateTag(ctx, req.Tag, "")
	if err != nil {
		return nil, err
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	var payloadID int64
	err = tx.QueryRowContext(ctx,
		`INSERT INTO payloads (tag_id, data, checksum) VALUES ($1, $2, $3) RETURNING id`,
		tagID, []byte(req.Data), checksum(req.Data),
	).Scan(&payloadID)
	if err != nil {
		return nil, fmt.Errorf("insert payload: %w", err)
	}

	var iov IOV
	err = tx.QueryRowContext(ctx, `
		INSERT INTO iovs (tag_id, channel_id, payload_id, since, till, inserted_by, comment)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING id, tag_id, channel_id, payload_id, since, till, revision,
		          is_active, inserted_at, coalesce(inserted_by, ''), coalesce(comment, '')`,
		tagID, req.ChannelID, payloadID, req.Since, req.Till, req.InsertedBy, req.Comment,
	).Scan(&iov.ID, &iov.TagID, &iov.ChannelID, &iov.PayloadID, &iov.Since, &iov.Till,
		&iov.Revision, &iov.IsActive, &iov.InsertedAt, &iov.InsertedBy, &iov.Comment)
	if err != nil {
		if isExclusionViolation(err) {
			return nil, ErrOverlap
		}
		return nil, fmt.Errorf("insert iov: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit: %w", err)
	}
	iov.TagName = req.Tag
	return &iov, nil
}

// GetValidCalibration resolves the active IOV (and its payload) for a tag +
// channel whose validity range contains "at" (a run number or timestamp).
func (s *Store) GetValidCalibration(ctx context.Context, tag string, channelID, at int64) (*IOV, json.RawMessage, error) {
	var iov IOV
	var data json.RawMessage
	err := s.db.QueryRowContext(ctx, `
		SELECT i.id, i.tag_id, t.name, i.channel_id, i.payload_id, i.since, i.till,
		       i.revision, i.is_active, i.inserted_at,
		       coalesce(i.inserted_by, ''), coalesce(i.comment, ''), p.data
		FROM iovs i
		JOIN tags t ON t.id = i.tag_id
		JOIN payloads p ON p.id = i.payload_id
		WHERE t.name = $1 AND i.channel_id = $2 AND i.is_active
		  AND i.validity @> $3::bigint
		LIMIT 1`,
		tag, channelID, at,
	).Scan(&iov.ID, &iov.TagID, &iov.TagName, &iov.ChannelID, &iov.PayloadID, &iov.Since, &iov.Till,
		&iov.Revision, &iov.IsActive, &iov.InsertedAt, &iov.InsertedBy, &iov.Comment, &data)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil, ErrNotFound
	}
	if err != nil {
		return nil, nil, fmt.Errorf("query valid iov: %w", err)
	}
	return &iov, data, nil
}

// ListCalibrations returns all active IOVs for a tag, optionally filtered by channel.
func (s *Store) ListCalibrations(ctx context.Context, tag string, channelID *int64) ([]IOV, error) {
	query := `
		SELECT i.id, i.tag_id, t.name, i.channel_id, i.payload_id, i.since, i.till,
		       i.revision, i.is_active, i.inserted_at,
		       coalesce(i.inserted_by, ''), coalesce(i.comment, '')
		FROM iovs i
		JOIN tags t ON t.id = i.tag_id
		WHERE t.name = $1 AND i.is_active`
	args := []interface{}{tag}
	if channelID != nil {
		query += ` AND i.channel_id = $2`
		args = append(args, *channelID)
	}
	query += ` ORDER BY i.channel_id, i.since`

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list iovs: %w", err)
	}
	defer rows.Close()

	var out []IOV
	for rows.Next() {
		var iov IOV
		if err := rows.Scan(&iov.ID, &iov.TagID, &iov.TagName, &iov.ChannelID, &iov.PayloadID,
			&iov.Since, &iov.Till, &iov.Revision, &iov.IsActive, &iov.InsertedAt,
			&iov.InsertedBy, &iov.Comment); err != nil {
			return nil, fmt.Errorf("scan iov: %w", err)
		}
		out = append(out, iov)
	}
	return out, rows.Err()
}

// GetHistory returns every IOV revision (active and superseded) for a
// tag+channel, in validity order - useful for auditing "what did we think
// the constants were at insert time T", independent of later corrections.
func (s *Store) GetHistory(ctx context.Context, tag string, channelID int64) ([]IOV, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT i.id, i.tag_id, t.name, i.channel_id, i.payload_id, i.since, i.till,
		       i.revision, i.is_active, i.inserted_at,
		       coalesce(i.inserted_by, ''), coalesce(i.comment, '')
		FROM iovs i
		JOIN tags t ON t.id = i.tag_id
		WHERE t.name = $1 AND i.channel_id = $2
		ORDER BY i.since, i.revision`,
		tag, channelID,
	)
	if err != nil {
		return nil, fmt.Errorf("history query: %w", err)
	}
	defer rows.Close()

	var out []IOV
	for rows.Next() {
		var iov IOV
		if err := rows.Scan(&iov.ID, &iov.TagID, &iov.TagName, &iov.ChannelID, &iov.PayloadID,
			&iov.Since, &iov.Till, &iov.Revision, &iov.IsActive, &iov.InsertedAt,
			&iov.InsertedBy, &iov.Comment); err != nil {
			return nil, fmt.Errorf("scan iov: %w", err)
		}
		out = append(out, iov)
	}
	return out, rows.Err()
}

// CorrectCalibration supersedes any active IOV(s) overlapping [since, till)
// for a tag+channel with a freshly inserted payload, bumping the revision
// number. Superseded rows are kept (is_active = false) rather than deleted,
// so past reads remain reproducible.
func (s *Store) CorrectCalibration(ctx context.Context, tag string, channelID int64, req CorrectCalibrationRequest) (*IOV, error) {
	if req.Since >= req.Till {
		return nil, fmt.Errorf("since (%d) must be < till (%d)", req.Since, req.Till)
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	var tagID int64
	if err := tx.QueryRowContext(ctx, `SELECT id FROM tags WHERE name = $1`, tag).Scan(&tagID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("lookup tag: %w", err)
	}

	// Deactivate any active IOVs whose validity overlaps the new interval,
	// tracking the highest revision seen so the new row continues the series.
	rows, err := tx.QueryContext(ctx, `
		UPDATE iovs SET is_active = false
		WHERE tag_id = $1 AND channel_id = $2 AND is_active
		  AND validity && int8range($3, $4, '[)')
		RETURNING revision`,
		tagID, channelID, req.Since, req.Till,
	)
	if err != nil {
		return nil, fmt.Errorf("deactivate overlapping iovs: %w", err)
	}
	maxRevision := 0
	for rows.Next() {
		var rev int
		if err := rows.Scan(&rev); err != nil {
			rows.Close()
			return nil, fmt.Errorf("scan revision: %w", err)
		}
		if rev > maxRevision {
			maxRevision = rev
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate revisions: %w", err)
	}

	var payloadID int64
	if err := tx.QueryRowContext(ctx,
		`INSERT INTO payloads (tag_id, data, checksum) VALUES ($1, $2, $3) RETURNING id`,
		tagID, []byte(req.Data), checksum(req.Data),
	).Scan(&payloadID); err != nil {
		return nil, fmt.Errorf("insert payload: %w", err)
	}

	var iov IOV
	err = tx.QueryRowContext(ctx, `
		INSERT INTO iovs (tag_id, channel_id, payload_id, since, till, revision, inserted_by, comment)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		RETURNING id, tag_id, channel_id, payload_id, since, till, revision,
		          is_active, inserted_at, coalesce(inserted_by, ''), coalesce(comment, '')`,
		tagID, channelID, payloadID, req.Since, req.Till, maxRevision+1, req.InsertedBy, req.Comment,
	).Scan(&iov.ID, &iov.TagID, &iov.ChannelID, &iov.PayloadID, &iov.Since, &iov.Till,
		&iov.Revision, &iov.IsActive, &iov.InsertedAt, &iov.InsertedBy, &iov.Comment)
	if err != nil {
		if isExclusionViolation(err) {
			return nil, ErrOverlap
		}
		return nil, fmt.Errorf("insert iov: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit: %w", err)
	}
	iov.TagName = tag
	return &iov, nil
}

// DeactivateIOV soft-deletes a single IOV row by id (e.g. to retract a bad entry).
func (s *Store) DeactivateIOV(ctx context.Context, id int64) error {
	res, err := s.db.ExecContext(ctx, `UPDATE iovs SET is_active = false WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("deactivate iov: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("rows affected: %w", err)
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

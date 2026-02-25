package store

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"

	"github.com/fanzy618/pop/internal/config"
)

type SQLite struct {
	db *sql.DB
}

func OpenSQLite(path string) (*SQLite, error) {
	if path == "" {
		return nil, errors.New("sqlite path is required")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("ensure sqlite dir: %w", err)
	}

	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	db.SetMaxOpenConns(1)

	s := &SQLite{db: db}
	if err := s.init(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

func (s *SQLite) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

func (s *SQLite) init() error {
	stmts := []string{
		`PRAGMA foreign_keys = ON;`,
		`CREATE TABLE IF NOT EXISTS upstreams (
      id TEXT PRIMARY KEY,
      url TEXT NOT NULL,
      enabled INTEGER NOT NULL,
      created_at INTEGER NOT NULL
    );`,
		`CREATE TABLE IF NOT EXISTS rules (
      id TEXT PRIMARY KEY,
      enabled INTEGER NOT NULL,
      pattern TEXT NOT NULL,
      action TEXT NOT NULL,
      upstream_id TEXT,
      block_status INTEGER,
      created_at INTEGER NOT NULL,
      FOREIGN KEY(upstream_id) REFERENCES upstreams(id) ON DELETE RESTRICT
    );`,
		`CREATE INDEX IF NOT EXISTS idx_rules_created_at_desc ON rules(created_at DESC);`,
		`CREATE INDEX IF NOT EXISTS idx_rules_upstream_id ON rules(upstream_id);`,
	}

	for _, stmt := range stmts {
		if _, err := s.db.Exec(stmt); err != nil {
			return fmt.Errorf("init sqlite schema: %w", err)
		}
	}
	return nil
}

func (s *SQLite) ListUpstreams() ([]config.UpstreamConfig, error) {
	rows, err := s.db.Query(`SELECT id, url, enabled FROM upstreams ORDER BY created_at DESC, id DESC`)
	if err != nil {
		return nil, fmt.Errorf("query upstreams: %w", err)
	}
	defer rows.Close()

	var items []config.UpstreamConfig
	for rows.Next() {
		var item config.UpstreamConfig
		var enabled int
		if err := rows.Scan(&item.ID, &item.URL, &enabled); err != nil {
			return nil, fmt.Errorf("scan upstream: %w", err)
		}
		item.Enabled = enabled != 0
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate upstreams: %w", err)
	}
	return items, nil
}

func (s *SQLite) CreateUpstream(item config.UpstreamConfig) error {
	if item.ID == "" {
		return errors.New("id is required")
	}
	if item.URL == "" {
		return errors.New("url is required")
	}
	_, err := s.db.Exec(
		`INSERT INTO upstreams(id, url, enabled, created_at) VALUES (?, ?, ?, ?)`,
		item.ID,
		item.URL,
		boolToInt(item.Enabled),
		time.Now().UnixMilli(),
	)
	if err != nil {
		return fmt.Errorf("insert upstream: %w", err)
	}
	return nil
}

func (s *SQLite) UpdateUpstream(id string, item config.UpstreamConfig) error {
	if id == "" {
		return errors.New("id is required")
	}
	res, err := s.db.Exec(`UPDATE upstreams SET url=?, enabled=? WHERE id=?`, item.URL, boolToInt(item.Enabled), id)
	if err != nil {
		return fmt.Errorf("update upstream: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (s *SQLite) DeleteUpstream(id string) error {
	if id == "" {
		return errors.New("id is required")
	}
	res, err := s.db.Exec(`DELETE FROM upstreams WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete upstream: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (s *SQLite) ListRules() ([]config.RuleConfig, error) {
	rows, err := s.db.Query(`SELECT id, enabled, pattern, action, upstream_id, block_status, created_at FROM rules ORDER BY created_at DESC, id DESC`)
	if err != nil {
		return nil, fmt.Errorf("query rules: %w", err)
	}
	defer rows.Close()

	var items []config.RuleConfig
	for rows.Next() {
		var item config.RuleConfig
		var enabled int
		var upstreamID sql.NullString
		var blockStatus sql.NullInt64
		if err := rows.Scan(&item.ID, &enabled, &item.Pattern, &item.Action, &upstreamID, &blockStatus, &item.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan rule: %w", err)
		}
		item.Enabled = enabled != 0
		if upstreamID.Valid {
			item.UpstreamID = upstreamID.String
		}
		if blockStatus.Valid {
			item.BlockStatus = int(blockStatus.Int64)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate rules: %w", err)
	}
	return items, nil
}

func (s *SQLite) CreateRule(item config.RuleConfig) error {
	if item.ID == "" {
		return errors.New("id is required")
	}
	if item.Pattern == "" {
		return errors.New("pattern is required")
	}
	_, err := s.db.Exec(
		`INSERT INTO rules(id, enabled, pattern, action, upstream_id, block_status, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		item.ID,
		boolToInt(item.Enabled),
		item.Pattern,
		item.Action,
		nullString(item.UpstreamID),
		nullInt(item.BlockStatus),
		time.Now().UnixMilli(),
	)
	if err != nil {
		return fmt.Errorf("insert rule: %w", err)
	}
	return nil
}

func (s *SQLite) UpdateRule(id string, item config.RuleConfig) error {
	if id == "" {
		return errors.New("id is required")
	}
	res, err := s.db.Exec(
		`UPDATE rules SET enabled=?, pattern=?, action=?, upstream_id=?, block_status=? WHERE id=?`,
		boolToInt(item.Enabled),
		item.Pattern,
		item.Action,
		nullString(item.UpstreamID),
		nullInt(item.BlockStatus),
		id,
	)
	if err != nil {
		return fmt.Errorf("update rule: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (s *SQLite) DeleteRule(id string) error {
	if id == "" {
		return errors.New("id is required")
	}
	res, err := s.db.Exec(`DELETE FROM rules WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete rule: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func boolToInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

func nullString(v string) sql.NullString {
	if v == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: v, Valid: true}
}

func nullInt(v int) sql.NullInt64 {
	if v == 0 {
		return sql.NullInt64{}
	}
	return sql.NullInt64{Int64: int64(v), Valid: true}
}

package store

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"

	"github.com/fanzy618/pop/internal/config"
	"github.com/fanzy618/pop/internal/rules"
)

const CurrentDataFormatVersion = "1"

type BackupPayload struct {
	DataFormatVersion string                  `json:"data_format_version"`
	CreatedAt         string                  `json:"created_at"`
	Upstreams         []config.UpstreamConfig `json:"upstreams"`
	Rules             []config.RuleConfig     `json:"rules"`
}

type RuleListOptions struct {
	Keyword string
	Limit   int
	Offset  int
}

type RuleListPage struct {
	Items []config.RuleConfig
	Total int
}

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
	if _, err := s.db.Exec(`PRAGMA foreign_keys = ON;`); err != nil {
		return fmt.Errorf("enable sqlite foreign keys: %w", err)
	}
	return s.ensureSchema()
}

func (s *SQLite) ensureSchema() error {
	hasUpstreams, err := s.tableExists("upstreams")
	if err != nil {
		return err
	}
	hasRules, err := s.tableExists("rules")
	if err != nil {
		return err
	}

	if !hasUpstreams && !hasRules {
		if err := s.createSchema(); err != nil {
			return err
		}
		return s.ensureMetaVersion()
	}

	legacy, err := s.isLegacySchema()
	if err != nil {
		return err
	}
	if legacy {
		if err := s.migrateLegacySchema(); err != nil {
			return err
		}
	}
	if err := s.ensureIndexes(); err != nil {
		return err
	}
	return s.ensureMetaVersion()
}

func (s *SQLite) createSchema() error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS system_meta (
      key TEXT PRIMARY KEY,
      value TEXT NOT NULL
    );`,
		`CREATE TABLE IF NOT EXISTS upstreams (
      id INTEGER PRIMARY KEY AUTOINCREMENT,
      name TEXT,
      url TEXT NOT NULL,
      enabled INTEGER NOT NULL,
      created_at INTEGER NOT NULL
    );`,
		`CREATE TABLE IF NOT EXISTS rules (
      id INTEGER PRIMARY KEY AUTOINCREMENT,
      enabled INTEGER NOT NULL,
      pattern TEXT NOT NULL,
      action TEXT NOT NULL,
      upstream_ref INTEGER,
      block_status INTEGER,
      created_at INTEGER NOT NULL,
      FOREIGN KEY(upstream_ref) REFERENCES upstreams(id) ON DELETE RESTRICT
    );`,
	}
	for _, stmt := range stmts {
		if _, err := s.db.Exec(stmt); err != nil {
			return fmt.Errorf("create sqlite schema: %w", err)
		}
	}
	return s.ensureIndexes()
}

func (s *SQLite) ensureIndexes() error {
	stmts := []string{
		`CREATE INDEX IF NOT EXISTS idx_rules_created_at_desc ON rules(created_at DESC);`,
		`CREATE INDEX IF NOT EXISTS idx_rules_upstream_ref ON rules(upstream_ref);`,
	}
	for _, stmt := range stmts {
		if _, err := s.db.Exec(stmt); err != nil {
			return fmt.Errorf("ensure sqlite index: %w", err)
		}
	}
	return nil
}

func (s *SQLite) ensureMetaVersion() error {
	if _, err := s.db.Exec(`CREATE TABLE IF NOT EXISTS system_meta (key TEXT PRIMARY KEY, value TEXT NOT NULL);`); err != nil {
		return fmt.Errorf("ensure system_meta table: %w", err)
	}
	if _, err := s.db.Exec(`INSERT INTO system_meta(key, value) VALUES ('data_format_version', ?) ON CONFLICT(key) DO NOTHING`, CurrentDataFormatVersion); err != nil {
		return fmt.Errorf("ensure data_format_version: %w", err)
	}
	return nil
}

func (s *SQLite) tableExists(name string) (bool, error) {
	var c int
	err := s.db.QueryRow(`SELECT COUNT(1) FROM sqlite_master WHERE type='table' AND name=?`, name).Scan(&c)
	if err != nil {
		return false, fmt.Errorf("check table %s exists: %w", name, err)
	}
	return c > 0, nil
}

func (s *SQLite) isLegacySchema() (bool, error) {
	upCols, err := s.tableColumns("upstreams")
	if err != nil {
		return false, err
	}
	ruleCols, err := s.tableColumns("rules")
	if err != nil {
		return false, err
	}

	upIDType := strings.ToUpper(upCols["id"])
	_, hasName := upCols["name"]
	_, hasUpstreamRef := ruleCols["upstream_ref"]

	if strings.Contains(upIDType, "TEXT") || !hasName || !hasUpstreamRef {
		return true, nil
	}
	return false, nil
}

func (s *SQLite) tableColumns(table string) (map[string]string, error) {
	rows, err := s.db.Query(`PRAGMA table_info(` + table + `);`)
	if err != nil {
		return nil, fmt.Errorf("load table info for %s: %w", table, err)
	}
	defer rows.Close()

	cols := make(map[string]string)
	for rows.Next() {
		var cid int
		var name, typ string
		var notNull int
		var dflt sql.NullString
		var pk int
		if err := rows.Scan(&cid, &name, &typ, &notNull, &dflt, &pk); err != nil {
			return nil, fmt.Errorf("scan table info for %s: %w", table, err)
		}
		cols[name] = typ
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate table info for %s: %w", table, err)
	}
	return cols, nil
}

func (s *SQLite) migrateLegacySchema() error {
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin schema migration: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	stmts := []string{
		`CREATE TABLE IF NOT EXISTS system_meta (
      key TEXT PRIMARY KEY,
      value TEXT NOT NULL
    );`,
		`ALTER TABLE rules RENAME TO rules_old;`,
		`ALTER TABLE upstreams RENAME TO upstreams_old;`,
		`CREATE TABLE upstreams (
      id INTEGER PRIMARY KEY AUTOINCREMENT,
      name TEXT,
      url TEXT NOT NULL,
      enabled INTEGER NOT NULL,
      created_at INTEGER NOT NULL
    );`,
		`CREATE TABLE rules (
      id INTEGER PRIMARY KEY AUTOINCREMENT,
      enabled INTEGER NOT NULL,
      pattern TEXT NOT NULL,
      action TEXT NOT NULL,
      upstream_ref INTEGER,
      block_status INTEGER,
      created_at INTEGER NOT NULL,
      FOREIGN KEY(upstream_ref) REFERENCES upstreams(id) ON DELETE RESTRICT
    );`,
		`INSERT INTO upstreams(name, url, enabled, created_at)
     SELECT id, url, enabled, created_at FROM upstreams_old;`,
		`INSERT INTO rules(enabled, pattern, action, upstream_ref, block_status, created_at)
     SELECT ro.enabled,
            ro.pattern,
            ro.action,
            CASE
              WHEN ro.upstream_id IS NULL OR ro.upstream_id = '' THEN NULL
              ELSE u.id
            END,
            CASE
              WHEN ro.action = 'BLOCK' THEN COALESCE(NULLIF(ro.block_status, 0), 404)
              ELSE ro.block_status
            END,
            ro.created_at
     FROM rules_old ro
     LEFT JOIN upstreams u ON u.name = ro.upstream_id;`,
		`DROP TABLE rules_old;`,
		`DROP TABLE upstreams_old;`,
		`CREATE INDEX IF NOT EXISTS idx_rules_created_at_desc ON rules(created_at DESC);`,
		`CREATE INDEX IF NOT EXISTS idx_rules_upstream_ref ON rules(upstream_ref);`,
		`INSERT INTO system_meta(key, value) VALUES ('data_format_version', '` + CurrentDataFormatVersion + `') ON CONFLICT(key) DO UPDATE SET value=excluded.value;`,
	}

	for _, stmt := range stmts {
		if _, err = tx.Exec(stmt); err != nil {
			return fmt.Errorf("run schema migration statement: %w", err)
		}
	}

	if err = tx.Commit(); err != nil {
		return fmt.Errorf("commit schema migration: %w", err)
	}
	return nil
}

func (s *SQLite) ListUpstreams() ([]config.UpstreamConfig, error) {
	rows, err := s.db.Query(`SELECT id, name, url, enabled FROM upstreams ORDER BY created_at DESC, id DESC`)
	if err != nil {
		return nil, fmt.Errorf("query upstreams: %w", err)
	}
	defer rows.Close()

	items := make([]config.UpstreamConfig, 0)
	for rows.Next() {
		var item config.UpstreamConfig
		var name sql.NullString
		var enabled int
		if err := rows.Scan(&item.ID, &name, &item.URL, &enabled); err != nil {
			return nil, fmt.Errorf("scan upstream: %w", err)
		}
		if name.Valid {
			item.Name = name.String
		}
		item.Enabled = enabled != 0
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate upstreams: %w", err)
	}
	return items, nil
}

func (s *SQLite) CreateUpstream(item *config.UpstreamConfig) error {
	if item == nil {
		return errors.New("upstream is required")
	}
	if item.URL == "" {
		return errors.New("url is required")
	}
	res, err := s.db.Exec(
		`INSERT INTO upstreams(name, url, enabled, created_at) VALUES (?, ?, ?, ?)`,
		nullString(item.Name),
		item.URL,
		boolToInt(item.Enabled),
		time.Now().UnixMilli(),
	)
	if err != nil {
		return fmt.Errorf("insert upstream: %w", err)
	}
	insertID, err := res.LastInsertId()
	if err != nil {
		return fmt.Errorf("read inserted upstream id: %w", err)
	}
	item.ID = insertID
	return nil
}

func (s *SQLite) UpdateUpstream(id int64, item config.UpstreamConfig) error {
	if id <= 0 {
		return errors.New("id is required")
	}
	res, err := s.db.Exec(
		`UPDATE upstreams SET name=?, url=?, enabled=? WHERE id=?`,
		nullString(item.Name),
		item.URL,
		boolToInt(item.Enabled),
		id,
	)
	if err != nil {
		return fmt.Errorf("update upstream: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (s *SQLite) DeleteUpstream(id int64) error {
	if id <= 0 {
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
	page, err := s.ListRulesPage(RuleListOptions{})
	if err != nil {
		return nil, err
	}
	return page.Items, nil
}

func (s *SQLite) ListRulesPage(opts RuleListOptions) (RuleListPage, error) {
	keyword := strings.TrimSpace(strings.ToLower(opts.Keyword))
	where := ""
	args := make([]any, 0, 3)
	if keyword != "" {
		where = ` WHERE lower(pattern) LIKE ?`
		args = append(args, "%"+keyword+"%")
	}

	var total int
	countQuery := `SELECT COUNT(*) FROM rules` + where
	if err := s.db.QueryRow(countQuery, args...).Scan(&total); err != nil {
		return RuleListPage{}, fmt.Errorf("count rules: %w", err)
	}

	queryArgs := append([]any{}, args...)
	query := `SELECT id, enabled, pattern, action, upstream_ref, block_status, created_at FROM rules` + where + ` ORDER BY created_at DESC, id DESC`
	if opts.Limit > 0 {
		query += ` LIMIT ? OFFSET ?`
		queryArgs = append(queryArgs, opts.Limit, max(opts.Offset, 0))
	}

	rows, err := s.db.Query(query, queryArgs...)
	if err != nil {
		return RuleListPage{}, fmt.Errorf("query rules: %w", err)
	}
	defer rows.Close()

	items := make([]config.RuleConfig, 0)
	for rows.Next() {
		var item config.RuleConfig
		var enabled int
		var upstreamID sql.NullInt64
		var blockStatus sql.NullInt64
		if err := rows.Scan(&item.ID, &enabled, &item.Pattern, &item.Action, &upstreamID, &blockStatus, &item.CreatedAt); err != nil {
			return RuleListPage{}, fmt.Errorf("scan rule: %w", err)
		}
		item.Enabled = enabled != 0
		if upstreamID.Valid {
			item.UpstreamID = upstreamID.Int64
		}
		if blockStatus.Valid {
			item.BlockStatus = int(blockStatus.Int64)
		}
		if item.Action == "BLOCK" && item.BlockStatus == 0 {
			item.BlockStatus = 404
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return RuleListPage{}, fmt.Errorf("iterate rules: %w", err)
	}
	return RuleListPage{Items: items, Total: total}, nil
}

func (s *SQLite) CreateRule(item *config.RuleConfig) error {
	if item == nil {
		return errors.New("rule is required")
	}
	item.Pattern = normalizeRulePattern(item.Pattern)
	if item.Pattern == "" {
		return errors.New("pattern is required")
	}
	if item.Action == "BLOCK" {
		item.BlockStatus = 404
	}

	now := time.Now().UnixMilli()
	rows, err := s.db.Query(`SELECT id FROM rules WHERE lower(trim(pattern, '.')) = ? ORDER BY created_at DESC, id DESC`, item.Pattern)
	if err != nil {
		return fmt.Errorf("query existing rule by pattern: %w", err)
	}
	defer rows.Close()

	matchedIDs := make([]int64, 0, 1)
	for rows.Next() {
		var id int64
		if scanErr := rows.Scan(&id); scanErr != nil {
			return fmt.Errorf("scan existing rule id: %w", scanErr)
		}
		matchedIDs = append(matchedIDs, id)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate existing rule id: %w", err)
	}

	if len(matchedIDs) == 0 {
		res, execErr := s.db.Exec(
			`INSERT INTO rules(enabled, pattern, action, upstream_ref, block_status, created_at) VALUES (?, ?, ?, ?, ?, ?)`,
			boolToInt(item.Enabled),
			item.Pattern,
			item.Action,
			nullInt64(item.UpstreamID),
			nullInt(item.BlockStatus),
			now,
		)
		if execErr != nil {
			return fmt.Errorf("insert rule: %w", execErr)
		}
		insertID, idErr := res.LastInsertId()
		if idErr != nil {
			return fmt.Errorf("read inserted rule id: %w", idErr)
		}
		item.ID = insertID
		item.CreatedAt = now
		return nil
	}

	keepID := matchedIDs[0]
	if _, err := s.db.Exec(
		`UPDATE rules SET enabled=?, pattern=?, action=?, upstream_ref=?, block_status=?, created_at=? WHERE id=?`,
		boolToInt(item.Enabled),
		item.Pattern,
		item.Action,
		nullInt64(item.UpstreamID),
		nullInt(item.BlockStatus),
		now,
		keepID,
	); err != nil {
		return fmt.Errorf("update existing rule by pattern: %w", err)
	}

	if len(matchedIDs) > 1 {
		for _, duplicateID := range matchedIDs[1:] {
			if _, err := s.db.Exec(`DELETE FROM rules WHERE id=?`, duplicateID); err != nil {
				return fmt.Errorf("cleanup duplicate rule %d: %w", duplicateID, err)
			}
		}
	}

	item.ID = keepID
	item.CreatedAt = now
	return nil
}

func (s *SQLite) UpdateRule(id int64, item config.RuleConfig) error {
	if id <= 0 {
		return errors.New("id is required")
	}
	if item.Action == "BLOCK" {
		item.BlockStatus = 404
	}
	res, err := s.db.Exec(
		`UPDATE rules SET enabled=?, pattern=?, action=?, upstream_ref=?, block_status=? WHERE id=?`,
		boolToInt(item.Enabled),
		item.Pattern,
		item.Action,
		nullInt64(item.UpstreamID),
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

func (s *SQLite) DeleteRule(id int64) error {
	if id <= 0 {
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

func (s *SQLite) GetDataFormatVersion() (string, error) {
	if err := s.ensureMetaVersion(); err != nil {
		return "", err
	}
	var version string
	err := s.db.QueryRow(`SELECT value FROM system_meta WHERE key='data_format_version'`).Scan(&version)
	if err != nil {
		return "", fmt.Errorf("get data format version: %w", err)
	}
	return version, nil
}

func (s *SQLite) ExportBackup() (*BackupPayload, error) {
	version, err := s.GetDataFormatVersion()
	if err != nil {
		return nil, err
	}
	upstreams, err := s.ListUpstreams()
	if err != nil {
		return nil, err
	}
	rulesList, err := s.ListRules()
	if err != nil {
		return nil, err
	}
	return &BackupPayload{
		DataFormatVersion: version,
		CreatedAt:         time.Now().UTC().Format(time.RFC3339),
		Upstreams:         upstreams,
		Rules:             rulesList,
	}, nil
}

func (s *SQLite) RestoreBackup(payload *BackupPayload) error {
	if payload == nil {
		return errors.New("backup payload is required")
	}
	if strings.TrimSpace(payload.DataFormatVersion) == "" {
		return errors.New("data_format_version is required")
	}
	if payload.DataFormatVersion != CurrentDataFormatVersion {
		return fmt.Errorf("unsupported data_format_version: %s", payload.DataFormatVersion)
	}
	if err := config.ValidateRuntime(payload.Upstreams, payload.Rules); err != nil {
		return fmt.Errorf("backup payload validation failed: %w", err)
	}

	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin restore transaction: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	if _, err = tx.Exec(`DELETE FROM rules`); err != nil {
		return fmt.Errorf("clear rules: %w", err)
	}
	if _, err = tx.Exec(`DELETE FROM upstreams`); err != nil {
		return fmt.Errorf("clear upstreams: %w", err)
	}

	for _, up := range payload.Upstreams {
		if _, err = tx.Exec(
			`INSERT INTO upstreams(id, name, url, enabled, created_at) VALUES (?, ?, ?, ?, ?)`,
			up.ID,
			nullString(up.Name),
			up.URL,
			boolToInt(up.Enabled),
			time.Now().UnixMilli(),
		); err != nil {
			return fmt.Errorf("restore upstream %d: %w", up.ID, err)
		}
	}

	for _, r := range payload.Rules {
		if r.Action == rules.ActionBlock {
			r.BlockStatus = 404
		}
		createdAt := r.CreatedAt
		if createdAt <= 0 {
			createdAt = time.Now().UnixMilli()
		}
		if _, err = tx.Exec(
			`INSERT INTO rules(id, enabled, pattern, action, upstream_ref, block_status, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
			r.ID,
			boolToInt(r.Enabled),
			r.Pattern,
			r.Action,
			nullInt64(r.UpstreamID),
			nullInt(r.BlockStatus),
			createdAt,
		); err != nil {
			return fmt.Errorf("restore rule %d: %w", r.ID, err)
		}
	}

	if _, err = tx.Exec(`INSERT INTO system_meta(key, value) VALUES ('data_format_version', ?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, CurrentDataFormatVersion); err != nil {
		return fmt.Errorf("set data_format_version: %w", err)
	}

	if err = tx.Commit(); err != nil {
		return fmt.Errorf("commit restore transaction: %w", err)
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
	v = strings.TrimSpace(v)
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

func nullInt64(v int64) sql.NullInt64 {
	if v == 0 {
		return sql.NullInt64{}
	}
	return sql.NullInt64{Int64: v, Valid: true}
}

func normalizeRulePattern(v string) string {
	v = strings.TrimSpace(strings.ToLower(v))
	v = strings.TrimSuffix(v, ".")
	return v
}

package store

import (
	"context"
	"database/sql"
	"encoding/binary"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/flazouh/mneme/internal/embedder"
	"github.com/flazouh/mneme/internal/errcat"
	"github.com/flazouh/mneme/internal/memory"
	_ "modernc.org/sqlite"
)

const schema = `
PRAGMA journal_mode=WAL;
PRAGMA busy_timeout=5000;
PRAGMA foreign_keys=ON;

CREATE TABLE IF NOT EXISTS memories (
  id TEXT PRIMARY KEY,
  text TEXT NOT NULL,
  type TEXT NOT NULL,
  scope TEXT NOT NULL,
  project TEXT NOT NULL DEFAULT '',
  source TEXT NOT NULL,
  source_kind TEXT NOT NULL,
  lifecycle TEXT NOT NULL DEFAULT 'active',
  importance INTEGER NOT NULL,
  confidence REAL NOT NULL,
  agent TEXT NOT NULL DEFAULT '',
  user_id TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  valid_until TEXT,
  superseded_by TEXT NOT NULL DEFAULT '',
  usefulness INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS embeddings (
  memory_id TEXT PRIMARY KEY REFERENCES memories(id) ON DELETE CASCADE,
  model TEXT NOT NULL,
  dim INTEGER NOT NULL,
  vector BLOB NOT NULL
);

CREATE TABLE IF NOT EXISTS outbox (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  memory_id TEXT NOT NULL,
  op TEXT NOT NULL,
  payload TEXT NOT NULL,
  attempts INTEGER NOT NULL DEFAULT 0,
  next_attempt TEXT NOT NULL,
  last_error TEXT NOT NULL DEFAULT ''
);

CREATE INDEX IF NOT EXISTS idx_memories_scope_project ON memories(scope, project);
CREATE INDEX IF NOT EXISTS idx_memories_lifecycle ON memories(lifecycle);
CREATE INDEX IF NOT EXISTS idx_outbox_next ON outbox(next_attempt);
`

type Store struct {
	db *sql.DB
}

func Open(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(schema); err != nil {
		_ = db.Close()
		return nil, err
	}
	return &Store{db: db}, nil
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) Put(ctx context.Context, rec memory.Record, vector []float32, model string) error {
	if rec.Lifecycle == "" {
		rec.Lifecycle = memory.LifecycleActive
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	_, err = tx.ExecContext(ctx, `
INSERT INTO memories (
  id, text, type, scope, project, source, source_kind, lifecycle,
  importance, confidence, agent, user_id, created_at, updated_at,
  valid_until, superseded_by, usefulness
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET
  text=excluded.text,
  type=excluded.type,
  scope=excluded.scope,
  project=excluded.project,
  source=excluded.source,
  source_kind=excluded.source_kind,
  lifecycle=excluded.lifecycle,
  importance=excluded.importance,
  confidence=excluded.confidence,
  agent=excluded.agent,
  user_id=excluded.user_id,
  updated_at=excluded.updated_at,
  valid_until=excluded.valid_until,
  superseded_by=excluded.superseded_by,
  usefulness=excluded.usefulness
`, rec.ID, rec.Text, string(rec.Type), string(rec.Scope), rec.Project, rec.Source,
		string(rec.SourceKind), string(rec.Lifecycle), rec.Importance, rec.Confidence,
		rec.Agent, rec.UserID, rec.CreatedAt.UTC().Format(time.RFC3339Nano),
		rec.UpdatedAt.UTC().Format(time.RFC3339Nano), validUntil(rec.ValidUntil),
		rec.SupersededBy, rec.Usefulness)
	if err != nil {
		return err
	}
	if len(vector) > 0 {
		blob := encodeVector(vector)
		_, err = tx.ExecContext(ctx, `
INSERT INTO embeddings (memory_id, model, dim, vector) VALUES (?, ?, ?, ?)
ON CONFLICT(memory_id) DO UPDATE SET model=excluded.model, dim=excluded.dim, vector=excluded.vector
`, rec.ID, model, len(vector), blob)
		if err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	return nil
}

func (s *Store) Get(ctx context.Context, id string) (memory.Record, error) {
	row := s.db.QueryRowContext(ctx, `
SELECT id, text, type, scope, project, source, source_kind, lifecycle,
       importance, confidence, agent, user_id, created_at, updated_at,
       valid_until, superseded_by, usefulness
FROM memories WHERE id = ?`, id)
	rec, err := scanRecord(row)
	if err == sql.ErrNoRows {
		return memory.Record{}, errcat.New(errcat.NotFound, "memory "+id+" not found")
	}
	return rec, err
}

func (s *Store) SetLifecycle(ctx context.Context, id string, life memory.Lifecycle, now time.Time) error {
	if !life.Valid() {
		return errcat.New(errcat.InvalidInput, "unknown lifecycle")
	}
	res, err := s.db.ExecContext(ctx, `UPDATE memories SET lifecycle = ?, updated_at = ? WHERE id = ?`,
		string(life), now.UTC().Format(time.RFC3339Nano), id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return errcat.New(errcat.NotFound, "memory "+id+" not found")
	}
	return nil
}

func (s *Store) AddUsefulness(ctx context.Context, id string, delta int, now time.Time) error {
	res, err := s.db.ExecContext(ctx, `UPDATE memories SET usefulness = usefulness + ?, updated_at = ? WHERE id = ?`,
		delta, now.UTC().Format(time.RFC3339Nano), id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return errcat.New(errcat.NotFound, "memory "+id+" not found")
	}
	return nil
}

type Hit struct {
	Record memory.Record
	Score  float64
}

func (s *Store) Search(ctx context.Context, query []float32, scope memory.Scope, project string, now time.Time, topK int, threshold float64) ([]Hit, error) {
	if topK <= 0 {
		topK = 5
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT m.id, m.text, m.type, m.scope, m.project, m.source, m.source_kind, m.lifecycle,
       m.importance, m.confidence, m.agent, m.user_id, m.created_at, m.updated_at,
       m.valid_until, m.superseded_by, m.usefulness, e.vector
FROM memories m
JOIN embeddings e ON e.memory_id = m.id
WHERE m.scope = ?
  AND (? = '' OR m.scope != 'project' OR m.project = ?)
`, string(scope), project, project)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var hits []Hit
	for rows.Next() {
		var rec memory.Record
		var created, updated, valid sql.NullString
		var blob []byte
		if err := rows.Scan(
			&rec.ID, &rec.Text, (*string)(&rec.Type), (*string)(&rec.Scope), &rec.Project,
			&rec.Source, (*string)(&rec.SourceKind), (*string)(&rec.Lifecycle),
			&rec.Importance, &rec.Confidence, &rec.Agent, &rec.UserID,
			&created, &updated, &valid, &rec.SupersededBy, &rec.Usefulness, &blob,
		); err != nil {
			return nil, err
		}
		rec.CreatedAt = parseTime(created.String)
		rec.UpdatedAt = parseTime(updated.String)
		rec.ValidUntil = parseTimePtr(valid)
		if !rec.Recallable(now) {
			continue
		}
		vec := decodeVector(blob)
		score := embedder.Cosine(query, vec)
		if threshold > 0 && score < threshold {
			continue
		}
		hits = append(hits, Hit{Record: rec, Score: score})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	sortHits(hits)
	if len(hits) > topK {
		hits = hits[:topK]
	}
	return hits, nil
}

func (s *Store) EnqueueOutbox(ctx context.Context, memoryID, op, payload string, when time.Time) error {
	_, err := s.db.ExecContext(ctx, `
INSERT INTO outbox (memory_id, op, payload, attempts, next_attempt) VALUES (?, ?, ?, 0, ?)`,
		memoryID, op, payload, when.UTC().Format(time.RFC3339Nano))
	return err
}

type OutboxJob struct {
	ID       int64
	MemoryID string
	Op       string
	Payload  string
	Attempts int
}

func (s *Store) DueOutbox(ctx context.Context, now time.Time, limit int) ([]OutboxJob, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT id, memory_id, op, payload, attempts FROM outbox
WHERE next_attempt <= ? ORDER BY id ASC LIMIT ?`, now.UTC().Format(time.RFC3339Nano), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var jobs []OutboxJob
	for rows.Next() {
		var j OutboxJob
		if err := rows.Scan(&j.ID, &j.MemoryID, &j.Op, &j.Payload, &j.Attempts); err != nil {
			return nil, err
		}
		jobs = append(jobs, j)
	}
	return jobs, rows.Err()
}

func (s *Store) CompleteOutbox(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM outbox WHERE id = ?`, id)
	return err
}

func (s *Store) FailOutbox(ctx context.Context, id int64, attempts int, next time.Time, lastErr string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE outbox SET attempts = ?, next_attempt = ?, last_error = ? WHERE id = ?`,
		attempts, next.UTC().Format(time.RFC3339Nano), truncate(lastErr, 500), id)
	return err
}

func (s *Store) OutboxDepth(ctx context.Context) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM outbox`).Scan(&n)
	return n, err
}

func (s *Store) Count(ctx context.Context) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM memories`).Scan(&n)
	return n, err
}

func (s *Store) Ping(ctx context.Context) error {
	return s.db.PingContext(ctx)
}

type scanner interface {
	Scan(dest ...any) error
}

func scanRecord(row scanner) (memory.Record, error) {
	var rec memory.Record
	var created, updated, valid sql.NullString
	err := row.Scan(
		&rec.ID, &rec.Text, (*string)(&rec.Type), (*string)(&rec.Scope), &rec.Project,
		&rec.Source, (*string)(&rec.SourceKind), (*string)(&rec.Lifecycle),
		&rec.Importance, &rec.Confidence, &rec.Agent, &rec.UserID,
		&created, &updated, &valid, &rec.SupersededBy, &rec.Usefulness,
	)
	if err != nil {
		return memory.Record{}, err
	}
	rec.CreatedAt = parseTime(created.String)
	rec.UpdatedAt = parseTime(updated.String)
	rec.ValidUntil = parseTimePtr(valid)
	return rec, nil
}

func validUntil(t *time.Time) any {
	if t == nil {
		return nil
	}
	return t.UTC().Format(time.RFC3339Nano)
}

func parseTime(s string) time.Time {
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		t, _ = time.Parse(time.RFC3339, s)
	}
	return t
}

func parseTimePtr(s sql.NullString) *time.Time {
	if !s.Valid || strings.TrimSpace(s.String) == "" {
		return nil
	}
	t := parseTime(s.String)
	if t.IsZero() {
		return nil
	}
	return &t
}

func encodeVector(v []float32) []byte {
	b := make([]byte, 4*len(v))
	for i, x := range v {
		binary.LittleEndian.PutUint32(b[i*4:], math.Float32bits(x))
	}
	return b
}

func decodeVector(b []byte) []float32 {
	n := len(b) / 4
	v := make([]float32, n)
	for i := 0; i < n; i++ {
		v[i] = math.Float32frombits(binary.LittleEndian.Uint32(b[i*4:]))
	}
	return v
}

func sortHits(hits []Hit) {
	sort.Slice(hits, func(i, j int) bool { return hits[i].Score > hits[j].Score })
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

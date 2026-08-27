package archive

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/flazouh/mneme/internal/memory"
	"github.com/flazouh/mneme/internal/store"
)

type Worker struct {
	Store  *store.Store
	Repo   string
	Push   bool
	Now    func() time.Time
	Runner func(ctx context.Context, dir string, args ...string) error
}

func (w *Worker) Enqueue(ctx context.Context, rec memory.Record) error {
	payload, err := json.Marshal(rec)
	if err != nil {
		return err
	}
	now := w.now()
	return w.Store.EnqueueOutbox(ctx, rec.ID, "upsert", string(payload), now)
}

func (w *Worker) Tick(ctx context.Context) error {
	if w.Repo == "" {
		return nil
	}
	jobs, err := w.Store.DueOutbox(ctx, w.now(), 16)
	if err != nil {
		return err
	}
	for _, job := range jobs {
		if err := w.apply(ctx, job); err != nil {
			next := w.now().Add(backoff(job.Attempts + 1))
			_ = w.Store.FailOutbox(ctx, job.ID, job.Attempts+1, next, err.Error())
			continue
		}
		if err := w.Store.CompleteOutbox(ctx, job.ID); err != nil {
			return err
		}
	}
	return nil
}

func (w *Worker) apply(ctx context.Context, job store.OutboxJob) error {
	var rec memory.Record
	if err := json.Unmarshal([]byte(job.Payload), &rec); err != nil {
		return err
	}
	rel := filepath.Join("memories", rec.CreatedAt.UTC().Format("2006/01/02"), rec.ID+".json")
	full := filepath.Join(w.Repo, rel)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		return err
	}
	doc := archiveDoc(rec)
	raw, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	if err := os.WriteFile(full, raw, 0o644); err != nil {
		return err
	}
	run := w.Runner
	if run == nil {
		run = gitRun
	}
	if err := run(ctx, w.Repo, "add", rel); err != nil {
		return err
	}
	msg := fmt.Sprintf("memory: archive %s %s", job.Op, rec.ID)
	if err := run(ctx, w.Repo, "commit", "-m", msg); err != nil {
		// Nothing to commit is success (idempotent replay).
		if !isNothingToCommit(err) {
			return err
		}
	}
	if w.Push {
		if err := run(ctx, w.Repo, "push"); err != nil {
			return err
		}
	}
	return nil
}

func (w *Worker) now() time.Time {
	if w.Now != nil {
		return w.Now()
	}
	return time.Now()
}

func backoff(attempts int) time.Duration {
	d := time.Second * time.Duration(1<<min(attempts, 6))
	if d > 5*time.Minute {
		d = 5 * time.Minute
	}
	return d
}

func gitRun(ctx context.Context, dir string, args ...string) error {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git %v: %w: %s", args, err, truncate(string(out), 300))
	}
	return nil
}

func isNothingToCommit(err error) bool {
	if err == nil {
		return false
	}
	s := err.Error()
	return strings.Contains(s, "nothing to commit") || strings.Contains(s, "no changes added")
}

func archiveDoc(rec memory.Record) map[string]any {
	sum := sha256.Sum256([]byte(rec.Text))
	return map[string]any{
		"id":         rec.ID,
		"memory":     rec.Text,
		"created_at": rec.CreatedAt.UTC().Format(time.RFC3339Nano),
		"updated_at": rec.UpdatedAt.UTC().Format(time.RFC3339Nano),
		"hash":       hex.EncodeToString(sum[:16]),
		"user_id":    rec.UserID,
		"archive": map[string]any{
			"schema_version": memory.SchemaVersion,
			"source":         "mneme",
		},
		"metadata": map[string]any{
			"type":           rec.Type,
			"scope":          rec.Scope,
			"project":        rec.Project,
			"source":         rec.Source,
			"source_kind":    rec.SourceKind,
			"lifecycle":      rec.Lifecycle,
			"importance":     rec.Importance,
			"confidence":     rec.Confidence,
			"schema_version": memory.SchemaVersion,
		},
	}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

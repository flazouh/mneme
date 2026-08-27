package archive_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/flazouh/mneme/internal/archive"
	"github.com/flazouh/mneme/internal/memory"
	"github.com/flazouh/mneme/internal/store"
)

func TestOutboxRetriesThenLands(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	st, err := store.Open(filepath.Join(t.TempDir(), "mneme.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	repo := t.TempDir()
	now := time.Date(2026, 8, 27, 17, 0, 0, 0, time.UTC)
	attempts := 0
	w := &archive.Worker{
		Store: st,
		Repo:  repo,
		Now:   func() time.Time { return now },
		Runner: func(_ context.Context, _ string, args ...string) error {
			if args[0] == "commit" {
				attempts++
				if attempts == 1 {
					return os.ErrPermission
				}
			}
			return nil
		},
	}
	rec := memory.Record{
		ID:        "11111111-2222-3333-4444-555555555555",
		Text:      "Lesson: archive is an outbox, not a fire-once subprocess.",
		Type:      memory.TypeLesson,
		Scope:     memory.ScopeGlobal,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := w.Enqueue(ctx, rec); err != nil {
		t.Fatal(err)
	}
	if err := w.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	depth, _ := st.OutboxDepth(ctx)
	if depth != 1 {
		t.Fatalf("expected retry still queued, depth=%d", depth)
	}
	now = now.Add(10 * time.Second)
	w.Now = func() time.Time { return now }
	if err := w.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	depth, _ = st.OutboxDepth(ctx)
	if depth != 0 {
		t.Fatalf("expected complete, depth=%d", depth)
	}
	want := filepath.Join(repo, "memories", "2026/08/27", rec.ID+".json")
	raw, err := os.ReadFile(want)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), rec.Text) {
		t.Fatalf("archive file missing text: %s", raw)
	}
}

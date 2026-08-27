package store_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/flazouh/mneme/internal/embedder"
	"github.com/flazouh/mneme/internal/errcat"
	"github.com/flazouh/mneme/internal/memory"
	"github.com/flazouh/mneme/internal/store"
)

func TestPutGetAndGetByIDNeedsNoProject(t *testing.T) {
	t.Parallel()
	st := open(t)
	ctx := context.Background()
	now := time.Date(2026, 8, 27, 12, 0, 0, 0, time.UTC)
	rec := memory.Record{
		ID:         "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee",
		Text:       "Lesson: prove tracked files with git ls-tree, not ls.",
		Type:       memory.TypeLesson,
		Scope:      memory.ScopeGlobal,
		Source:     "test",
		SourceKind: memory.SourceAgentInference,
		Lifecycle:  memory.LifecycleActive,
		Importance: 4,
		Confidence: 0.9,
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	vec := embedder.HashEmbed(rec.Text, 8)
	if err := st.Put(ctx, rec, vec, "hash"); err != nil {
		t.Fatal(err)
	}
	got, err := st.Get(ctx, rec.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Text != rec.Text {
		t.Fatalf("got %q", got.Text)
	}
}

func TestSearchDropsInactiveAndRanks(t *testing.T) {
	t.Parallel()
	st := open(t)
	ctx := context.Background()
	now := time.Now().UTC()
	active := put(t, st, "active-id", "Lesson: keep request assembly append-only.", memory.LifecycleActive, now)
	put(t, st, "archived-id", "Lesson: keep request assembly append-only archived copy.", memory.LifecycleArchived, now)
	query := embedder.HashEmbed(active.Text, 8)
	hits, err := st.Search(ctx, query, memory.ScopeGlobal, "", now, 5, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 || hits[0].Record.ID != "active-id" {
		t.Fatalf("hits=%+v", hits)
	}
}

func TestMissingGet(t *testing.T) {
	t.Parallel()
	st := open(t)
	_, err := st.Get(context.Background(), "missing")
	if !errcat.Is(err, errcat.NotFound) {
		t.Fatalf("got %v", err)
	}
}

func open(t *testing.T) *store.Store {
	t.Helper()
	path := filepath.Join(t.TempDir(), "mneme.db")
	st, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st
}

func put(t *testing.T, st *store.Store, id, text string, life memory.Lifecycle, now time.Time) memory.Record {
	t.Helper()
	rec := memory.Record{
		ID:         id,
		Text:       text,
		Type:       memory.TypeLesson,
		Scope:      memory.ScopeGlobal,
		Source:     "test",
		SourceKind: memory.SourceAgentInference,
		Lifecycle:  life,
		Importance: 3,
		Confidence: 0.8,
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	if err := st.Put(context.Background(), rec, embedder.HashEmbed(text, 8), "hash"); err != nil {
		t.Fatal(err)
	}
	return rec
}

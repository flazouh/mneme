package engine_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/flazouh/mneme/internal/embedder"
	"github.com/flazouh/mneme/internal/engine"
	"github.com/flazouh/mneme/internal/errcat"
	"github.com/flazouh/mneme/internal/memory"
	"github.com/flazouh/mneme/internal/store"
)

func TestAddPackGetRoundTrip(t *testing.T) {
	t.Parallel()
	eng := newEngine(t)
	ctx := context.Background()
	rec, _, err := eng.Add(ctx, engine.AddRequest{
		Text:       "Lesson: prove tracked files with git ls-tree instead of directory listings.",
		Type:       memory.TypeLesson,
		Scope:      memory.ScopeGlobal,
		Source:     "test",
		SourceKind: memory.SourceAgentInference,
		Importance: 4,
		Confidence: 0.9,
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := eng.Get(ctx, rec.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != rec.ID {
		t.Fatalf("get mismatch")
	}
	pack, err := eng.Pack(ctx, engine.PackRequest{
		Query:     "how should I verify that a package exists in git",
		Scope:     memory.ScopeGlobal,
		TopK:      5,
		Threshold: 0,
		MaxChars:  1200,
	})
	if err != nil {
		t.Fatal(err)
	}
	if pack.Count != 1 {
		t.Fatalf("pack count=%d pack=%q", pack.Count, pack.ContextPack)
	}
}

func TestAddRejectsSecrets(t *testing.T) {
	t.Parallel()
	eng := newEngine(t)
	_, _, err := eng.Add(context.Background(), engine.AddRequest{
		Text:       "OPENAI_API_KEY=sk-abcdefghijklmnopqrstuvwxyz0123456789",
		Type:       memory.TypeLesson,
		Scope:      memory.ScopeGlobal,
		Source:     "test",
		SourceKind: memory.SourceAgentInference,
		Importance: 3,
		Confidence: 0.8,
	})
	if !errcat.Is(err, errcat.WriteRejected) {
		t.Fatalf("got %v", err)
	}
}

func TestLifecycleRemovesFromRecall(t *testing.T) {
	t.Parallel()
	eng := newEngine(t)
	ctx := context.Background()
	rec, _, err := eng.Add(ctx, engine.AddRequest{
		Text:       "Lesson: interrupt persistence must write the interrupted boundary first.",
		Type:       memory.TypeLesson,
		Scope:      memory.ScopeGlobal,
		Source:     "test",
		SourceKind: memory.SourceAgentInference,
		Importance: 3,
		Confidence: 0.8,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := eng.Lifecycle(ctx, rec.ID, memory.LifecycleArchived); err != nil {
		t.Fatal(err)
	}
	_, err = eng.Pack(ctx, engine.PackRequest{
		Query:     "interrupt persistence boundary",
		Scope:     memory.ScopeGlobal,
		Threshold: 0,
	})
	if !errcat.Is(err, errcat.EmptyRecall) {
		t.Fatalf("got %v", err)
	}
}

func newEngine(t *testing.T) *engine.Engine {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "mneme.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return &engine.Engine{
		Store:    st,
		Embedder: embedder.Hash{Dims: 32},
		Now:      func() time.Time { return time.Date(2026, 8, 27, 17, 0, 0, 0, time.UTC) },
	}
}

package daemon_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/flazouh/mneme/internal/daemon"
	"github.com/flazouh/mneme/internal/embedder"
	"github.com/flazouh/mneme/internal/engine"
	"github.com/flazouh/mneme/internal/memory"
	"github.com/flazouh/mneme/internal/store"
)

func TestDaemonRoundTripAndSingleLock(t *testing.T) {
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "mneme.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	eng := &engine.Engine{Store: st, Embedder: embedder.Hash{Dims: 32}}
	sock := filepath.Join(os.TempDir(), fmt.Sprintf("mneme-%d.sock", time.Now().UnixNano()))
	lock := filepath.Join(os.TempDir(), fmt.Sprintf("mneme-%d.lock", time.Now().UnixNano()))
	t.Cleanup(func() {
		_ = os.Remove(sock)
		_ = os.Remove(lock)
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	srv := &daemon.Server{Engine: eng, Socket: sock, Lock: lock}
	errCh := make(chan error, 1)
	go func() { errCh <- srv.Serve(ctx) }()

	client := daemon.Client{Socket: sock}
	deadline := time.Now().Add(2 * time.Second)
	var raw json.RawMessage
	for {
		raw, err = client.Call(context.Background(), "ping", map[string]any{})
		if err == nil {
			break
		}
		if time.Now().After(deadline) {
			select {
			case serveErr := <-errCh:
				t.Fatalf("daemon did not start: dial=%v serve=%v", err, serveErr)
			default:
				t.Fatalf("daemon did not start: %v", err)
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	if string(raw) == "" {
		t.Fatal("empty ping")
	}

	addRaw, err := client.Call(context.Background(), "add", engine.AddRequest{
		Text:       "Lesson: one unix socket daemon is the single writer for all agent hosts.",
		Type:       memory.TypeLesson,
		Scope:      memory.ScopeGlobal,
		Source:     "test",
		SourceKind: memory.SourceAgentInference,
		Importance: 3,
		Confidence: 0.9,
	})
	if err != nil {
		t.Fatal(err)
	}
	var added struct {
		Record memory.Record `json:"record"`
	}
	if err := json.Unmarshal(addRaw, &added); err != nil {
		t.Fatal(err)
	}
	got, err := client.Call(context.Background(), "get", map[string]string{"id": added.Record.ID})
	if err != nil {
		t.Fatal(err)
	}
	if !json.Valid(got) {
		t.Fatalf("invalid get json %s", got)
	}

	second := &daemon.Server{Engine: eng, Socket: filepath.Join(dir, "other.sock"), Lock: lock}
	if err := second.Serve(context.Background()); err == nil {
		t.Fatal("expected lock conflict")
	}
	cancel()
	select {
	case <-errCh:
	case <-time.After(2 * time.Second):
		t.Fatal("server did not exit")
	}
}

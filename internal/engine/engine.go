package engine

import (
	"context"
	"strings"
	"time"

	"github.com/flazouh/mneme/internal/archive"
	"github.com/flazouh/mneme/internal/embedder"
	"github.com/flazouh/mneme/internal/errcat"
	"github.com/flazouh/mneme/internal/gate"
	"github.com/flazouh/mneme/internal/memory"
	"github.com/flazouh/mneme/internal/project"
	"github.com/flazouh/mneme/internal/recall"
	"github.com/flazouh/mneme/internal/store"
	"github.com/google/uuid"
)

type Engine struct {
	Store    *store.Store
	Embedder embedder.Embedder
	Archive  *archive.Worker
	Now      func() time.Time
}

type AddRequest struct {
	Text       string
	Type       memory.Type
	Scope      memory.Scope
	Project    string
	Source     string
	SourceKind memory.SourceKind
	Importance int
	Confidence float64
	Agent      string
	UserID     string
	CWD        string
}

type PackRequest struct {
	Query     string
	TopK      int
	Threshold float64
	MaxChars  int
	Scope     memory.Scope
	Project   string
	CWD       string
}

type Health struct {
	OK          bool           `json:"ok"`
	Store       bool           `json:"store"`
	Memories    int            `json:"memories"`
	Outbox      int            `json:"outbox"`
	Embedder    string         `json:"embedder"`
	ArchiveRepo string         `json:"archive_repo,omitempty"`
	Checks      map[string]any `json:"checks"`
}

func (e *Engine) now() time.Time {
	if e.Now != nil {
		return e.Now()
	}
	return time.Now()
}

func (e *Engine) Add(ctx context.Context, req AddRequest) (memory.Record, gate.Result, error) {
	gated, err := gate.RejectBlocked(req.Text)
	if err != nil {
		return memory.Record{}, gated, err
	}
	if req.Type == "" {
		req.Type = memory.TypeLesson
	}
	if req.Scope == "" {
		req.Scope = memory.ScopeProject
	}
	if req.Source == "" {
		req.Source = "cli"
	}
	if req.SourceKind == "" {
		req.SourceKind = memory.SourceAgentInference
	}
	if req.Importance == 0 {
		req.Importance = 3
	}
	if req.Confidence == 0 {
		req.Confidence = 0.8
	}
	if req.Scope == memory.ScopeProject {
		key, err := project.Key(req.CWD, req.Project)
		if err != nil {
			return memory.Record{}, gated, err
		}
		req.Project = key
	}
	if err := memory.ValidateWrite(memory.WriteInput{
		Text:       req.Text,
		Type:       req.Type,
		Scope:      req.Scope,
		Project:    req.Project,
		Source:     req.Source,
		SourceKind: req.SourceKind,
		Importance: req.Importance,
		Confidence: req.Confidence,
		Agent:      req.Agent,
		UserID:     req.UserID,
	}); err != nil {
		return memory.Record{}, gated, err
	}
	now := e.now()
	rec := memory.Record{
		ID:         uuid.NewString(),
		Text:       strings.Join(strings.Fields(req.Text), " "),
		Type:       req.Type,
		Scope:      req.Scope,
		Project:    req.Project,
		Source:     req.Source,
		SourceKind: req.SourceKind,
		Lifecycle:  memory.LifecycleActive,
		Importance: req.Importance,
		Confidence: req.Confidence,
		Agent:      req.Agent,
		UserID:     req.UserID,
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	vec, err := e.Embedder.Embed(ctx, rec.Text)
	if err != nil {
		return memory.Record{}, gated, errcat.Wrap(errcat.ServiceUnavailable, "embed failed", err)
	}
	if err := e.Store.Put(ctx, rec, vec, e.Embedder.Model()); err != nil {
		return memory.Record{}, gated, err
	}
	if e.Archive != nil && e.Archive.Repo != "" {
		if err := e.Archive.Enqueue(ctx, rec); err != nil {
			return rec, gated, errcat.Wrap(errcat.ServiceUnavailable, "archive enqueue failed", err)
		}
	}
	return rec, gated, nil
}

func (e *Engine) Pack(ctx context.Context, req PackRequest) (recall.Pack, error) {
	if strings.TrimSpace(req.Query) == "" {
		return recall.Pack{}, errcat.New(errcat.InvalidInput, "query is empty")
	}
	if req.Scope == "" {
		req.Scope = memory.ScopeProject
	}
	if req.Scope == memory.ScopeProject {
		key, err := project.Key(req.CWD, req.Project)
		if err != nil {
			return recall.Pack{}, err
		}
		req.Project = key
	}
	vec, err := e.Embedder.Embed(ctx, req.Query)
	if err != nil {
		return recall.Pack{}, errcat.Wrap(errcat.ServiceUnavailable, "embed failed", err)
	}
	hits, err := e.Store.Search(ctx, vec, req.Scope, req.Project, e.now(), req.TopK, req.Threshold)
	if err != nil {
		return recall.Pack{}, err
	}
	pack := recall.Compact(hits, req.MaxChars, e.now())
	if pack.Count == 0 {
		return pack, errcat.New(errcat.EmptyRecall, "no active memories matched")
	}
	return pack, nil
}

func (e *Engine) Get(ctx context.Context, id string) (memory.Record, error) {
	if strings.TrimSpace(id) == "" {
		return memory.Record{}, errcat.New(errcat.InvalidInput, "id is empty")
	}
	return e.Store.Get(ctx, id)
}

func (e *Engine) Lifecycle(ctx context.Context, id string, life memory.Lifecycle) error {
	return e.Store.SetLifecycle(ctx, id, life, e.now())
}

func (e *Engine) Usefulness(ctx context.Context, id string, useful bool) error {
	delta := 1
	if !useful {
		delta = -1
	}
	return e.Store.AddUsefulness(ctx, id, delta, e.now())
}

func (e *Engine) Health(ctx context.Context) Health {
	h := Health{
		Checks:   map[string]any{},
		Embedder: e.Embedder.Model(),
	}
	if e.Archive != nil {
		h.ArchiveRepo = e.Archive.Repo
	}
	if err := e.Store.Ping(ctx); err != nil {
		h.Checks["store"] = err.Error()
		return h
	}
	h.Store = true
	n, _ := e.Store.Count(ctx)
	h.Memories = n
	out, _ := e.Store.OutboxDepth(ctx)
	h.Outbox = out
	h.Checks["store"] = "ok"
	h.Checks["outbox"] = out
	h.OK = true
	return h
}

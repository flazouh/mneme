package mcpserver

import (
	"context"
	"encoding/json"

	"github.com/flazouh/mneme/internal/daemon"
	"github.com/flazouh/mneme/internal/engine"
	"github.com/flazouh/mneme/internal/memory"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func New(socket string) *mcp.Server {
	client := daemon.Client{Socket: socket}
	server := mcp.NewServer(&mcp.Implementation{Name: "mneme", Version: version}, nil)

	type packArgs struct {
		Query     string  `json:"query" jsonschema:"semantic query"`
		TopK      int     `json:"top_k,omitempty" jsonschema:"max results, default 5"`
		Threshold float64 `json:"threshold,omitempty" jsonschema:"minimum cosine score, default 0.25"`
		MaxChars  int     `json:"max_chars,omitempty" jsonschema:"pack character budget, default 1200"`
		Scope     string  `json:"scope,omitempty" jsonschema:"global, project, domain, agent, or session"`
		Project   string  `json:"project,omitempty" jsonschema:"optional explicit project key"`
		CWD       string  `json:"cwd,omitempty" jsonschema:"working directory used to resolve git origin"`
	}
	mcp.AddTool(server, &mcp.Tool{
		Name:        "memory_pack",
		Description: "Return a compact recall pack of active memories for the current work.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, args packArgs) (*mcp.CallToolResult, any, error) {
		raw, err := client.Call(ctx, "pack", engine.PackRequest{
			Query:     args.Query,
			TopK:      args.TopK,
			Threshold: args.Threshold,
			MaxChars:  args.MaxChars,
			Scope:     memory.Scope(args.Scope),
			Project:   args.Project,
			CWD:       args.CWD,
		})
		return textResult(raw, err)
	})

	type addArgs struct {
		Text       string  `json:"text" jsonschema:"durable memory text"`
		Type       string  `json:"type,omitempty" jsonschema:"preference, decision, lesson, project_fact, identity, procedure, constraint, explicit_memory"`
		Scope      string  `json:"scope,omitempty"`
		Project    string  `json:"project,omitempty"`
		Source     string  `json:"source,omitempty"`
		SourceKind string  `json:"source_kind,omitempty" jsonschema:"explicit_user_instruction, verified_repository_fact, or agent_inference"`
		Importance int     `json:"importance,omitempty"`
		Confidence float64 `json:"confidence,omitempty"`
		Agent      string  `json:"agent,omitempty"`
		CWD        string  `json:"cwd,omitempty"`
	}
	mcp.AddTool(server, &mcp.Tool{
		Name:        "memory_add",
		Description: "Gate and store one durable memory. Rejects secrets, transcripts, and rediscoverable events.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, args addArgs) (*mcp.CallToolResult, any, error) {
		raw, err := client.Call(ctx, "add", engine.AddRequest{
			Text:       args.Text,
			Type:       memory.Type(args.Type),
			Scope:      memory.Scope(args.Scope),
			Project:    args.Project,
			Source:     first(args.Source, "mcp"),
			SourceKind: memory.SourceKind(args.SourceKind),
			Importance: args.Importance,
			Confidence: args.Confidence,
			Agent:      args.Agent,
			CWD:        args.CWD,
		})
		return textResult(raw, err)
	})

	type idArgs struct {
		ID string `json:"id" jsonschema:"memory id"`
	}
	mcp.AddTool(server, &mcp.Tool{
		Name:        "memory_get",
		Description: "Fetch one memory by id. Project scope is not required.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, args idArgs) (*mcp.CallToolResult, any, error) {
		raw, err := client.Call(ctx, "get", map[string]string{"id": args.ID})
		return textResult(raw, err)
	})

	type lifeArgs struct {
		ID        string `json:"id"`
		Lifecycle string `json:"lifecycle" jsonschema:"active, archived, quarantined, expired, superseded, deleted"`
	}
	mcp.AddTool(server, &mcp.Tool{
		Name:        "memory_lifecycle",
		Description: "Change a memory lifecycle without hard-deleting it.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, args lifeArgs) (*mcp.CallToolResult, any, error) {
		raw, err := client.Call(ctx, "lifecycle", map[string]string{"id": args.ID, "lifecycle": args.Lifecycle})
		return textResult(raw, err)
	})

	type useArgs struct {
		ID     string `json:"id"`
		Useful bool   `json:"useful"`
	}
	mcp.AddTool(server, &mcp.Tool{
		Name:        "memory_usefulness",
		Description: "Record whether a recalled memory helped.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, args useArgs) (*mcp.CallToolResult, any, error) {
		raw, err := client.Call(ctx, "usefulness", map[string]any{"id": args.ID, "useful": args.Useful})
		return textResult(raw, err)
	})

	type healthArgs struct{}
	mcp.AddTool(server, &mcp.Tool{
		Name:        "memory_health",
		Description: "Report daemon, store, and archive-outbox health.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, _ healthArgs) (*mcp.CallToolResult, any, error) {
		raw, err := client.Call(ctx, "health", map[string]any{})
		return textResult(raw, err)
	})

	return server
}

func textResult(raw json.RawMessage, err error) (*mcp.CallToolResult, any, error) {
	if err != nil {
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: err.Error()}},
			IsError: true,
		}, nil, nil
	}
	text := string(raw)
	if text == "" {
		text = "{}"
	}
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: text}},
	}, nil, nil
}

func first(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

const version = "0.1.0"

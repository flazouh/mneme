package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/flazouh/mneme/internal/archive"
	"github.com/flazouh/mneme/internal/config"
	"github.com/flazouh/mneme/internal/daemon"
	"github.com/flazouh/mneme/internal/embedder"
	"github.com/flazouh/mneme/internal/engine"
	"github.com/flazouh/mneme/internal/memory"
	"github.com/flazouh/mneme/internal/mcpserver"
	"github.com/flazouh/mneme/internal/store"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const version = "0.1.0"

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		return errors.New(usage)
	}
	switch args[0] {
	case "serve":
		return cmdServe()
	case "mcp":
		return cmdMCP()
	case "pack":
		return cmdPack(args[1:])
	case "add":
		return cmdAdd(args[1:])
	case "get":
		return cmdGet(args[1:])
	case "lifecycle":
		return cmdLifecycle(args[1:])
	case "health":
		return cmdHealth()
	case "install-launchd":
		return cmdInstallLaunchd()
	case "version", "-v", "--version":
		fmt.Println(version)
		return nil
	case "help", "-h", "--help":
		fmt.Println(usage)
		return nil
	default:
		return fmt.Errorf("unknown command %q\n%s", args[0], usage)
	}
}

const usage = `mneme is a single-writer local memory daemon for coding agents.

Usage:
  mneme serve              run the unix-socket daemon (single writer)
  mneme mcp                stdio MCP shim; talks to the daemon
  mneme pack <query>       compact recall pack
  mneme add [flags] <text> gated write
  mneme get <id>           fetch by id (no project required)
  mneme lifecycle <id> <state>
  mneme health
  mneme install-launchd    write a KeepAlive LaunchAgent

Environment:
  MNEME_DATA_DIR, MNEME_SOCKET, MNEME_DB, MNEME_ARCHIVE_REPO
  MNEME_HASH_EMBED=1 for offline embeddings
  OPENAI_API_KEY / OPENAI_BASE_URL for production embeddings`

func cmdServe() error {
	cfg := config.Load()
	eng, arch, st, err := openEngine(cfg)
	if err != nil {
		return err
	}
	defer st.Close()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	srv := &daemon.Server{
		Engine:  eng,
		Socket:  cfg.Socket,
		Lock:    cfg.Lock,
		Archive: arch,
		Logger:  slog.New(slog.NewTextHandler(os.Stderr, nil)),
	}
	return srv.Serve(ctx)
}

func cmdMCP() error {
	cfg := config.Load()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return mcpserver.New(cfg.Socket).Run(ctx, &mcp.StdioTransport{})
}

func cmdPack(args []string) error {
	cfg := config.Load()
	query, flags := parseFlags(args)
	if strings.TrimSpace(query) == "" {
		return errors.New("pack requires a query")
	}
	cwd, _ := os.Getwd()
	req := engine.PackRequest{
		Query:     query,
		TopK:      intFlag(flags, "top-k", 5),
		Threshold: floatFlag(flags, "threshold", 0.25),
		MaxChars:  intFlag(flags, "max-chars", 1200),
		Scope:     memory.Scope(strFlag(flags, "scope", "project")),
		Project:   strFlag(flags, "project", ""),
		CWD:       cwd,
	}
	raw, err := callOrLocal(cfg, "pack", req)
	if err != nil {
		return err
	}
	return printRaw(raw)
}

func cmdAdd(args []string) error {
	cfg := config.Load()
	text, flags := parseFlags(args)
	if strings.TrimSpace(text) == "" {
		return errors.New("add requires text")
	}
	cwd, _ := os.Getwd()
	req := engine.AddRequest{
		Text:       text,
		Type:       memory.Type(strFlag(flags, "type", "lesson")),
		Scope:      memory.Scope(strFlag(flags, "scope", "global")),
		Project:    strFlag(flags, "project", ""),
		Source:     strFlag(flags, "source", "cli"),
		SourceKind: memory.SourceKind(strFlag(flags, "source-kind", string(memory.SourceAgentInference))),
		Importance: intFlag(flags, "importance", 3),
		Confidence: floatFlag(flags, "confidence", 0.8),
		Agent:      strFlag(flags, "agent", ""),
		UserID:     cfg.UserID,
		CWD:        cwd,
	}
	raw, err := callOrLocal(cfg, "add", req)
	if err != nil {
		return err
	}
	return printRaw(raw)
}

func cmdGet(args []string) error {
	if len(args) < 1 {
		return errors.New("get requires an id")
	}
	cfg := config.Load()
	raw, err := callOrLocal(cfg, "get", map[string]string{"id": args[0]})
	if err != nil {
		return err
	}
	return printRaw(raw)
}

func cmdLifecycle(args []string) error {
	if len(args) < 2 {
		return errors.New("lifecycle requires <id> <state>")
	}
	cfg := config.Load()
	raw, err := callOrLocal(cfg, "lifecycle", map[string]string{"id": args[0], "lifecycle": args[1]})
	if err != nil {
		return err
	}
	return printRaw(raw)
}

func cmdHealth() error {
	cfg := config.Load()
	raw, err := callOrLocal(cfg, "health", map[string]any{})
	if err != nil {
		return err
	}
	return printRaw(raw)
}

func cmdInstallLaunchd() error {
	cfg := config.Load()
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	exe, err = filepath.EvalSymlinks(exe)
	if err != nil {
		exe, _ = os.Executable()
	}
	home, _ := os.UserHomeDir()
	dir := filepath.Join(home, "Library", "LaunchAgents")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	plist := filepath.Join(dir, "com.flazouh.mneme.plist")
	body := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key><string>com.flazouh.mneme</string>
  <key>ProgramArguments</key>
  <array>
    <string>%s</string>
    <string>serve</string>
  </array>
  <key>RunAtLoad</key><true/>
  <key>KeepAlive</key><true/>
  <key>StandardOutPath</key><string>%s</string>
  <key>StandardErrorPath</key><string>%s</string>
</dict>
</plist>
`, exe, filepath.Join(cfg.DataDir, "mneme.log"), filepath.Join(cfg.DataDir, "mneme.log"))
	if err := os.MkdirAll(cfg.DataDir, 0o700); err != nil {
		return err
	}
	if err := os.WriteFile(plist, []byte(body), 0o644); err != nil {
		return err
	}
	fmt.Println(plist)
	fmt.Println("load with: launchctl load " + plist)
	return nil
}

func callOrLocal(cfg config.Config, method string, params any) (json.RawMessage, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	client := daemon.Client{Socket: cfg.Socket}
	raw, err := client.Call(ctx, method, params)
	if err == nil {
		return raw, nil
	}
	// Offline path for tests and first-run CLI without a daemon.
	eng, _, st, openErr := openEngine(cfg)
	if openErr != nil {
		return nil, err
	}
	defer st.Close()
	return localCall(ctx, eng, method, params)
}

func localCall(ctx context.Context, eng *engine.Engine, method string, params any) (json.RawMessage, error) {
	raw, _ := json.Marshal(params)
	switch method {
	case "pack":
		var p engine.PackRequest
		if err := json.Unmarshal(raw, &p); err != nil {
			return nil, err
		}
		out, err := eng.Pack(ctx, p)
		if err != nil {
			return nil, err
		}
		return json.Marshal(out)
	case "add":
		var p engine.AddRequest
		if err := json.Unmarshal(raw, &p); err != nil {
			return nil, err
		}
		rec, gated, err := eng.Add(ctx, p)
		if err != nil {
			return nil, err
		}
		return json.Marshal(map[string]any{"record": rec, "gate": gated})
	case "get":
		var p struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(raw, &p); err != nil {
			return nil, err
		}
		out, err := eng.Get(ctx, p.ID)
		if err != nil {
			return nil, err
		}
		return json.Marshal(out)
	case "lifecycle":
		var p struct {
			ID        string           `json:"id"`
			Lifecycle memory.Lifecycle `json:"lifecycle"`
		}
		if err := json.Unmarshal(raw, &p); err != nil {
			return nil, err
		}
		if err := eng.Lifecycle(ctx, p.ID, p.Lifecycle); err != nil {
			return nil, err
		}
		return json.Marshal(p)
	case "health":
		return json.Marshal(eng.Health(ctx))
	default:
		return nil, fmt.Errorf("unknown method %s", method)
	}
}

func openEngine(cfg config.Config) (*engine.Engine, *archive.Worker, *store.Store, error) {
	st, err := store.Open(cfg.DBPath)
	if err != nil {
		return nil, nil, nil, err
	}
	var emb embedder.Embedder
	if cfg.HashEmbed || strings.TrimSpace(cfg.OpenAIKey) == "" {
		emb = embedder.Hash{Dims: 32}
	} else {
		emb = embedder.OpenAI{
			APIKey:  cfg.OpenAIKey,
			BaseURL: cfg.OpenAIBaseURL,
			ModelID: cfg.EmbeddingModel,
		}
	}
	arch := &archive.Worker{Store: st, Repo: cfg.ArchiveRepo, Push: cfg.ArchivePush}
	eng := &engine.Engine{Store: st, Embedder: emb, Archive: arch}
	return eng, arch, st, nil
}

func printRaw(raw json.RawMessage) error {
	var pretty any
	if err := json.Unmarshal(raw, &pretty); err != nil {
		_, err2 := io.WriteString(os.Stdout, string(raw)+"\n")
		return err2
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(pretty)
}

func parseFlags(args []string) (string, map[string]string) {
	flags := map[string]string{}
	var rest []string
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if !strings.HasPrefix(arg, "--") {
			rest = append(rest, arg)
			continue
		}
		key := strings.TrimPrefix(arg, "--")
		if strings.Contains(key, "=") {
			parts := strings.SplitN(key, "=", 2)
			flags[parts[0]] = parts[1]
			continue
		}
		if i+1 >= len(args) {
			flags[key] = "true"
			continue
		}
		flags[key] = args[i+1]
		i++
	}
	return strings.Join(rest, " "), flags
}

func strFlag(flags map[string]string, key, fallback string) string {
	if v, ok := flags[key]; ok {
		return v
	}
	return fallback
}

func intFlag(flags map[string]string, key string, fallback int) int {
	v, ok := flags[key]
	if !ok {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return fallback
	}
	return n
}

func floatFlag(flags map[string]string, key string, fallback float64) float64 {
	v, ok := flags[key]
	if !ok {
		return fallback
	}
	n, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return fallback
	}
	return n
}

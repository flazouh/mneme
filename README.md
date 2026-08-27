# mneme

Single-writer local memory for coding agents. One Go binary. No Python. No Node.

Agents talk to a unix-socket daemon. The MCP stdio process is a thin shim. SQLite is the source of truth. Git archive is an outbox with retries, not a fire-once subprocess.

## Why this shape

Cursor, Claude Code, and Codex each spawn their own MCP server. That multiplies environments, so a PATH bug in one host does not match another, and a code fix does not reach a process that is already running.

`mneme serve` holds an exclusive lock. Every host's `mneme mcp` dials the same socket. One writer. One environment.

## Install

```bash
go install github.com/flazouh/mneme/cmd/mneme@latest
mneme serve
```

In another terminal:

```bash
mneme add --scope global --type lesson \
  "Lesson: prove tracked files with git ls-tree, not ls."
mneme pack "how do I verify a package exists in git"
mneme health
```

macOS daemon:

```bash
mneme install-launchd
launchctl load ~/Library/LaunchAgents/com.flazouh.mneme.plist
```

## MCP

Point every agent at the same shim. The shim has no logic. The daemon does.

```json
{
  "mcpServers": {
    "mneme": {
      "command": "mneme",
      "args": ["mcp"]
    }
  }
}
```

Six tools: `memory_pack`, `memory_add`, `memory_get`, `memory_lifecycle`, `memory_usefulness`, `memory_health`.

`memory_get` is by id. It does not require a project.

## Write gate

Hard reject: secrets, prompt-injection text, raw transcripts, rediscoverable PR/CI/branch events.

High-impact `decision` and `constraint` (importance >= 4) need `source_kind=explicit_user_instruction` or `verified_repository_fact`.

Lifecycle states replace hard deletes: `active`, `archived`, `quarantined`, `expired`, `superseded`, `deleted`. Recall reads `active` only.

## Storage

Default data dir: `~/.local/share/mneme`.

| Path | Role |
| --- | --- |
| `mneme.db` | SQLite WAL, memories + embeddings + outbox |
| `mneme.sock` | unix socket, mode 0600 |
| `mneme.lock` | flock, released on process death |

Embeddings: OpenAI-compatible HTTP when `OPENAI_API_KEY` is set. Otherwise a local hash embedder so tests and first run work offline. Set `MNEME_HASH_EMBED=1` to force the local embedder.

Optional git archive: set `MNEME_ARCHIVE_REPO` to a git checkout. Writes enqueue. A worker in the daemon commits, retries, then optionally pushes (`MNEME_ARCHIVE_PUSH=true`).

## Config

`~/.config/mneme/config.env` plus environment.

```
MNEME_DATA_DIR
MNEME_SOCKET
MNEME_DB
MNEME_ARCHIVE_REPO
MNEME_ARCHIVE_PUSH
OPENAI_API_KEY
OPENAI_BASE_URL
MNEME_EMBEDDING_MODEL
MNEME_HASH_EMBED
```

## Development

```bash
make test
make vet
```

Go 1.23+. CGO is not required.

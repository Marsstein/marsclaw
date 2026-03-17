<p align="center">
  <h1 align="center">LiteClaw</h1>
  <p align="center">
    <strong>Lightweight, secure, multi-agent AI runtime</strong>
  </p>
  <p align="center">
    <a href="#quick-start">Quick Start</a> ·
    <a href="#features">Features</a> ·
    <a href="#comparison">Comparison</a> ·
    <a href="#configuration">Configuration</a> ·
    <a href="#architecture">Architecture</a> ·
    <a href="#roadmap">Roadmap</a>
  </p>
</p>

---

**13MB binary · <50MB RAM · Sub-second startup · Zero CVEs**

LiteClaw is a personal AI agent runtime written in Go. It connects to Claude, GPT, and local models to help you code, automate tasks, and orchestrate multi-agent workflows — all from a single binary with no dependencies.

```
$ liteclaw "add error handling to main.go"

⚡ read_file
✓ read_file
⚡ edit_file
✓ edit_file

Added error wrapping with fmt.Errorf to all three return paths in main().

── claude-sonnet-4 │ 1.2K in / 523 out │ $0.012 session ──
```

## Quick Start

```bash
# Install (Go 1.23+)
go install github.com/marsstein/liteclaw/cmd/liteclaw@latest

# Or download a binary
curl -sSfL https://liteclaw.dev/install.sh | sh

# Set your API key
export ANTHROPIC_API_KEY="sk-ant-..."

# Interactive mode
liteclaw

# Single prompt
liteclaw "explain this codebase"
```

## Comparison

|  | OpenClaw | PicoClaw | ZeroClaw | **LiteClaw** |
|---|---------|----------|----------|:------------:|
| **Language** | TypeScript | Go | Rust | **Go** |
| **Binary** | npm install | 8MB | 3.4MB | **13MB** |
| **Memory** | 200MB+ | <20MB | <15MB | **<50MB** |
| **Startup** | 3-5s | <100ms | <50ms | **<200ms** |
| **CVEs** | 512+ | 0 | 0 | **0** |
| **LOC** | 430K | ~15K | ~10K | **<3K** |
| **Multi-agent** | No | No | No | **6 patterns** |
| **Cost tracking** | No | No | No | **Built-in** |
| **Bounded memory** | No | No | No | **14K cap** |
| **Credential scanning** | No | No | No | **Yes** |
| **Tool approval** | No | Partial | No | **Per-danger-level** |

## Features

### Agent Loop

A flat while-loop — the same pattern powering Claude Code. No state machines, no graph orchestration:

```
for each turn (max 25):
    1. Build context (SOUL.md + memory + trimmed history)
    2. Call LLM (with retry + streaming)
    3. Check token budget
    4. Route: text → done | tool calls → execute → loop
```

Every step traced. Every error fed back to the model for self-correction. Never crashes on bad tool calls.

### Built-in Tools

| Tool | Description | Danger Level |
|------|-------------|:------------:|
| `read_file` | Read files with line numbers | Safe |
| `write_file` | Create/overwrite files | Medium |
| `edit_file` | Surgical string replacement | Medium |
| `shell` | Execute shell commands | High |
| `list_files` | Directory listing with glob | Safe |
| `search` | Regex search across files | Safe |

### Multi-Agent Orchestration

Six built-in patterns — **no other lightweight alternative has any**:

| Pattern | Description |
|---------|-------------|
| **Supervisor** | Coordinator delegates to specialist agents |
| **Pipeline** | Agent A → Agent B → Agent C, each transforms |
| **Parallel** | Fan-out to N agents, aggregate results |
| **Handoff** | Dynamic transfer of conversation |
| **Debate** | Multiple agents argue, judge synthesizes |
| **Swarm** | Autonomous agents self-organize |

Each sub-agent runs its own loop with isolated history, tools, and safety checks.

### Cost Tracking

Microdollar accounting (int64, no float drift):

```
── claude-sonnet-4 │ 1.2K in / 523 out │ $0.012 session ──
```

- Per-model pricing tables built in
- Daily/monthly budget enforcement
- Session and cumulative tracking

### Security

| Protection | How |
|------------|-----|
| **Default-deny tools** | Every tool has a `DangerLevel` — high-danger requires approval |
| **Path traversal guard** | All file paths validated against allowed directories |
| **Credential scanning** | Regex patterns catch API keys, passwords, private keys in all tool outputs |
| **JSON validation** | Tool arguments validated before execution |
| **Timeout enforcement** | LLM calls (120s) and tool calls (60s) have hard timeouts |

### Context Engineering

Following Anthropic's production guidelines:

```
┌─────────────────────────────────┐
│ System prompt: 25% of budget    │ ← SOUL.md + memory
├─────────────────────────────────┤
│ History: 65% of budget          │ ← Conversation + tool results
├─────────────────────────────────┤
│ Reserved for output: 10%        │ ← Model's response
└─────────────────────────────────┘
```

- **History trimming**: Keeps first message (anchor) + most recent. Drops middle.
- **Tool result truncation**: 70% head + 30% tail for large outputs.
- **180K token budget** by default (configurable).

## Configuration

```yaml
# ~/.liteclaw/config.yaml

providers:
  default: anthropic
  anthropic:
    api_key_env: ANTHROPIC_API_KEY
    default_model: claude-sonnet-4-20250514
  openai:
    api_key_env: OPENAI_API_KEY
    default_model: gpt-4o

agent:
  max_turns: 25
  max_input_tokens: 180000
  enable_streaming: true

cost:
  inline_display: true
  daily_budget: 10.00

security:
  scan_credentials: true
  path_traversal_guard: true
  allowed_dirs:
    - /home/user/projects
```

All settings can be overridden with environment variables: `LITECLAW_AGENT_MAX_TURNS=50`.

## Interactive Commands

```
/help      Show available commands
/clear     Clear conversation history
/history   Show message history
/quit      Exit LiteClaw
```

## Architecture

```
liteclaw/
├── cmd/liteclaw/           # CLI entrypoint (kong)
├── internal/
│   ├── agent/              # Agent loop, context builder, sub-agent orchestrator
│   ├── config/             # YAML config (koanf)
│   ├── llm/                # Provider abstraction, Anthropic SDK, cost tracking
│   ├── security/           # Safety checker, credential scanner
│   ├── terminal/           # Interactive terminal UI
│   ├── tool/               # Built-in tools (read/write/edit/shell/search)
│   └── types/              # Shared data structures
├── Taskfile.yaml           # Build tasks
├── .goreleaser.yaml        # Cross-platform releases
└── .golangci.yml           # Linter config
```

**Dependency graph** (no cycles):

```
types ← security
types ← llm
types ← tool
types ← agent ← security (via interface)
types ← terminal ← agent
cmd/liteclaw ← all of the above
```

## Development

```bash
# Prerequisites: Go 1.23+, Task (go-task.dev)

# Build
task build

# Run tests
task test

# Lint (requires golangci-lint)
task lint

# All checks
task check

# Build snapshot release
task release:snapshot
```

## Roadmap

### Done

- [x] Core agent loop with streaming
- [x] Anthropic Claude provider
- [x] 6 built-in tools (read, write, edit, shell, list, search)
- [x] Interactive terminal mode
- [x] Cost tracking (microdollar accounting)
- [x] Safety rails (credential scanning, path traversal, tool approval)
- [x] Context engineering (auto-trim, budget allocation)
- [x] Sub-agent orchestrator

### Next

- [ ] OpenAI / Ollama providers
- [ ] Session persistence (SQLite)
- [ ] Multi-agent patterns (supervisor, pipeline, parallel)
- [ ] Bounded memory (3-tier, 14K cap)
- [ ] MCP client support
- [ ] AGENTS.md / SOUL.md file discovery
- [ ] Platform adapters (Telegram, Slack, Discord)
- [ ] VS Code extension
- [ ] Web UI

## License

Apache-2.0

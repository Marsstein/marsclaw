# Contributing to MarsClaw

Thanks for your interest in contributing! MarsClaw is a lightweight AI agent runtime, and we value contributions that keep it that way — small, fast, and clean.

## Getting Started

```bash
git clone https://github.com/marsstein/marsclaw.git
cd marsclaw
go build ./cmd/marsclaw/
```

Requirements: Go 1.23+

## How to Contribute

### Bug Reports

Open an issue with:
- What you expected
- What happened
- Steps to reproduce
- Go version and OS

### Feature Requests

Open an issue describing the use case. We prioritize features that:
- Keep the binary small
- Don't add external dependencies
- Serve the "run from any device" mission

### Pull Requests

1. Fork the repo
2. Create a branch: `git checkout -b my-feature`
3. Make your changes
4. Run checks: `go vet ./... && go build ./cmd/marsclaw/`
5. Commit with a clear message
6. Open a PR against `main`

### Code Style

- Follow existing patterns in the codebase
- No deep nesting (max 2 levels)
- Single responsibility per function
- Keep it flat — flat over nested
- No unnecessary abstractions
- Delete unused code

### What We Won't Merge

- Large dependency additions
- Features that significantly increase binary size
- Breaking changes without discussion
- Code without clear purpose

## Project Structure

```
internal/          All packages are internal (no public API yet)
├── agent/         Core agent loop
├── llm/           LLM providers
├── tool/          Built-in tools
├── security/      Safety rails
├── server/        HTTP server + Web UI
├── store/         SQLite persistence
├── mcp/           MCP client
├── hooks/         Event hooks
└── types/         Shared types (breaks import cycles)
```

## License

By contributing, you agree that your contributions will be licensed under the Apache-2.0 License.

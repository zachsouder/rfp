# CLAUDE.md

This file provides guidance to Claude Code when working with code in this repository.

## Project Overview

RFP Intelligence Platform - a two-service system for discovering and managing RFP opportunities in the parking/event operations industry.

**Read PROJECT_SPEC.md first** - it contains the complete specification including architecture, database schema, features, and implementation phases.

## Architecture

Two services, one monorepo:

1. **Discovery Service** (`discovery/`): Autonomous search and research pipeline
   - Finds RFPs via Gemini API with Google Search grounding
   - Validates URLs, extracts details, downloads PDFs
   - Stores raw RFPs in `discovery.*` schema
   - Runs daily via scheduler

2. **Client Application** (`client/`): Per-client web application
   - Views and manages RFPs from discovery
   - Client-specific scoring, stages, notes
   - Stores data in `client.*` schema
   - Each client gets their own deployment

Shared code lives in `shared/`.

## Tech Stack

- **Language**: Go 1.22+
- **Database**: PostgreSQL 16
- **Storage**: Cloudflare R2 (S3-compatible)
- **External API**: Google Gemini
- **Frontend**: Server-rendered Go templates + Tailwind CSS
- **Deployment**: Docker + Coolify

## Project Structure

```
rfp/
├── discovery/           # Discovery service
│   ├── cmd/discovery/   # Entry point
│   └── internal/        # Business logic
├── client/              # Client application
│   ├── cmd/client/      # Entry point
│   ├── internal/        # Business logic
│   └── web/             # Templates, static
├── shared/              # Shared code
│   ├── db/              # Database helpers
│   ├── models/          # Shared types
│   └── config/          # Config loading
├── migrations/          # SQL migrations
├── cli/                 # CLI tools
└── docker-compose.yml
```

## Development Commands

```bash
# Start local environment (Postgres)
docker-compose up -d

# Run database migrations
make migrate

# Run discovery service
make run-discovery

# Run client app
make run-client

# Run both services
make dev

# Run tests
make test

# Build binaries
make build
```

## Database

Two PostgreSQL schemas:

- `discovery.*` - Raw RFPs, search results, research steps (shared data)
- `client.*` - Users, tracking, scores, notes (per-client data)

Migrations are in `migrations/` directory, numbered sequentially.

## Key Patterns

### Go Module Structure

Each service is its own Go module:
```
discovery/go.mod
client/go.mod
shared/go.mod
```

Services import shared: `import "github.com/yourorg/rfp/shared/models"`

### Error Handling

Use structured errors with context:
```go
if err != nil {
    return fmt.Errorf("failed to fetch URL %s: %w", url, err)
}
```

### Database Access

Use `shared/db` for connection management. Prefer explicit SQL over ORM:
```go
row := db.QueryRow(ctx, "SELECT id, title FROM discovery.rfps WHERE id = $1", id)
```

### Configuration

Load from environment via `shared/config`:
```go
cfg := config.Load()
// cfg.DatabaseURL, cfg.GeminiAPIKey, etc.
```

### HTTP Handlers

Use standard library `net/http`. Structure handlers in `internal/handlers/`:
```go
func (h *Handlers) RFPDetail(w http.ResponseWriter, r *http.Request) {
    // ...
}
```

### Templates

Go templates in `client/web/templates/`. Embed for single-binary deployment:
```go
//go:embed templates/*
var templates embed.FS
```

## Important Context

### Porting from PHP Demo

This project is a rewrite of a PHP prototype in `../rfp-demo/`. Key logic to port:

1. **Research Agent** (`includes/ResearchAgent.php`) - multi-step agent that investigates search results
2. **Scoring Heuristic** - event-focused scoring rules
3. **Deduplication** (`includes/DeduplicationService.php`) - fuzzy matching

Don't port the architecture (single-file PHP), just the business logic.

### The "Can't See Findings" Problem

The CLI tools (`cli/`) should make it easy to inspect production data:
- See recent discoveries
- Debug research failures
- Export data for analysis

This is important because dev happens locally but discoveries happen in production.

### Multi-Client Future

The system is designed to support multiple clients:
- Discovery runs once, shared across all clients
- Each client has their own deployment of the client app
- For now, focus on single client - structure allows expansion

## Coding Guidelines

- Keep handlers thin, business logic in `internal/`
- Log structured data (JSON) for production debugging
- Use context for cancellation and request-scoped values
- Validate input at handler level, trust internal functions
- Write table-driven tests for business logic

## Issue Tracking

Use beads for issue tracking:
```bash
bd ready              # Find available work
bd show <id>          # View issue details
bd update <id> --status in_progress  # Claim work
bd close <id>         # Complete work
bd sync --flush-only  # Export to JSONL
```

## Landing the Plane (Session Completion)

**When ending a work session**, you MUST complete ALL steps below. Work is NOT complete until `git push` succeeds.

**MANDATORY WORKFLOW:**

1. **File issues for remaining work** - Create issues for anything that needs follow-up
2. **Run quality gates** (if code changed) - Tests, linters, builds
3. **Update issue status** - Close finished work, update in-progress items
4. **PUSH TO REMOTE** - This is MANDATORY:
   ```bash
   git pull --rebase
   bd sync
   git push
   git status  # MUST show "up to date with origin"
   ```
5. **Clean up** - Clear stashes, prune remote branches
6. **Verify** - All changes committed AND pushed
7. **Hand off** - Provide context for next session

**CRITICAL RULES:**
- Work is NOT complete until `git push` succeeds
- NEVER stop before pushing - that leaves work stranded locally
- NEVER say "ready to push when you are" - YOU must push
- If push fails, resolve and retry until it succeeds

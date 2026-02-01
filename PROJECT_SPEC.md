# RFP Intelligence Platform - Project Specification

## Overview

A two-service platform for discovering and managing RFP opportunities in the parking/event operations industry. The system consists of:

1. **Discovery Service**: Autonomous search and research pipeline that finds RFPs across the industry, validates them, extracts details, and stores raw data. Runs as a shared resource - one instance serves all clients.

2. **Client Application**: Per-client web application for viewing, scoring, and managing RFPs through a proposal pipeline. Each client gets their own deployment with custom scoring rules, their own users, and optionally their own domain.

The Discovery Service is the **data asset** - its value increases as clients are added because search/research costs are amortized. Client Applications are **customized views** into that shared intelligence.

---

## Technology Stack

### Backend
- **Language**: Go 1.22+
- **Database**: PostgreSQL 16
- **File Storage**: Cloudflare R2 (S3-compatible, for PDFs and attachments)
- **External APIs**: Google Gemini (search with grounding, structured extraction)

### Frontend
- **Rendering**: Server-rendered HTML (Go templates)
- **Styling**: Tailwind CSS
- **JavaScript**: Vanilla JS, minimal progressive enhancement

### Deployment
- **Hosting**: Hetzner VPS (or similar)
- **Containerization**: Docker
- **Orchestration**: Coolify (or docker-compose for simpler setups)

### Development
- **Issue Tracking**: beads (git-backed, in-repo)
- **Local Dev**: docker-compose with Postgres, hot reload

---

## Project Structure

```
rfp/
├── PROJECT_SPEC.md          # This file
├── CLAUDE.md                # Agent instructions
├── .beads/                  # Issue tracking
├── .env                     # Environment variables (not committed)
├── .env.example             # Template for env vars
│
├── discovery/               # Discovery Service
│   ├── cmd/
│   │   └── discovery/       # Main entry point
│   │       └── main.go
│   ├── internal/
│   │   ├── search/          # Gemini search with grounding
│   │   ├── research/        # Multi-step research agent
│   │   ├── validation/      # URL validation, login detection
│   │   ├── dedup/           # Fuzzy deduplication
│   │   ├── pdf/             # PDF discovery and download
│   │   └── scheduler/       # Cron-like job runner
│   └── go.mod
│
├── client/                  # Client Application
│   ├── cmd/
│   │   └── client/          # Main entry point (web server)
│   │       └── main.go
│   ├── internal/
│   │   ├── auth/            # Authentication, sessions
│   │   ├── scoring/         # Client-specific scoring logic
│   │   ├── workflow/        # Pipeline stages, transitions
│   │   ├── handlers/        # HTTP handlers
│   │   └── middleware/      # Auth, logging, etc.
│   ├── web/
│   │   ├── templates/       # Go HTML templates
│   │   ├── static/          # CSS, JS, images
│   │   └── embed.go         # Embed static assets
│   └── go.mod
│
├── shared/                  # Shared code (imported by both services)
│   ├── db/                  # Database connection, query helpers
│   ├── models/              # Shared types (RFP, Source, etc.)
│   ├── config/              # Environment loading
│   ├── r2/                  # R2/S3 client for file storage
│   └── go.mod
│
├── migrations/              # PostgreSQL migrations (numbered)
│   ├── 001_discovery_schema.sql
│   ├── 002_client_schema.sql
│   └── ...
│
├── cli/                     # CLI tools for operations
│   ├── cmd/
│   │   └── rfp-cli/
│   │       └── main.go
│   └── go.mod
│
├── docker-compose.yml       # Local dev environment
├── Dockerfile.discovery     # Discovery service container
├── Dockerfile.client        # Client app container
└── Makefile                 # Common commands
```

---

## Database Schema

### Discovery Schema (`discovery.*`)

Stores raw RFP data found by the search pipeline. No client-specific data here.

```sql
-- Search queries that have been run
CREATE TABLE discovery.search_queries (
    id              SERIAL PRIMARY KEY,
    query_text      TEXT NOT NULL,
    query_config_id INTEGER REFERENCES discovery.search_query_configs(id),
    executed_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    results_count   INTEGER DEFAULT 0,
    status          TEXT DEFAULT 'completed'  -- 'running', 'completed', 'failed'
);

-- Configurable search query templates
CREATE TABLE discovery.search_query_configs (
    id              SERIAL PRIMARY KEY,
    name            TEXT NOT NULL,
    query_template  TEXT NOT NULL,  -- e.g., "parking RFP {state} site:bonfirehub.com"
    enabled         BOOLEAN DEFAULT true,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Raw search results before research
CREATE TABLE discovery.search_results (
    id              SERIAL PRIMARY KEY,
    query_id        INTEGER REFERENCES discovery.search_queries(id),
    url             TEXT NOT NULL,
    title           TEXT,
    snippet         TEXT,

    -- Validation status
    url_validated   BOOLEAN DEFAULT false,
    url_valid       BOOLEAN,
    final_url       TEXT,  -- After redirects
    content_type    TEXT,  -- 'rfp_page', 'portal_listing', 'login_wall', 'pdf', 'other'

    -- Extracted hints (pre-research)
    hint_agency     TEXT,
    hint_state      TEXT,
    hint_due_date   DATE,

    -- Research status
    research_status TEXT DEFAULT 'pending',  -- 'pending', 'in_progress', 'completed', 'failed', 'skipped'
    promoted_rfp_id INTEGER REFERENCES discovery.rfps(id),
    duplicate_of_id INTEGER REFERENCES discovery.rfps(id),

    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Research steps (observable agent reasoning)
CREATE TABLE discovery.research_steps (
    id              SERIAL PRIMARY KEY,
    search_result_id INTEGER REFERENCES discovery.search_results(id),
    step_number     INTEGER NOT NULL,
    action          TEXT NOT NULL,  -- 'fetch_page', 'extract_details', 'find_pdf', 'check_login', 'decide'
    input_summary   TEXT,
    output_summary  TEXT,
    reasoning       TEXT,  -- Why this action was taken
    success         BOOLEAN,
    error_message   TEXT,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Raw RFPs (the shared data asset)
CREATE TABLE discovery.rfps (
    id              SERIAL PRIMARY KEY,

    -- Core identification
    title           TEXT NOT NULL,
    agency          TEXT,
    state           TEXT,
    city            TEXT,

    -- Source information
    source_url      TEXT,
    portal          TEXT,  -- 'bonfire', 'opengov', 'bidnet', 'planetbids', 'direct', etc.
    portal_id       TEXT,  -- ID within the portal system

    -- Dates
    posted_date     DATE,
    due_date        DATE,

    -- Classification (raw, not scored)
    category        TEXT,  -- 'parking', 'valet', 'event_ops', 'transit', 'enforcement', etc.
    venue_type      TEXT,  -- 'arena', 'stadium', 'convention_center', 'airport', 'municipal', etc.
    scope_keywords  TEXT[],  -- ['event parking', 'vip valet', 'traffic control']

    -- Contract details
    term_months     INTEGER,
    estimated_value DECIMAL(12,2),
    incumbent       TEXT,

    -- Access
    login_required  BOOLEAN DEFAULT false,
    login_notes     TEXT,

    -- Documents
    pdf_urls        TEXT[],  -- Direct links to PDF documents

    -- Metadata
    raw_content     TEXT,  -- Full extracted text for search
    discovered_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_checked    TIMESTAMPTZ,
    is_active       BOOLEAN DEFAULT true  -- False if RFP closed/removed
);

-- Sources being monitored (for future portal-specific ingestion)
CREATE TABLE discovery.sources (
    id              SERIAL PRIMARY KEY,
    name            TEXT NOT NULL,
    source_type     TEXT NOT NULL,  -- 'gemini_search', 'portal_scrape', 'manual'
    config          JSONB,  -- Source-specific configuration
    enabled         BOOLEAN DEFAULT true,
    last_run        TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
```

### Client Schema (`client.*`)

Each client deployment uses this schema with their own data. In a multi-client setup, could use separate databases or schema prefixes.

```sql
-- Users for this client
CREATE TABLE client.users (
    id              SERIAL PRIMARY KEY,
    email           TEXT UNIQUE NOT NULL,
    password_hash   TEXT NOT NULL,
    first_name      TEXT,
    last_name       TEXT,
    role            TEXT DEFAULT 'member',  -- 'admin', 'member'
    last_active_at  TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Sessions
CREATE TABLE client.sessions (
    id              TEXT PRIMARY KEY,  -- Secure random token
    user_id         INTEGER REFERENCES client.users(id) ON DELETE CASCADE,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expires_at      TIMESTAMPTZ NOT NULL
);

-- Client's view of an RFP (references discovery.rfps)
CREATE TABLE client.rfp_tracking (
    id              SERIAL PRIMARY KEY,
    discovery_rfp_id INTEGER NOT NULL,  -- References discovery.rfps(id)

    -- Pipeline stage
    stage           TEXT DEFAULT 'new',  -- 'new', 'reviewing', 'qualified', 'pursuing', 'submitted', 'won', 'lost', 'passed'
    stage_changed_at TIMESTAMPTZ,
    stage_changed_by INTEGER REFERENCES client.users(id),

    -- Scoring (client-specific)
    auto_score      DECIMAL(3,1),  -- 1.0-5.0, calculated by scoring rules
    manual_score    DECIMAL(3,1),  -- Override by user
    score_reasons   JSONB,  -- Breakdown of scoring factors

    -- Workflow
    assigned_to     INTEGER REFERENCES client.users(id),
    priority        TEXT DEFAULT 'normal',  -- 'high', 'normal', 'low'
    decision_date   DATE,  -- Internal deadline for go/no-go

    -- Tracking
    is_hidden       BOOLEAN DEFAULT false,  -- Hide from default views
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    UNIQUE(discovery_rfp_id)  -- One tracking record per RFP per client
);

-- Notes on RFPs
CREATE TABLE client.notes (
    id              SERIAL PRIMARY KEY,
    rfp_tracking_id INTEGER REFERENCES client.rfp_tracking(id) ON DELETE CASCADE,
    author_id       INTEGER REFERENCES client.users(id),
    content         TEXT NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Uploaded attachments (proposals, analysis, etc.)
CREATE TABLE client.attachments (
    id              SERIAL PRIMARY KEY,
    rfp_tracking_id INTEGER REFERENCES client.rfp_tracking(id) ON DELETE CASCADE,
    uploaded_by     INTEGER REFERENCES client.users(id),
    filename        TEXT NOT NULL,
    file_path       TEXT NOT NULL,  -- R2 path
    file_size       INTEGER,
    content_type    TEXT,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Client-specific scoring configuration
CREATE TABLE client.scoring_rules (
    id              SERIAL PRIMARY KEY,
    name            TEXT NOT NULL,
    description     TEXT,
    rule_type       TEXT NOT NULL,  -- 'venue_match', 'scope_match', 'geography', 'term_length', etc.
    config          JSONB NOT NULL,  -- Rule-specific config (keywords, weights, etc.)
    weight          DECIMAL(3,2) DEFAULT 1.0,  -- Relative weight in scoring
    enabled         BOOLEAN DEFAULT true,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Email subscription preferences
CREATE TABLE client.email_subscriptions (
    id              SERIAL PRIMARY KEY,
    user_id         INTEGER REFERENCES client.users(id) ON DELETE CASCADE,
    digest_enabled  BOOLEAN DEFAULT true,
    digest_frequency TEXT DEFAULT 'daily',  -- 'daily', 'weekly', 'never'
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
```

---

## Discovery Service

### Purpose

Autonomously find RFPs across the parking/event operations industry, validate them, extract details, and store raw data for client applications to consume.

### Search Strategy

Use Gemini API with Google Search grounding to find RFPs. This approach:
- Avoids scraping/TOS issues
- Gets real-time results
- Handles diverse portal formats

**Query Templates** (configurable):
```
parking RFP {state}
valet services RFP
event parking management RFP
stadium parking RFP
arena parking services bid
convention center parking RFP
parking management services site:bonfirehub.com
parking RFP site:opengov.com
```

### Research Pipeline

For each search result, run a multi-step research agent:

1. **URL Validation**: HEAD request to check URL works, follow redirects, detect content type
2. **Page Fetch**: GET the page, extract text content
3. **Login Detection**: Check for login walls, paywalls, restricted content
4. **Detail Extraction**: Use Gemini structured output to extract:
   - Title, agency, location
   - Posted date, due date
   - Category, venue type
   - Contract term, estimated value
   - Incumbent (if mentioned)
5. **PDF Discovery**: Find linked PDFs, download and store in R2
6. **Deduplication**: Fuzzy match against existing RFPs (agency + state + due date)
7. **Storage**: Insert into `discovery.rfps` if unique

### Observable Reasoning

Every step is logged to `research_steps` with:
- What action was taken
- Why (reasoning)
- What was found
- Success/failure

This enables debugging and refinement without re-running searches.

### Scheduling

Run daily (configurable). Process:
1. Execute all enabled query configs
2. Validate and research new results (with concurrency limit)
3. Skip already-processed URLs
4. Log summary stats

### CLI Tools

```bash
# Check recent search stats
rfp-cli discovery stats

# View research steps for a specific result
rfp-cli discovery inspect <result-id>

# Manually research a URL
rfp-cli discovery research <url>

# Retry failed research
rfp-cli discovery retry-failed

# List recent discoveries
rfp-cli discovery recent --days=7

# Export findings for analysis
rfp-cli discovery export --format=json --since=2024-01-01
```

---

## Client Application

### Purpose

Provide a web interface for a client's team to view, score, and manage RFPs through their proposal pipeline.

### User Roles

- **Admin**: Can manage users, configure scoring rules, access all features
- **Member**: Can view RFPs, add notes, move through pipeline

For v1, roles are simple. The platform admin (you) has database access for anything beyond this.

### Features

#### Dashboard
- Summary stats: new RFPs this week, in pipeline, upcoming deadlines
- Action items: RFPs needing review, approaching due dates
- Recent activity feed

#### RFP List
- Filterable by: stage, score range, state, category, due date
- Sortable by: score, due date, posted date, title
- Quick actions: change stage, assign, hide

#### RFP Detail
- Full RFP information from discovery
- Client-specific: score breakdown, stage history, assigned user
- Notes section
- Attachments (uploaded proposals, analysis)
- Links to source and PDFs

#### Pipeline View
- Kanban-style board of RFPs by stage
- Drag to move between stages
- Filter by assignee, score

#### Scoring Configuration
- View/edit scoring rules
- Test scoring against sample RFPs
- See score distribution

#### User Management (Admin)
- Invite users by email
- Deactivate users
- View activity

### Scoring System

Scoring is **client-specific**. Each client configures rules that weight factors differently.

Default scoring rules for parking/event operations:

| Factor | Weight | Logic |
|--------|--------|-------|
| Venue Match | 30% | Arena, stadium, amphitheater, convention center = high score |
| Scope Fit | 25% | Event parking, VIP valet, traffic control = high score |
| Geography | 15% | Configurable preferred states |
| Term Length | 10% | Longer terms score higher |
| Time to Due | 10% | ≥14 days = full points, <7 days = penalty |
| Value | 10% | Higher estimated value = higher score |

**Negative signals** (reduce score):
- On-street enforcement only
- PARCS/hardware focus
- Design-build projects
- Regular commercial garage (no events)

Scoring runs automatically when an RFP is first tracked. Users can override with manual score.

### Authentication

- Email + password login
- Long session duration (30 days)
- Password reset via email
- No self-registration (admin invites users)

### Pages and Routes

```
GET  /login                    Login page
POST /login                    Authenticate
GET  /logout                   End session

GET  /                         Dashboard
GET  /rfps                     RFP list (with filters)
GET  /rfps/:id                 RFP detail
POST /rfps/:id/stage           Update stage
POST /rfps/:id/score           Set manual score
POST /rfps/:id/assign          Assign to user
POST /rfps/:id/notes           Add note
POST /rfps/:id/attachments     Upload attachment

GET  /pipeline                 Pipeline/Kanban view

GET  /settings                 User settings
POST /settings                 Update settings
POST /settings/password        Change password

GET  /admin/users              User management (admin)
POST /admin/users/invite       Send invite (admin)
POST /admin/users/:id/deactivate  Deactivate user (admin)

GET  /admin/scoring            Scoring rules (admin)
POST /admin/scoring            Update rules (admin)
```

---

## Design System

### Principles
- Clean, professional, not flashy
- Works on mobile, tablet, desktop
- Fast page loads (server-rendered, minimal JS)
- Accessible to less technical users
- Information-dense but not cluttered

### Color Palette
- **Primary**: Navy (#1e3a5f) - headers, primary buttons
- **Secondary**: Slate gray for secondary elements
- **Background**: Light gray (#f8fafc)
- **Surface**: White for cards
- **Accent**: Blue (#3b82f6) for links, highlights
- **Success**: Green (#22c55e)
- **Warning**: Amber (#f59e0b)
- **Danger**: Red (#ef4444)

### Typography
- **Headings**: Inter or system sans-serif, semibold
- **Body**: Inter or system sans-serif, regular
- **Monospace**: For IDs, technical details

### Components
- Cards with subtle shadows for RFP items
- Badges for stages (color-coded)
- Score display: filled/empty circles or numeric
- Tables with sticky headers for lists
- Modal dialogs for confirmations
- Toast notifications for feedback

---

## Development Workflow

### Local Development

```bash
# Start Postgres and services
docker-compose up -d

# Run migrations
make migrate

# Start discovery service (watches for changes)
make run-discovery

# Start client app (watches for changes)
make run-client

# Run both
make dev
```

### Environment Variables

```bash
# Database
DATABASE_URL=postgres://user:pass@localhost:5432/rfp

# Gemini API
GEMINI_API_KEY=your-key

# R2 Storage
R2_ACCOUNT_ID=your-account
R2_ACCESS_KEY_ID=your-key
R2_SECRET_ACCESS_KEY=your-secret
R2_BUCKET=rfp-documents

# Client app
SESSION_SECRET=random-secret
SMTP_HOST=smtp.example.com
SMTP_USER=...
SMTP_PASS=...

# Optional
LOG_LEVEL=debug
```

### Operational CLI

Address the "can't see findings" problem - CLI tools that work against production:

```bash
# Connect to production
export DATABASE_URL=postgres://...production...

# See what discovery found recently
rfp-cli discovery recent --days=3

# See research failures to understand what's not working
rfp-cli discovery failures --days=7

# Export data for local analysis
rfp-cli discovery export --format=csv --since=2024-01-01 > rfps.csv

# Check search query effectiveness
rfp-cli discovery query-stats

# See which portals are yielding results
rfp-cli discovery source-stats
```

---

## Implementation Phases

### Phase 1: Foundation
- [ ] Project structure, Go modules, docker-compose
- [ ] Postgres schema (both schemas)
- [ ] Database connection and migrations
- [ ] Shared models and config loading
- [ ] Basic CLI structure

### Phase 2: Discovery Service
- [ ] Gemini search integration
- [ ] URL validation
- [ ] Research agent (port from PHP, improve)
- [ ] Deduplication logic
- [ ] PDF download to R2
- [ ] Scheduler for daily runs
- [ ] CLI tools for inspection

### Phase 3: Client Application - Core
- [ ] Web server setup
- [ ] Authentication (login, sessions, password reset)
- [ ] Dashboard page
- [ ] RFP list with filters
- [ ] RFP detail page
- [ ] Stage management

### Phase 4: Client Application - Features
- [ ] Notes and attachments
- [ ] Scoring system with configurable rules
- [ ] Pipeline/Kanban view
- [ ] User management
- [ ] Email notifications (digest)

### Phase 5: Polish and Deploy
- [ ] Production Dockerfile and docker-compose
- [ ] Coolify deployment config
- [ ] Domain setup for first client
- [ ] Seed data migration from PHP demo
- [ ] User documentation

---

## Migration from PHP Demo

The PHP demo has working logic that should be ported:

### Port Directly
- Search query templates (from `search_query_config` table)
- Scoring heuristic logic (adapt to configurable rules)
- Research agent step structure

### Improve While Porting
- Research agent: better login detection, smarter PDF finding
- Deduplication: currently fuzzy match, consider additional signals
- Error handling: more structured, better logging

### Don't Port
- Single-file architecture
- Auto-registration (use invite-only)
- Inline CSS/JS (use proper templates)

### Data Migration
For the first client, export RFPs from PHP demo and import into new system:
```bash
# Export from PHP demo
php cli/export_rfps.php > rfps.json

# Import into new system
rfp-cli import rfps.json
```

---

## Success Criteria

### Discovery Service
- [ ] Finds real RFPs daily without manual intervention
- [ ] Research steps are logged and inspectable
- [ ] Deduplication prevents repeated entries
- [ ] PDFs are downloaded and stored reliably
- [ ] Can diagnose issues via CLI without touching production DB

### Client Application
- [ ] Users can log in and view RFPs
- [ ] RFPs can be moved through pipeline stages
- [ ] Scoring provides useful prioritization
- [ ] Notes and attachments work
- [ ] Works well on mobile

### Operational
- [ ] Local dev environment works reliably
- [ ] Production deployment is repeatable
- [ ] Can add second client without major changes
- [ ] CLI tools provide visibility into production

---

## Future Considerations

### When Adding Client #2
- Decide: separate database or schema prefix
- Domain routing (client1.rfps.com vs client2.rfps.com)
- Shared discovery, separate client schemas

### Potential Features (Not v1)
- Email notifications when high-score RFPs discovered
- Calendar integration for due dates
- Proposal template library
- Win/loss analysis
- Portal-specific scrapers (if Gemini search insufficient)
- Mobile app (PWA)

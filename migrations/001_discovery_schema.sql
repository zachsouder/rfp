-- Discovery Schema
-- Stores raw RFP data found by the search pipeline
-- No client-specific data here

CREATE SCHEMA IF NOT EXISTS discovery;

-- Configurable search query templates
CREATE TABLE discovery.search_query_configs (
    id              SERIAL PRIMARY KEY,
    name            TEXT NOT NULL,
    query_template  TEXT NOT NULL,
    enabled         BOOLEAN DEFAULT true,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Search queries that have been run
CREATE TABLE discovery.search_queries (
    id              SERIAL PRIMARY KEY,
    query_text      TEXT NOT NULL,
    query_config_id INTEGER REFERENCES discovery.search_query_configs(id),
    executed_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    results_count   INTEGER DEFAULT 0,
    status          TEXT DEFAULT 'completed'
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
    portal          TEXT,
    portal_id       TEXT,

    -- Dates
    posted_date     DATE,
    due_date        DATE,

    -- Classification (raw, not scored)
    category        TEXT,
    venue_type      TEXT,
    scope_keywords  TEXT[],

    -- Contract details
    term_months     INTEGER,
    estimated_value DECIMAL(12,2),
    incumbent       TEXT,

    -- Access
    login_required  BOOLEAN DEFAULT false,
    login_notes     TEXT,

    -- Documents
    pdf_urls        TEXT[],

    -- Metadata
    raw_content     TEXT,
    discovered_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_checked    TIMESTAMPTZ,
    is_active       BOOLEAN DEFAULT true
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
    final_url       TEXT,
    content_type    TEXT,

    -- Extracted hints (pre-research)
    hint_agency     TEXT,
    hint_state      TEXT,
    hint_due_date   DATE,

    -- Research status
    research_status TEXT DEFAULT 'pending',
    promoted_rfp_id INTEGER REFERENCES discovery.rfps(id),
    duplicate_of_id INTEGER REFERENCES discovery.rfps(id),

    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Research steps (observable agent reasoning)
CREATE TABLE discovery.research_steps (
    id              SERIAL PRIMARY KEY,
    search_result_id INTEGER REFERENCES discovery.search_results(id),
    step_number     INTEGER NOT NULL,
    action          TEXT NOT NULL,
    input_summary   TEXT,
    output_summary  TEXT,
    reasoning       TEXT,
    success         BOOLEAN,
    error_message   TEXT,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Sources being monitored
CREATE TABLE discovery.sources (
    id              SERIAL PRIMARY KEY,
    name            TEXT NOT NULL,
    source_type     TEXT NOT NULL,
    config          JSONB,
    enabled         BOOLEAN DEFAULT true,
    last_run        TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Indexes
CREATE INDEX idx_rfps_state ON discovery.rfps(state);
CREATE INDEX idx_rfps_due_date ON discovery.rfps(due_date);
CREATE INDEX idx_rfps_discovered_at ON discovery.rfps(discovered_at);
CREATE INDEX idx_rfps_is_active ON discovery.rfps(is_active);
CREATE INDEX idx_search_results_research_status ON discovery.search_results(research_status);
CREATE INDEX idx_search_results_query_id ON discovery.search_results(query_id);

-- Seed some default query configs
INSERT INTO discovery.search_query_configs (name, query_template) VALUES
    ('Parking RFP General', 'parking management RFP'),
    ('Parking RFP by State', 'parking RFP {state}'),
    ('Valet Services', 'valet services RFP'),
    ('Event Parking', 'event parking management RFP'),
    ('Stadium Parking', 'stadium parking RFP'),
    ('Arena Parking', 'arena parking services bid'),
    ('Convention Center', 'convention center parking RFP'),
    ('Bonfire Portal', 'parking management services site:bonfirehub.com'),
    ('OpenGov Portal', 'parking RFP site:opengov.com');

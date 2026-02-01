-- Client Schema
-- Per-client data: users, tracking, scores, notes
-- Each client deployment uses this schema with their own data

CREATE SCHEMA IF NOT EXISTS client;

-- Users for this client
CREATE TABLE client.users (
    id              SERIAL PRIMARY KEY,
    email           TEXT UNIQUE NOT NULL,
    password_hash   TEXT NOT NULL,
    first_name      TEXT,
    last_name       TEXT,
    role            TEXT DEFAULT 'member',
    last_active_at  TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Sessions
CREATE TABLE client.sessions (
    id              TEXT PRIMARY KEY,
    user_id         INTEGER REFERENCES client.users(id) ON DELETE CASCADE,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expires_at      TIMESTAMPTZ NOT NULL
);

-- Client's view of an RFP (references discovery.rfps)
CREATE TABLE client.rfp_tracking (
    id              SERIAL PRIMARY KEY,
    discovery_rfp_id INTEGER NOT NULL,

    -- Pipeline stage
    stage           TEXT DEFAULT 'new',
    stage_changed_at TIMESTAMPTZ,
    stage_changed_by INTEGER REFERENCES client.users(id),

    -- Scoring (client-specific)
    auto_score      DECIMAL(3,1),
    manual_score    DECIMAL(3,1),
    score_reasons   JSONB,

    -- Workflow
    assigned_to     INTEGER REFERENCES client.users(id),
    priority        TEXT DEFAULT 'normal',
    decision_date   DATE,

    -- Tracking
    is_hidden       BOOLEAN DEFAULT false,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    UNIQUE(discovery_rfp_id)
);

-- Notes on RFPs
CREATE TABLE client.notes (
    id              SERIAL PRIMARY KEY,
    rfp_tracking_id INTEGER REFERENCES client.rfp_tracking(id) ON DELETE CASCADE,
    author_id       INTEGER REFERENCES client.users(id),
    content         TEXT NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Uploaded attachments
CREATE TABLE client.attachments (
    id              SERIAL PRIMARY KEY,
    rfp_tracking_id INTEGER REFERENCES client.rfp_tracking(id) ON DELETE CASCADE,
    uploaded_by     INTEGER REFERENCES client.users(id),
    filename        TEXT NOT NULL,
    file_path       TEXT NOT NULL,
    file_size       INTEGER,
    content_type    TEXT,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Client-specific scoring configuration
CREATE TABLE client.scoring_rules (
    id              SERIAL PRIMARY KEY,
    name            TEXT NOT NULL,
    description     TEXT,
    rule_type       TEXT NOT NULL,
    config          JSONB NOT NULL,
    weight          DECIMAL(3,2) DEFAULT 1.0,
    enabled         BOOLEAN DEFAULT true,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Email subscription preferences
CREATE TABLE client.email_subscriptions (
    id              SERIAL PRIMARY KEY,
    user_id         INTEGER REFERENCES client.users(id) ON DELETE CASCADE,
    digest_enabled  BOOLEAN DEFAULT true,
    digest_frequency TEXT DEFAULT 'daily',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Indexes
CREATE INDEX idx_rfp_tracking_stage ON client.rfp_tracking(stage);
CREATE INDEX idx_rfp_tracking_assigned_to ON client.rfp_tracking(assigned_to);
CREATE INDEX idx_rfp_tracking_is_hidden ON client.rfp_tracking(is_hidden);
CREATE INDEX idx_sessions_user_id ON client.sessions(user_id);
CREATE INDEX idx_sessions_expires_at ON client.sessions(expires_at);
CREATE INDEX idx_notes_rfp_tracking_id ON client.notes(rfp_tracking_id);

-- Seed default scoring rules for parking/event operations
INSERT INTO client.scoring_rules (name, description, rule_type, config, weight) VALUES
    (
        'Venue Match',
        'Score higher for event venues (arenas, stadiums, convention centers)',
        'venue_match',
        '{"positive": ["arena", "stadium", "amphitheater", "convention center", "performing arts", "coliseum"], "negative": ["commercial garage", "on-street"]}',
        0.30
    ),
    (
        'Scope Fit',
        'Score higher for event-related parking services',
        'scope_match',
        '{"positive": ["event parking", "vip valet", "traffic control", "gameday", "attendants"], "negative": ["enforcement", "citation", "PARCS", "hardware"]}',
        0.25
    ),
    (
        'Geography',
        'Preferred states/regions',
        'geography',
        '{"preferred_states": [], "excluded_states": []}',
        0.15
    ),
    (
        'Term Length',
        'Longer terms score higher',
        'term_length',
        '{"min_months": 12, "ideal_months": 36}',
        0.10
    ),
    (
        'Time to Due',
        'Penalize RFPs with short deadlines',
        'time_to_due',
        '{"min_days": 7, "ideal_days": 21}',
        0.10
    ),
    (
        'Estimated Value',
        'Higher value RFPs score higher',
        'value',
        '{"min_value": 100000, "ideal_value": 1000000}',
        0.10
    );

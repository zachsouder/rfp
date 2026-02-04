-- Password reset tokens
-- Tokens expire after 1 hour and are single-use

CREATE TABLE client.password_reset_tokens (
    id              SERIAL PRIMARY KEY,
    user_id         INTEGER REFERENCES client.users(id) ON DELETE CASCADE,
    token           TEXT UNIQUE NOT NULL,
    expires_at      TIMESTAMPTZ NOT NULL,
    used_at         TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_password_reset_tokens_token ON client.password_reset_tokens(token);
CREATE INDEX idx_password_reset_tokens_user_id ON client.password_reset_tokens(user_id);
CREATE INDEX idx_password_reset_tokens_expires_at ON client.password_reset_tokens(expires_at);

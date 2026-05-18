-- API Keys table
CREATE TABLE IF NOT EXISTS api_keys (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name        TEXT NOT NULL,
    key_hash    TEXT NOT NULL UNIQUE,
    key_prefix  TEXT NOT NULL,
    is_active   BOOLEAN NOT NULL DEFAULT TRUE,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_used_at TIMESTAMPTZ,
    expires_at  TIMESTAMPTZ,
    note        TEXT
);

-- Request logs table
CREATE TABLE IF NOT EXISTS request_logs (
    id              BIGSERIAL PRIMARY KEY,
    api_key_id      UUID REFERENCES api_keys(id) ON DELETE SET NULL,
    api_key_prefix  TEXT,
    model           TEXT,
    endpoint        TEXT,
    method          TEXT,
    status_code     INT,
    prompt_tokens   INT DEFAULT 0,
    completion_tokens INT DEFAULT 0,
    total_tokens    INT DEFAULT 0,
    latency_ms      INT,
    error_message   TEXT,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_request_logs_api_key_id ON request_logs(api_key_id);
CREATE INDEX IF NOT EXISTS idx_request_logs_created_at ON request_logs(created_at DESC);

ALTER TABLE request_logs ADD COLUMN IF NOT EXISTS cost_usd NUMERIC(12,8) DEFAULT 0;
ALTER TABLE api_keys ADD COLUMN IF NOT EXISTS token_limit BIGINT DEFAULT 0;
ALTER TABLE api_keys ADD COLUMN IF NOT EXISTS budget_usd NUMERIC(10,4) DEFAULT 0;
ALTER TABLE api_keys ADD COLUMN IF NOT EXISTS request_limit BIGINT DEFAULT 0;

-- Settings table (key-value store for admin-configurable options)
CREATE TABLE IF NOT EXISTS settings (
    key        TEXT PRIMARY KEY,
    value      TEXT NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

INSERT INTO settings (key, value) VALUES
    ('compression_mode', 'standard')
ON CONFLICT (key) DO NOTHING;

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

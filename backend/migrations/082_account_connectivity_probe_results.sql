-- 082: Store periodic account connectivity probe results (hi-style checks)

CREATE TABLE IF NOT EXISTS account_connectivity_probe_results (
    id               BIGSERIAL PRIMARY KEY,
    account_id       BIGINT NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    platform         VARCHAR(32) NOT NULL DEFAULT '',
    model_id         VARCHAR(128) NOT NULL DEFAULT '',
    status           VARCHAR(20) NOT NULL DEFAULT '',
    error_message    TEXT NOT NULL DEFAULT '',
    latency_ms       BIGINT NOT NULL DEFAULT 0,
    response_preview TEXT NOT NULL DEFAULT '',
    created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_acpr_account_created ON account_connectivity_probe_results (account_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_acpr_status_created ON account_connectivity_probe_results (status, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_acpr_created_at ON account_connectivity_probe_results (created_at);

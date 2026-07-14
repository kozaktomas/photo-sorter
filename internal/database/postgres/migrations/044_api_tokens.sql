-- api_tokens: long-lived, read-only bearer tokens for machine clients.
--
-- Motivation: the migration client (Kukátko) exports the whole library over
-- the HTTP API. The only credentials available before this table were a
-- 30-day session cookie and `Authorization: Bearer <sessionID>`, both tied to
-- a session row that the cleanup loop eventually deletes — an export job must
-- not die halfway because its session aged out.
--
-- Only the SHA-256 of the raw token is stored. The raw value is shown exactly
-- once, at creation time, and is unrecoverable afterwards. SHA-256 (rather
-- than bcrypt, which `users.password_hash` uses) is deliberate: the token is
-- 256 bits of crypto/rand, so it has no low-entropy structure to brute-force,
-- and it is verified on every single API request — a bcrypt round per request
-- would add ~100 ms of CPU to a 20k-photo export.
--
-- `scope` is constrained to 'read' today. The column exists so a future write
-- scope is an additive CHECK change rather than a table rewrite; the auth
-- middleware maps every token onto the `viewer` role and additionally refuses
-- any non-GET/HEAD/OPTIONS request, so a token cannot mutate data.
CREATE TABLE IF NOT EXISTS api_tokens (
    uid VARCHAR(32) PRIMARY KEY,
    name VARCHAR(255) NOT NULL,
    token_hash VARCHAR(64) NOT NULL UNIQUE,
    scope VARCHAR(16) NOT NULL DEFAULT 'read'
        CHECK (scope IN ('read')),
    -- The admin who minted the token. Nullable + ON DELETE SET NULL so
    -- deleting that user neither blocks the delete nor silently revokes a
    -- migration token that is mid-export (mirrors migration 043's treatment
    -- of album_share_links / smart_albums).
    created_by_user_uid VARCHAR(32) REFERENCES users(uid) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    -- NULL = never expires. A migration token is explicitly allowed to be
    -- immortal; the operator revokes it when the migration is done.
    expires_at TIMESTAMPTZ,
    -- Set by the auth path, throttled to at most one write per minute so a
    -- bulk export does not turn every read into a write.
    last_used_at TIMESTAMPTZ,
    -- Soft revoke: keeps the audit trail of which tokens ever existed.
    revoked_at TIMESTAMPTZ
);

-- The auth hot path looks a token up by its hash on every request. The UNIQUE
-- constraint above already provides the index, so no extra index is needed
-- for that lookup. This one supports the `api-tokens list` CLI view.
CREATE INDEX IF NOT EXISTS idx_api_tokens_created_at ON api_tokens(created_at DESC);

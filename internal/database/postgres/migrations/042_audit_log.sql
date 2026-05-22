-- Audit log: append-only record of mutating actions.
--
-- Records who/what/when/where for every successful mutation on the auth-
-- side API plus a small set of explicit security events
-- (login_failed, share_link_password_failed). Read-only GETs are never
-- logged. Each row carries the authenticated user_uid (NULL for
-- anonymous events), the symbolic action name, the affected entity
-- (type + uid, both optional), action-specific metadata as JSONB, plus
-- the client IP and User-Agent for forensic context.
--
-- user_uid is a FK back to users(uid) with ON DELETE SET NULL: deleting a
-- user must not break the audit trail, so the trail simply forgets which
-- specific user did the action while preserving everything else.
--
-- Retention is intentionally unbounded for v1; a future task will add a
-- retention policy or partition-based purge.

CREATE TABLE IF NOT EXISTS audit_log (
    id          BIGSERIAL    PRIMARY KEY,
    user_uid    VARCHAR(32)  NULL REFERENCES users(uid) ON DELETE SET NULL,
    action      TEXT         NOT NULL,
    entity_type TEXT         NULL,
    entity_uid  TEXT         NULL,
    metadata    JSONB        NOT NULL DEFAULT '{}'::jsonb,
    ip          TEXT         NULL,
    user_agent  TEXT         NULL,
    created_at  TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_audit_log_user_uid_created
    ON audit_log (user_uid, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_audit_log_action_created
    ON audit_log (action, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_audit_log_entity
    ON audit_log (entity_type, entity_uid);

CREATE INDEX IF NOT EXISTS idx_audit_log_created
    ON audit_log (created_at DESC);

-- Public album share links.
--
-- Each row holds a per-album shareable URL. The slug is the URL component
-- (PRIMARY KEY, ASCII slug-style). Password is optional and stored as a
-- bcrypt hash; expires_at is optional (NULL = no expiration). created_by_
-- user_uid records the user who minted the link for audit / future per-
-- user listing. The FK to albums(uid) cascades, so deleting an album also
-- revokes every share link pointing at it.

CREATE TABLE IF NOT EXISTS album_share_links (
    slug                VARCHAR(64)  PRIMARY KEY,
    album_uid           VARCHAR(32)  NOT NULL REFERENCES albums(uid) ON DELETE CASCADE,
    password_hash       TEXT         NULL,
    expires_at          TIMESTAMPTZ  NULL,
    created_at          TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    created_by_user_uid VARCHAR(32)  NOT NULL REFERENCES users(uid),
    CHECK (slug ~ '^[a-z0-9-]{3,64}$')
);

CREATE INDEX IF NOT EXISTS idx_album_share_links_album_uid
    ON album_share_links (album_uid);

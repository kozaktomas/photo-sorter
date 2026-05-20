-- Add role column to sessions so the request-path middleware can resolve
-- the caller's role without a second DB hit. Default 'viewer' so any
-- pre-existing rows (from the PhotoPrism-era login flow) are treated as
-- read-only until they expire and are recreated by a native login.
ALTER TABLE sessions ADD COLUMN IF NOT EXISTS role VARCHAR(16) NOT NULL DEFAULT 'viewer';

-- Add user_uid column to sessions for upload support after server restart
ALTER TABLE sessions ADD COLUMN IF NOT EXISTS user_uid VARCHAR(255) NOT NULL DEFAULT '';

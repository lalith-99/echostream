-- Add password hash column to users table for auth.
-- NOT NULL with no default: signup must always provide a hashed password.
ALTER TABLE users ADD COLUMN password_hash text NOT NULL DEFAULT '';

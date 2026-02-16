-- Migration: Add FCM token column to users table
-- Run this SQL script to add the fcm_token column if it doesn't exist

ALTER TABLE users ADD COLUMN IF NOT EXISTS fcm_token TEXT;

-- Optional: Add index for faster lookups (if needed)
-- CREATE INDEX IF NOT EXISTS idx_users_fcm_token ON users(fcm_token) WHERE fcm_token IS NOT NULL;


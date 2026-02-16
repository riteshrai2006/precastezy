-- Add boolean column 'others' to precast table (default false for existing rows)
ALTER TABLE precast ADD COLUMN IF NOT EXISTS others boolean NOT NULL DEFAULT false;
//jkjkhh
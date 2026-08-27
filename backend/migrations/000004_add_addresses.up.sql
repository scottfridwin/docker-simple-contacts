-- Add addresses JSONB column to persons table
ALTER TABLE persons ADD COLUMN addresses JSONB NOT NULL DEFAULT '[]'::jsonb;

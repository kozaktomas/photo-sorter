-- Add suggestions column to text_check_results for readability/flow advisory items
ALTER TABLE text_check_results ADD COLUMN IF NOT EXISTS suggestions JSONB;

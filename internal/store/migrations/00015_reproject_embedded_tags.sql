-- +goose Up
-- The normalized tag projection now distinguishes embedded metadata from
-- filename/directory hints. Mark cached present files as drafts so the
-- existing durable scan recovery path re-reads their embedded tags once.
-- This migration never mutates audio files.
UPDATE library_files
SET sync_state = 'draft', parse_error = ''
WHERE present = 1;

-- +goose Down
-- Projection data cannot be reconstructed by SQL when rolling application
-- code back. Leave the durable draft markers in place so the older scanner can
-- safely rebuild them on its next pass as well.
SELECT 1;

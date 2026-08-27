-- +goose Up
-- Older workers classified every item-level failure as "partial", including
-- jobs where none of the items succeeded. Repair those durable snapshots so
-- the terminal state agrees with the persisted counters.
UPDATE jobs
SET state = 'failed',
    detail = '处理完成：成功 0 项，失败 ' || failed || ' 项'
WHERE state = 'partial'
  AND succeeded = 0
  AND failed > 0;

-- +goose Down
-- The previous state cannot be reconstructed safely after new workers have
-- written consistent terminal snapshots.
SELECT 1;

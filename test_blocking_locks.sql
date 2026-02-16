-- Test script to create blocking locks for testing the psq blocking locks widget
-- 
-- Usage:
--   1. Open 3 terminal windows with psql connected to your test database
--   2. In terminal 1, run the "Setup" section
--   3. In terminal 2, run the "Create blocking transaction" section
--   4. In terminal 3, run the "Create blocked transaction" section
--   5. Check psq home dashboard - should show blocking lock info
--   6. Clean up by running COMMIT or ROLLBACK in terminals 2 and 3

-- ===================
-- Setup (Terminal 1)
-- ===================
-- Create a test table if it doesn't exist
CREATE TABLE IF NOT EXISTS test_locks (
    id INT PRIMARY KEY,
    value TEXT
);

-- Insert a test row
INSERT INTO test_locks (id, value) VALUES (1, 'test') 
ON CONFLICT (id) DO UPDATE SET value = 'test';

-- ===================
-- Create blocking transaction (Terminal 2)
-- ===================
BEGIN;
-- This will acquire an exclusive lock on the row
UPDATE test_locks SET value = 'blocking' WHERE id = 1;
-- DON'T COMMIT YET - leave this transaction open

-- ===================
-- Create blocked transaction (Terminal 3)
-- ===================
BEGIN;
-- This will try to update the same row and will be blocked
UPDATE test_locks SET value = 'blocked' WHERE id = 1;
-- This will hang, waiting for terminal 2 to commit/rollback

-- ===================
-- Check blocking locks (Terminal 1 or psql)
-- ===================
-- See all blocked queries
SELECT 
    blocked.pid AS blocked_pid,
    blocked.usename AS blocked_user,
    blocking.pid AS blocking_pid,
    blocking.usename AS blocking_user,
    blocked.query AS blocked_query,
    blocking.query AS blocking_query
FROM pg_stat_activity blocked
JOIN pg_stat_activity blocking ON blocking.pid = ANY(pg_blocking_pids(blocked.pid))
WHERE cardinality(pg_blocking_pids(blocked.pid)) > 0;

-- ===================
-- Cleanup
-- ===================
-- In terminal 2:
COMMIT; -- or ROLLBACK

-- In terminal 3:
-- (Should automatically complete now)
COMMIT; -- or ROLLBACK

-- Drop test table if desired:
-- DROP TABLE IF EXISTS test_locks;

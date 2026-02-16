-- Test script to create blocking locks for testing the psq blocking locks widget
-- 
-- Usage (Simple):
--   1. Open 3 terminal windows with psql connected to your test database
--   2. In terminal 1, run the "Setup" section
--   3. In terminal 2, run the "Create blocking transaction" section
--   4. In terminal 3, run the "Create blocked transaction" section
--   5. Check psq home dashboard - should show blocking lock info
--   6. Clean up by running COMMIT or ROLLBACK in terminals 2 and 3
--
-- Usage (Chain test - to verify root blocker detection):
--   1. Open 4 terminals
--   2. Terminal 1: Setup
--   3. Terminal 2: Lock row 1
--   4. Terminal 3: Lock row 2 (wait on row 1 - will be blocked by terminal 2)
--   5. Terminal 4: Lock row 2 (wait on row 2 - will be blocked by terminal 3)
--   6. psq should show terminal 2's PID as blocking 2 queries (terminals 3 and 4)

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
-- Cleanup (Simple test)
-- ===================
-- In terminal 2:
COMMIT; -- or ROLLBACK

-- In terminal 3:
-- (Should automatically complete now)
COMMIT; -- or ROLLBACK

-- ===================
-- Chain Test: Create a blocking chain
-- ===================

-- Terminal 2: ROOT BLOCKER - lock row 1
BEGIN;
UPDATE test_locks SET value = 'root-blocker' WHERE id = 1;
-- Don't commit!

-- Terminal 3: INTERMEDIATE BLOCKER - try to lock row 1, then lock row 2
BEGIN;
UPDATE test_locks SET value = 'intermediate' WHERE id = 1; -- Will block on terminal 2
-- Once blocked, this transaction can't proceed to lock row 2
-- To create a proper chain, we need another approach...

-- ===================
-- Better Chain Test: Use two test rows
-- ===================

-- Terminal 2: ROOT BLOCKER - lock row 1
BEGIN;
UPDATE test_locks SET value = 'root' WHERE id = 1;
-- Don't commit! Keep this transaction open

-- Terminal 3: INTERMEDIATE - lock row 2, THEN try to lock row 1
BEGIN;
-- First insert/update row 2 to lock it
INSERT INTO test_locks (id, value) VALUES (2, 'intermediate')
ON CONFLICT (id) DO UPDATE SET value = 'intermediate';
-- Now try to lock row 1 (will block on terminal 2)
UPDATE test_locks SET value = 'intermediate-wants-row1' WHERE id = 1;
-- This will hang here

-- Terminal 4: LEAF - try to lock row 2 (will block on terminal 3)
BEGIN;
UPDATE test_locks SET value = 'leaf' WHERE id = 2;
-- This will hang waiting for terminal 3

-- Now check psq: should show terminal 2's PID as the root blocker
-- blocking 2 queries (terminals 3 and 4)

-- Cleanup chain test:
-- Terminal 2: COMMIT;
-- (Terminal 3 will complete, then terminal 4 will complete)

-- Drop test table if desired:
-- DROP TABLE IF EXISTS test_locks;

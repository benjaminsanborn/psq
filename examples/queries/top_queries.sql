-- Top Queries
-- Requires pg_stat_statements; identifies heavy hitters
SELECT LEFT(query, 40) AS query, calls, total_exec_time, mean_exec_time, rows, shared_blks_hit, shared_blks_read, temp_blks_written FROM pg_stat_statements ORDER BY total_exec_time DESC LIMIT 25;

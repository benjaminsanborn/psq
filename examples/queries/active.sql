-- Active Connections
SELECT pid,
    LEFT(query, 50) AS query,
    LEFT(usename, 8) AS name,
    LEFT(state, 10) AS state,
    LEFT((NOW() - query_start)::text, 8) as age,
    wait_event,
    wait_event_type
FROM pg_stat_activity
WHERE state IS NOT NULL
    AND state != 'idle'
ORDER BY NOW() - query_start DESC;
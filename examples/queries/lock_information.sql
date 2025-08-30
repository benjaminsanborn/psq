-- Lock Information
SELECT l.pid,
    l.mode,
    l.granted,
    a.usename,
    a.query
FROM pg_locks l
    JOIN pg_stat_activity a ON l.pid = a.pid
WHERE NOT l.granted
ORDER BY l.pid;
-- Table Replication State
SELECT s.subname AS subscription,
    r.srsubstate AS table_state,
    ARRAY_AGG(
        c.relname
        ORDER BY c.relname
    ) AS tables
FROM pg_class c
    JOIN pg_namespace n ON n.oid = c.relnamespace
    LEFT JOIN pg_subscription_rel r ON r.srrelid = c.oid
    LEFT JOIN pg_subscription s ON s.oid = r.srsubid
WHERE n.nspname = 'public'
    AND c.relkind IN ('r', 'p', 'f')
GROUP BY s.subname,
    r.srsubstate
ORDER BY s.subname,
    r.srsubstate;
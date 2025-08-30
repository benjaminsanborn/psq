-- Index Creation
SELECT p.pid,
    c.relname AS table_name,
    ic.relname AS index_name,
    p.phase,
    p.lockers_done || '/' || p.lockers_total AS locks,
    p.blocks_done || '/' || p.blocks_total AS blocks,
    p.tuples_done || '/' || p.tuples_total AS tupes,
    p.partitions_done || '/' || p.partitions_total AS parts
FROM pg_stat_progress_create_index p
    JOIN pg_class c ON p.relid = c.oid
    JOIN pg_class ic ON p.index_relid = ic.oid;
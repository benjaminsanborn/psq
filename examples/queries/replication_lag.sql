-- Replication Lag
SELECT application_name,
    pg_wal_lsn_diff(sent_lsn, replay_lsn) as lag_bytes,
    client_addr,
    state,
    sent_lsn,
    write_lsn,
    flush_lsn,
    replay_lsn
FROM pg_stat_replication;
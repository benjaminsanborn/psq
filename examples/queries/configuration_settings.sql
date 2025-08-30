-- Configuration Settings
-- All current PostgreSQL configuration settings
SELECT name, setting, unit, category, short_desc FROM pg_settings ORDER BY category, name;

-- Configuration Settings
SELECT name,
    setting,
    unit,
    category,
    short_desc
FROM pg_settings
ORDER BY category,
    name;
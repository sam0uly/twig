-- Recent events, newest first.
WITH recent AS (
    SELECT id, actor, action, created_at
    FROM events
    WHERE created_at >= CURRENT_DATE - INTERVAL '7 days'
      AND action <> 'ping'
    ORDER BY created_at DESC
    LIMIT 100
)
/* Join only the accounts that are still active. */
SELECT r.actor,
       COUNT(*) AS events,
       MAX(r.created_at) AS latest
FROM recent AS r
INNER JOIN accounts AS a ON a.id = r.actor
WHERE a.active = TRUE
GROUP BY r.actor
HAVING COUNT(*) > 10
ORDER BY events DESC;

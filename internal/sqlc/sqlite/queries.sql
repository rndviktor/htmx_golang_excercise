-- name: GetServerByID :one
SELECT * FROM server
WHERE id = ? AND user_id = ?
LIMIT 1;

-- name: ListServersByGroup :many
SELECT 
    s.id, 
    s.name, 
    s.host, 
    s.port, 
    s.maintenance_db, 
    s.username, 
    sg.name AS group_name
FROM server s
INNER JOIN servergroup sg ON s.servergroup_id = sg.id
WHERE s.user_id = ?
ORDER BY sg.name, s.name;

-- name: CreateServer :one
INSERT INTO server (
    user_id, servergroup_id, name, host, port, maintenance_db, username, ssl_mode
) VALUES (
    ?, ?, ?, ?, ?, ?, ?, ?
)
RETURNING *;
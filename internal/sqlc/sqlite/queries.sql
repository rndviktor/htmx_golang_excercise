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

-- name: GetUserWorkspace :one
SELECT * FROM user_workspaces
WHERE user_id = ?
LIMIT 1;

-- name: SaveUserWorkspace :exec
INSERT INTO user_workspaces (user_id, active_tab_id, layout_metadata, updated_at)
VALUES (?, ?, ?, CURRENT_TIMESTAMP)
ON CONFLICT(user_id) DO UPDATE SET
    active_tab_id = excluded.active_tab_id,
    layout_metadata = excluded.layout_metadata,
    updated_at = CURRENT_TIMESTAMP;

-- name: ListWorkspaceTabs :many
SELECT * FROM workspace_tabs
WHERE user_id = ?
ORDER BY tab_order, created_at;

-- name: ReplaceWorkspaceTabs :exec
DELETE FROM workspace_tabs
WHERE user_id = ?;

-- name: InsertWorkspaceTab :exec
INSERT INTO workspace_tabs (id, user_id, title, connection_id, query_text, tab_order, created_at)
VALUES (?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP);

-- name: ListQueryHistory :many
SELECT id, user_id, tab_id, connection_id, query_text, executed_at, duration_ms, status
FROM query_history
WHERE user_id = ?
  AND (? = '' OR tab_id = ?)
  AND (? = '' OR connection_id = ?)
ORDER BY executed_at DESC, id DESC
LIMIT 50;

-- name: RecordQueryHistory :exec
INSERT INTO query_history (user_id, tab_id, connection_id, query_text, executed_at, duration_ms, status)
VALUES (?, ?, ?, ?, CURRENT_TIMESTAMP, ?, ?);
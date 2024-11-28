-- name: GetAllWatches :many
SELECT *
FROM watches
order by id;

-- name: GetWatchByNameAndUrl :one
SELECT *
FROM watches
WHERE name = ?
  AND url = ?;

-- name: InsertWatch :one
INSERT INTO watches(name, url, last_fetch, last_content)
VALUES (?, ?, CURRENT_TIMESTAMP, ?)
RETURNING *;

-- name: UpdateWatch :one
UPDATE watches
SET last_fetch=CURRENT_TIMESTAMP,
    last_content=?
WHERE id = ?
RETURNING *;

-- name: DeleteWatch :exec
DELETE
FROM watches
WHERE id = ?;

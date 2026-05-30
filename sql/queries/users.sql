-- name: CreateUser :one
INSERT INTO users (id, created_at, updated_at, email, hashed_password, is_chirpy_red)
VALUES (gen_random_uuid(), NOW(), NOW(), $1, $2, $3)
RETURNING *;

-- name: DeleteUsers :exec
DELETE FROM users;

-- name: GetUserByEmail :one
SELECT * FROM users
WHERE email = $1;

-- name: UpdateUserEmailAndPassword :one
UPDATE users
SET updated_at = NOW(),
    email = COALESCE($2, email),
    hashed_password = COALESCE($3, hashed_password)
WHERE id = $1
RETURNING *;

-- name: GetUserByID :one
SELECT * FROM users
WHERE id = $1;

-- name: UpdateUserToChirpyRed :one
UPDATE users
SET updated_at = NOW(),
    is_chirpy_red = TRUE
WHERE id = $1
RETURNING *;
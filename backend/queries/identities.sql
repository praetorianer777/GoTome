-- name: CreateOidcLogin :exec
INSERT INTO oidc_logins (state_hash, nonce, verifier, return_to, link_user_id, expires_at)
VALUES ($1, $2, $3, $4, $5, $6);

-- name: TakeOidcLogin :one
-- A state is good once, and only until it runs out.
DELETE FROM oidc_logins
WHERE state_hash = sqlc.arg(state_hash) AND expires_at > sqlc.arg(now)::timestamptz
RETURNING *;

-- name: DeleteExpiredOidcLogins :exec
DELETE FROM oidc_logins WHERE expires_at <= sqlc.arg(now)::timestamptz;

-- name: GetIdentityUser :one
SELECT u.*, i.id AS identity_id
FROM user_identities i
JOIN users u ON u.id = i.user_id
WHERE i.issuer = $1 AND i.subject = $2;

-- name: GetUserByEmail :one
SELECT * FROM users WHERE email = $1;

-- name: UsernameTaken :one
SELECT EXISTS (SELECT 1 FROM users WHERE username = $1);

-- name: CreateIdentity :one
INSERT INTO user_identities (user_id, issuer, subject, email)
VALUES ($1, $2, $3, $4)
ON CONFLICT (issuer, subject) DO NOTHING
RETURNING id;

-- name: TouchIdentity :exec
UPDATE user_identities SET email = $2, last_used_at = now() WHERE id = $1;

-- name: ListUserIdentities :many
SELECT id, issuer, email, created_at, last_used_at
FROM user_identities
WHERE user_id = $1
ORDER BY created_at;

-- name: DeleteUserIdentity :execrows
DELETE FROM user_identities WHERE id = $1 AND user_id = $2;

-- name: ActiveAdminsWithIdentity :one
-- Administrators who can sign in through an identity provider. While there
-- is none, an administrator may sign in with a password even when
-- passwords are turned off, so that nobody is locked out.
SELECT count(*) FROM users u
WHERE u.role = 'admin' AND u.disabled_at IS NULL
  AND EXISTS (SELECT 1 FROM user_identities i WHERE i.user_id = u.id);

-- name: SetRole :one
UPDATE users SET role = $2, updated_at = now() WHERE id = $1 RETURNING *;

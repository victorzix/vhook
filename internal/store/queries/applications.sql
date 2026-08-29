-- name: LockBootstrap :exec
-- Serialises concurrent bootstrap runs. Held until the transaction ends, so
-- the second run blocks and then sees the organization the first one created.
-- Nothing in the schema serialises them otherwise: each run mints a distinct
-- api_key_hash, so the UNIQUE on that column never collides.
SELECT pg_advisory_xact_lock($1);

-- name: CountOrganizations :one
SELECT count(*) FROM organizations;

-- name: CreateOrganization :one
INSERT INTO organizations (id, name)
VALUES ($1, $2)
RETURNING *;

-- name: CreateApplication :one
INSERT INTO applications (id, organization_id, name, api_key_hash, locale, backoff_profile)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

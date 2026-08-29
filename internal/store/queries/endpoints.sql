-- name: LockApplication :one
-- Trava a linha da application para o resto da transação. Tomada ANTES da
-- contagem: sem ela, duas criações simultâneas leem o mesmo total e ambas
-- inserem. É por tenant, então dois clientes não se esperam.
SELECT id FROM applications WHERE id = $1 FOR UPDATE;

-- name: CountEndpoints :one
SELECT count(*) FROM endpoints WHERE application_id = $1;

-- name: CreateEndpoint :one
INSERT INTO endpoints (id, application_id, url, secret_encrypted)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: ListEndpoints :many
SELECT * FROM endpoints
WHERE application_id = $1
ORDER BY created_at, id;

-- name: GetEndpoint :one
-- O application_id no WHERE é o que faz recurso de outro tenant devolver 404
-- em vez de 403: existência e autorização ficam indistinguíveis de fora.
SELECT * FROM endpoints WHERE id = $1 AND application_id = $2;

-- name: UpdateEndpointURL :one
UPDATE endpoints
SET url = $3, updated_at = now()
WHERE id = $1 AND application_id = $2
RETURNING *;

CREATE TABLE organizations (
    id          uuid PRIMARY KEY,
    name        text        NOT NULL,
    created_at  timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE applications (
    id               uuid PRIMARY KEY,
    organization_id  uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    name             text NOT NULL,
    api_key_hash     text NOT NULL,
    plan             text NOT NULL DEFAULT 'free'
                     CHECK (plan IN ('free')),
    backoff_profile  text NOT NULL DEFAULT 'production'
                     CHECK (backoff_profile IN ('production', 'demo')),
    locale           text NOT NULL DEFAULT 'pt-BR'
                     CHECK (locale IN ('pt-BR', 'en', 'es', 'fr')),
    created_at       timestamptz NOT NULL DEFAULT now(),
    UNIQUE (api_key_hash)
);

CREATE TABLE endpoints (
    id                    uuid PRIMARY KEY,
    application_id        uuid NOT NULL REFERENCES applications(id) ON DELETE CASCADE,
    url                   text  NOT NULL,
    secret_encrypted      bytea NOT NULL,
    status                text  NOT NULL DEFAULT 'active'
                          CHECK (status IN ('active', 'disabled')),
    consecutive_failures  integer     NOT NULL DEFAULT 0,
    disabled_at           timestamptz,
    created_at            timestamptz NOT NULL DEFAULT now(),
    updated_at            timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX endpoints_application_id_idx ON endpoints (application_id);

-- payload é text e não jsonb de propósito: jsonb reordena chaves e
-- re-renderiza na leitura, o que quebraria o HMAC calculado sobre os bytes
-- crus. Ver ARCHITECTURE.md §4.32. NULL significa expurgado por retenção.
CREATE TABLE events (
    id               uuid PRIMARY KEY,
    application_id   uuid NOT NULL REFERENCES applications(id) ON DELETE CASCADE,
    event_type       text NOT NULL,
    payload          text,
    idempotency_key  text,
    received_at      timestamptz NOT NULL DEFAULT now(),
    UNIQUE (application_id, idempotency_key)
);

CREATE TABLE deliveries (
    id               uuid PRIMARY KEY,
    event_id         uuid NOT NULL REFERENCES events(id)    ON DELETE CASCADE,
    endpoint_id      uuid NOT NULL REFERENCES endpoints(id) ON DELETE CASCADE,
    status           text NOT NULL DEFAULT 'pending'
                     CHECK (status IN ('pending','delivering','succeeded','failed','dead')),
    attempt_count    integer NOT NULL DEFAULT 0,
    next_attempt_at  timestamptz,
    completed_at     timestamptz,
    created_at       timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX deliveries_keyset_idx     ON deliveries (created_at, id);
CREATE INDEX deliveries_reconciler_idx ON deliveries (status, next_attempt_at);

CREATE TABLE delivery_attempts (
    id                uuid PRIMARY KEY,
    delivery_id       uuid    NOT NULL REFERENCES deliveries(id) ON DELETE CASCADE,
    attempt_number    integer NOT NULL,
    status_code       integer,
    response_time_ms  integer,
    response_snippet  text,
    error             text,
    attempted_at      timestamptz NOT NULL DEFAULT now(),
    UNIQUE (delivery_id, attempt_number)
);

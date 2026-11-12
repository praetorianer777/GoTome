-- +goose Up
-- Addresses an administrator has GOtome tell of events (#418). The secret
-- is sealed like a setting's secret, with the hook's ID as associated data,
-- and never sent back. An empty kinds list is every kind.
CREATE TABLE webhooks (
    id            uuid PRIMARY KEY DEFAULT uuidv7(),
    name          text NOT NULL,
    url           text NOT NULL,
    kinds         text[] NOT NULL DEFAULT '{}',
    secret_sealed bytea NOT NULL,
    trusted       boolean NOT NULL DEFAULT false,
    enabled       boolean NOT NULL DEFAULT true,
    created_by    uuid REFERENCES users (id) ON DELETE SET NULL,
    created_at    timestamptz NOT NULL DEFAULT now(),
    updated_at    timestamptz NOT NULL DEFAULT now()
);

-- The log of what was sent to each hook, and the queue a delivery job reads
-- its payload from. Trimmed by webhooks.trim; a backup leaves the rows out.
CREATE TABLE webhook_deliveries (
    id            uuid PRIMARY KEY DEFAULT uuidv7(),
    hook_id       uuid NOT NULL REFERENCES webhooks (id) ON DELETE CASCADE,
    kind          text NOT NULL,
    payload       jsonb NOT NULL,
    status        text NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'delivered', 'failed')),
    attempts      integer NOT NULL DEFAULT 0,
    last_error    text,
    response_code integer,
    created_at    timestamptz NOT NULL DEFAULT now(),
    updated_at    timestamptz NOT NULL DEFAULT now(),
    delivered_at  timestamptz
);

CREATE INDEX webhook_deliveries_hook_idx ON webhook_deliveries (hook_id, created_at DESC);
CREATE INDEX webhook_deliveries_created_idx ON webhook_deliveries (created_at);

-- +goose Down
DROP TABLE webhook_deliveries;
DROP TABLE webhooks;

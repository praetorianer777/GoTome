-- +goose Up
-- Sign-in through an OpenID Connect identity provider (#72).

-- An account at an identity provider, by the provider's issuer and its
-- subject for the person, which never changes; the e-mail address it gave
-- last is only shown.
CREATE TABLE user_identities (
    id           uuid PRIMARY KEY DEFAULT uuidv7(),
    user_id      uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    issuer       text NOT NULL,
    subject      text NOT NULL,
    email        text,
    created_at   timestamptz NOT NULL DEFAULT now(),
    last_used_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (issuer, subject)
);
CREATE INDEX user_identities_user ON user_identities (user_id);

-- A sign-in on its way through the identity provider, found again by the
-- hash of its state when the browser comes back. A row is taken once.
CREATE TABLE oidc_logins (
    state_hash   bytea PRIMARY KEY,
    nonce        text NOT NULL,
    verifier     text NOT NULL,
    return_to    text NOT NULL,
    -- Set when a signed-in person links an identity to their account.
    link_user_id uuid REFERENCES users (id) ON DELETE CASCADE,
    expires_at   timestamptz NOT NULL
);

-- +goose Down
DROP TABLE oidc_logins;
DROP TABLE user_identities;

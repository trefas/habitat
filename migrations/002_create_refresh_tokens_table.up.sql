CREATE TABLE refresh_tokens (
    id          uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id     uuid        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token_hash  bytea       NOT NULL,
    family_id   uuid        NOT NULL DEFAULT gen_random_uuid(),
    replaced_by uuid        REFERENCES refresh_tokens(id) ON DELETE SET NULL,
    expires_at  timestamptz NOT NULL,
    revoked_at  timestamptz,
    created_at  timestamptz NOT NULL DEFAULT now(),
    user_agent  text        NOT NULL DEFAULT '',
    ip          inet
);

CREATE UNIQUE INDEX refresh_tokens_hash_uidx    ON refresh_tokens (token_hash);
CREATE INDEX        refresh_tokens_user_act_idx ON refresh_tokens (user_id) WHERE revoked_at IS NULL;
CREATE INDEX        refresh_tokens_family_idx   ON refresh_tokens (family_id);
CREATE INDEX        refresh_tokens_expires_idx  ON refresh_tokens (expires_at);
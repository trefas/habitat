CREATE TABLE users (
    id            uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    email         text        NOT NULL,
    password_hash text        NOT NULL,
    display_name  text        NOT NULL DEFAULT '',
    role          text        NOT NULL DEFAULT 'user',
    status        text        NOT NULL DEFAULT 'active',
    created_at    timestamptz NOT NULL DEFAULT now(),
    updated_at    timestamptz NOT NULL DEFAULT now(),
    last_login_at timestamptz,

    CONSTRAINT users_role_check   CHECK (role IN ('user', 'admin')),
    CONSTRAINT users_status_check CHECK (status IN ('pending', 'active', 'blocked')),
    CONSTRAINT users_email_len    CHECK (char_length(email) BETWEEN 3 AND 254),
    CONSTRAINT users_email_lower  CHECK (email = lower(email))
);

CREATE UNIQUE INDEX users_email_uidx ON users (lower(email));

CREATE OR REPLACE FUNCTION set_updated_at() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    NEW.updated_at := now();
    RETURN NEW;
END $$;

CREATE TRIGGER users_set_updated_at
    BEFORE UPDATE ON users
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();
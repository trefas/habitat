-- 1. Справочник типов сущностей.
-- Пока реализован только тип message; остальные типы добавляются
-- отдельными миграциями вместе со своими телами.
CREATE TABLE entity_types (
    code  varchar(32) PRIMARY KEY,
    title varchar(128) NOT NULL
);

INSERT INTO entity_types (code, title) VALUES
    ('message', 'Сообщение');

-- 2. Реестр сущностей: единая точка входа для прав, шеринга и уведомлений.
CREATE TABLE entities (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    type_code   varchar(32) NOT NULL REFERENCES entity_types(code),
    owner_id    uuid NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    title       varchar(256) NOT NULL DEFAULT '',
    is_archived boolean NOT NULL DEFAULT false,
    version     bigint NOT NULL DEFAULT 1,
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now(),

    -- Нужно для составного FK из типизированных таблиц тел.
    UNIQUE (id, type_code)
);

CREATE INDEX entities_owner_idx ON entities(owner_id);
CREATE INDEX entities_type_owner_idx
    ON entities(type_code, owner_id)
    WHERE is_archived = FALSE;

CREATE TRIGGER entities_set_updated_at
    BEFORE UPDATE ON entities
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- 3. Тело сообщения.
-- type_code — технический дискриминатор, гарантирующий, что в messages
-- могут лежать только сущности типа message.
CREATE TABLE messages (
    entity_id uuid PRIMARY KEY,
    type_code varchar(32) NOT NULL DEFAULT 'message'
              CHECK (type_code = 'message'),
    text      text NOT NULL CHECK (char_length(text) BETWEEN 1 AND 20000),

    CONSTRAINT messages_entity_fk
        FOREIGN KEY (entity_id, type_code)
        REFERENCES entities(id, type_code) ON DELETE CASCADE
);

-- 4. Передача доступа.
-- Пока приложение создаёт только mode='direct', permission='read',
-- но модель готова к copy/write для будущих типов сущностей.
CREATE TABLE entity_shares (
    id               uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    entity_id        uuid NOT NULL REFERENCES entities(id) ON DELETE CASCADE,
    grantor_id       uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    grantee_id       uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    mode             text NOT NULL CHECK (mode IN ('direct', 'copy')),
    permission       text NOT NULL CHECK (permission IN ('read', 'write')),
    granted_at       timestamptz NOT NULL DEFAULT now(),
    expires_at       timestamptz,
    revoked_at       timestamptz,
    copied_entity_id uuid UNIQUE REFERENCES entities(id) ON DELETE CASCADE,

    CONSTRAINT entity_shares_no_self CHECK (grantor_id <> grantee_id),
    CONSTRAINT entity_shares_dates CHECK (expires_at IS NULL OR expires_at > granted_at),
    CONSTRAINT entity_shares_copy CHECK (
        mode <> 'copy' OR (
            permission = 'write'
            AND expires_at IS NULL
            AND copied_entity_id IS NOT NULL
        )
    ),
    CONSTRAINT entity_shares_direct CHECK (
        mode <> 'direct' OR copied_entity_id IS NULL
    )
);

CREATE UNIQUE INDEX entity_shares_active_direct_uniq
    ON entity_shares(entity_id, grantee_id)
    WHERE mode = 'direct' AND revoked_at IS NULL;

CREATE INDEX entity_shares_grantee_idx
    ON entity_shares(grantee_id, entity_id)
    WHERE mode = 'direct' AND revoked_at IS NULL;

CREATE INDEX entity_shares_entity_idx
    ON entity_shares(entity_id)
    WHERE revoked_at IS NULL;

CREATE INDEX entity_shares_expires_idx
    ON entity_shares(expires_at)
    WHERE mode = 'direct' AND revoked_at IS NULL AND expires_at IS NOT NULL;

-- 5. Факт прочтения сообщения конкретным получателем.
CREATE TABLE message_reads (
    entity_id uuid NOT NULL REFERENCES messages(entity_id) ON DELETE CASCADE,
    user_id   uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    read_at   timestamptz NOT NULL DEFAULT now(),

    PRIMARY KEY (entity_id, user_id)
);

-- 6. Входящие уведомления.
CREATE TABLE notifications (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id    uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    kind       text NOT NULL CHECK (kind IN (
                   'share_received', 'share_revoked', 'share_expiring',
                   'entity_copied', 'entity_updated')),
    entity_id  uuid REFERENCES entities(id) ON DELETE SET NULL,
    share_id   uuid REFERENCES entity_shares(id) ON DELETE SET NULL,
    actor_id   uuid REFERENCES users(id) ON DELETE SET NULL,
    payload    jsonb NOT NULL DEFAULT '{}'
               CHECK (jsonb_typeof(payload) = 'object'),
    read_at    timestamptz,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX notifications_user_created_idx
    ON notifications(user_id, created_at DESC);

CREATE INDEX notifications_user_unread_idx
    ON notifications(user_id, created_at DESC)
    WHERE read_at IS NULL;

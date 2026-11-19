package postgres

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"habitat/internal/domain"
)

func recipient(u *domain.User) domain.MessageRecipient {
	return domain.MessageRecipient{UserID: u.ID, Email: u.Email, DisplayName: u.DisplayName}
}

func TestMessageFlow(t *testing.T) {
	store := setupTestDB(t)
	ctx := t.Context()

	owner := createTestUser(t, store, "ivan@test.local", "Иван")
	alice := createTestUser(t, store, "alice@test.local", "Алиса")
	bob := createTestUser(t, store, "bob@test.local", "Боб")
	carol := createTestUser(t, store, "carol@test.local", "Карол")

	msg, err := store.CreateMessage(ctx, owner.ID, "Привет, коллеги!",
		[]domain.MessageRecipient{recipient(alice), recipient(bob)})
	require.NoError(t, err)
	require.NotEmpty(t, msg.ID)
	require.True(t, msg.IsOwner)
	require.Equal(t, 2, msg.RecipientsCount)
	require.Equal(t, "Привет, коллеги!", msg.Text)

	// Отправитель видит сообщение в sent.
	sent, total, err := store.ListMessages(ctx, owner.ID, "sent", 10, 0)
	require.NoError(t, err)
	require.Equal(t, 1, total)
	require.Len(t, sent, 1)
	require.True(t, sent[0].IsOwner)
	require.Equal(t, 0, sent[0].ReadCount)

	// Получатель видит сообщение во входящих.
	inbox, total, err := store.ListMessages(ctx, alice.ID, "inbox", 10, 0)
	require.NoError(t, err)
	require.Equal(t, 1, total)
	require.Len(t, inbox, 1)
	require.False(t, inbox[0].IsOwner)
	require.Nil(t, inbox[0].ReadAt)

	// Факт прочтения.
	require.NoError(t, store.MarkMessageRead(ctx, alice.ID, msg.ID))
	require.NoError(t, store.MarkMessageRead(ctx, alice.ID, msg.ID)) // идемпотентно

	view, err := store.GetMessage(ctx, alice.ID, msg.ID)
	require.NoError(t, err)
	require.NotNil(t, view.ReadAt)

	receipts, err := store.ListMessageRecipients(ctx, owner.ID, msg.ID)
	require.NoError(t, err)
	require.Len(t, receipts, 2)
	readByAlice := false
	for _, r := range receipts {
		switch r.UserID {
		case alice.ID:
			require.NotNil(t, r.ReadAt)
			readByAlice = true
		case bob.ID:
			require.Nil(t, r.ReadAt)
		}
	}
	require.True(t, readByAlice)

	// Уведомления получателей.
	notifs, total, err := store.ListNotifications(ctx, alice.ID, true, 10, 0)
	require.NoError(t, err)
	require.Equal(t, 1, total)
	require.Len(t, notifs, 1)
	require.Equal(t, domain.NotificationShareReceived, notifs[0].Kind)
	require.NotNil(t, notifs[0].EntityID)
	require.Equal(t, msg.ID, *notifs[0].EntityID)

	require.NoError(t, store.MarkNotificationRead(ctx, alice.ID, notifs[0].ID))
	_, total, err = store.ListNotifications(ctx, alice.ID, true, 10, 0)
	require.NoError(t, err)
	require.Equal(t, 0, total)

	// Повторное добавление существующего получателя не создаёт дубль.
	require.NoError(t, store.AddMessageRecipients(ctx, owner.ID, msg.ID, []domain.MessageRecipient{recipient(bob)}))
	receipts, err = store.ListMessageRecipients(ctx, owner.ID, msg.ID)
	require.NoError(t, err)
	require.Len(t, receipts, 2)

	// Новый получатель.
	require.NoError(t, store.AddMessageRecipients(ctx, owner.ID, msg.ID, []domain.MessageRecipient{recipient(carol)}))
	receipts, err = store.ListMessageRecipients(ctx, owner.ID, msg.ID)
	require.NoError(t, err)
	require.Len(t, receipts, 3)
	_, total, err = store.ListNotifications(ctx, carol.ID, true, 10, 0)
	require.NoError(t, err)
	require.Equal(t, 1, total)

	// Отзыв доступа.
	require.NoError(t, store.RevokeMessageShare(ctx, owner.ID, msg.ID, bob.ID))
	_, err = store.GetMessage(ctx, bob.ID, msg.ID)
	require.ErrorIs(t, err, domain.ErrNotFound)

	receipts, err = store.ListMessageRecipients(ctx, owner.ID, msg.ID)
	require.NoError(t, err)
	require.Len(t, receipts, 2)

	_, total, err = store.ListNotifications(ctx, bob.ID, true, 10, 0)
	require.NoError(t, err)
	require.Equal(t, 2, total, "share_received + share_revoked")

	// Чужой пользователь не видит сообщение.
	_, err = store.GetMessage(ctx, carol.ID, msg.ID)
	require.NoError(t, err) // Карол теперь получатель
	dave := createTestUser(t, store, "dave@test.local", "Дейв")
	_, err = store.GetMessage(ctx, dave.ID, msg.ID)
	require.ErrorIs(t, err, domain.ErrNotFound)
}

func TestMessageRecipientsOnlyForOwner(t *testing.T) {
	store := setupTestDB(t)
	ctx := t.Context()

	owner := createTestUser(t, store, "owner2@test.local", "Owner")
	recipientUser := createTestUser(t, store, "recipient2@test.local", "Recipient")

	msg, err := store.CreateMessage(ctx, owner.ID, "secret", []domain.MessageRecipient{recipient(recipientUser)})
	require.NoError(t, err)

	list, err := store.ListMessageRecipients(ctx, recipientUser.ID, msg.ID)
	require.NoError(t, err)
	require.Empty(t, list)

	view, err := store.GetMessage(ctx, recipientUser.ID, msg.ID)
	require.NoError(t, err)
	require.False(t, view.IsOwner)
}

func TestMessageBodyTypeConstraint(t *testing.T) {
	store := setupTestDB(t)
	ctx := t.Context()

	owner := createTestUser(t, store, "typecheck@test.local", "Owner")
	_, err := store.pool.Exec(ctx,
		`INSERT INTO entity_types (code, title) VALUES ('note', 'Заметка') ON CONFLICT (code) DO NOTHING`)
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = store.pool.Exec(context.Background(), `DELETE FROM entity_types WHERE code = 'note'`)
	})

	var entityID string
	err = store.pool.QueryRow(ctx, `
		INSERT INTO entities (type_code, owner_id) VALUES ('note', $1::uuid)
		RETURNING id::text`, owner.ID).Scan(&entityID)
	require.NoError(t, err)

	_, err = store.pool.Exec(ctx, `INSERT INTO messages (entity_id, text) VALUES ($1::uuid, 'oops')`, entityID)
	require.Error(t, err, "messages должен принимать только entities.type_code = 'message'")
}

func TestSoftDeleteUser(t *testing.T) {
	store := setupTestDB(t)
	ctx := t.Context()

	u := createTestUser(t, store, "soft@test.local", "Soft User")
	require.NoError(t, store.CreateRefreshToken(ctx, u.ID,
		[]byte("hash"), time.Now().Add(time.Hour), domain.SessionMeta{}))

	require.NoError(t, store.DeleteUser(ctx, u.ID))

	_, err := store.UserByID(ctx, u.ID)
	require.ErrorIs(t, err, domain.ErrNotFound)

	_, err = store.UserByEmail(ctx, "soft@test.local")
	require.ErrorIs(t, err, domain.ErrNotFound)

	adminView, err := store.AdminUserByID(ctx, u.ID)
	require.NoError(t, err)
	require.NotNil(t, adminView.DeletedAt)
	require.Equal(t, domain.StatusBlocked, adminView.Status)

	var active int
	err = store.pool.QueryRow(ctx,
		`SELECT count(*)::int FROM refresh_tokens WHERE user_id = $1::uuid AND revoked_at IS NULL`,
		u.ID).Scan(&active)
	require.NoError(t, err)
	require.Zero(t, active)

	// Повторное удаление — уже нечего удалять.
	require.ErrorIs(t, store.DeleteUser(ctx, u.ID), domain.ErrNotFound)
}

func TestSoftDeleteKeepsOwnedMessages(t *testing.T) {
	store := setupTestDB(t)
	ctx := t.Context()

	owner := createTestUser(t, store, fmt.Sprintf("owner-%d@test.local", time.Now().UnixNano()), "Owner")
	recipientUser := createTestUser(t, store, fmt.Sprintf("rcpt-%d@test.local", time.Now().UnixNano()), "Recipient")

	msg, err := store.CreateMessage(ctx, owner.ID, "before delete", []domain.MessageRecipient{recipient(recipientUser)})
	require.NoError(t, err)

	require.NoError(t, store.DeleteUser(ctx, owner.ID))

	view, err := store.GetMessage(ctx, recipientUser.ID, msg.ID)
	require.NoError(t, err)
	require.Equal(t, "before delete", view.Text)
}

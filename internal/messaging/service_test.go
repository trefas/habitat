package messaging

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"habitat/internal/domain"
)

const (
	ownerID   = "11111111-1111-1111-1111-111111111111"
	aliceID   = "22222222-2222-2222-2222-222222222222"
	bobID     = "33333333-3333-3333-3333-333333333333"
	blockedID = "44444444-4444-4444-4444-444444444444"
	messageID = "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"
)

type fakeStorage struct {
	users      map[string]domain.User
	messages   map[string]domain.Message
	recipients map[string][]domain.MessageRecipient
	reads      map[string]map[string]time.Time
}

func newFakeStorage() *fakeStorage {
	return &fakeStorage{
		users: map[string]domain.User{
			ownerID:   {ID: ownerID, Email: "owner@example.com", DisplayName: "Owner", Role: domain.RoleUser, Status: domain.StatusActive},
			aliceID:   {ID: aliceID, Email: "alice@example.com", DisplayName: "Alice", Role: domain.RoleUser, Status: domain.StatusActive},
			bobID:     {ID: bobID, Email: "bob@example.com", DisplayName: "Bob", Role: domain.RoleUser, Status: domain.StatusActive},
			blockedID: {ID: blockedID, Email: "blocked@example.com", DisplayName: "Blocked", Role: domain.RoleUser, Status: domain.StatusBlocked},
		},
		messages:   map[string]domain.Message{},
		recipients: map[string][]domain.MessageRecipient{},
		reads:      map[string]map[string]time.Time{},
	}
}

func (f *fakeStorage) UserByID(_ context.Context, id string) (*domain.User, error) {
	u, ok := f.users[id]
	if !ok || u.DeletedAt != nil {
		return nil, domain.ErrNotFound
	}
	return &u, nil
}

func (f *fakeStorage) UserByEmail(_ context.Context, email string) (*domain.User, error) {
	for _, u := range f.users {
		if u.Email == email && u.DeletedAt == nil {
			return &u, nil
		}
	}
	return nil, domain.ErrNotFound
}

func (f *fakeStorage) CreateMessage(_ context.Context, ownerID, text string, recipients []domain.MessageRecipient) (*domain.Message, error) {
	now := time.Now()
	m := domain.Message{
		ID:              messageID,
		OwnerID:         ownerID,
		Text:            text,
		IsOwner:         true,
		RecipientsCount: len(recipients),
		CreatedAt:       now,
		UpdatedAt:       now,
	}
	f.messages[messageID] = m
	f.recipients[messageID] = append([]domain.MessageRecipient(nil), recipients...)
	return &m, nil
}

func (f *fakeStorage) ListMessages(_ context.Context, userID, _ string, _, _ int) ([]domain.Message, int, error) {
	list := make([]domain.Message, 0, len(f.messages))
	for _, m := range f.messages {
		m.IsOwner = m.OwnerID == userID
		if !m.IsOwner && !f.hasAccess(m.ID, userID) {
			continue
		}
		list = append(list, m)
	}
	return list, len(list), nil
}

func (f *fakeStorage) GetMessage(_ context.Context, userID, id string) (*domain.Message, error) {
	m, ok := f.messages[id]
	if !ok {
		return nil, domain.ErrNotFound
	}
	m.IsOwner = m.OwnerID == userID
	if !m.IsOwner && !f.hasAccess(id, userID) {
		return nil, domain.ErrNotFound
	}
	if t, ok := f.reads[id][userID]; ok {
		m.ReadAt = &t
	}
	return &m, nil
}

func (f *fakeStorage) ListMessageRecipients(_ context.Context, ownerID, id string) ([]domain.MessageRecipient, error) {
	m, ok := f.messages[id]
	if !ok || m.OwnerID != ownerID {
		return nil, domain.ErrNotFound
	}
	out := append([]domain.MessageRecipient(nil), f.recipients[id]...)
	for i := range out {
		if t, ok := f.reads[id][out[i].UserID]; ok {
			out[i].ReadAt = &t
		}
	}
	return out, nil
}

func (f *fakeStorage) MarkMessageRead(_ context.Context, userID, id string) error {
	if !f.hasAccess(id, userID) {
		return domain.ErrNotFound
	}
	if f.reads[id] == nil {
		f.reads[id] = map[string]time.Time{}
	}
	if _, ok := f.reads[id][userID]; !ok {
		f.reads[id][userID] = time.Now()
	}
	return nil
}

func (f *fakeStorage) AddMessageRecipients(_ context.Context, _, id string, recipients []domain.MessageRecipient) error {
	f.recipients[id] = append(f.recipients[id], recipients...)
	m := f.messages[id]
	m.RecipientsCount += len(recipients)
	f.messages[id] = m
	return nil
}

func (f *fakeStorage) RevokeMessageShare(_ context.Context, _, id, granteeID string) error {
	list := f.recipients[id]
	for i, r := range list {
		if r.UserID == granteeID {
			f.recipients[id] = append(list[:i], list[i+1:]...)
			return nil
		}
	}
	return domain.ErrNotFound
}

func (f *fakeStorage) hasAccess(messageID, userID string) bool {
	for _, r := range f.recipients[messageID] {
		if r.UserID == userID {
			return true
		}
	}
	return false
}

func TestSend_ValidatesInput(t *testing.T) {
	svc := NewService(newFakeStorage())
	ctx := t.Context()

	_, err := svc.Send(ctx, ownerID, "   ", []string{"alice@example.com"})
	require.ErrorIs(t, err, domain.ErrValidation)

	_, err = svc.Send(ctx, ownerID, "hello", nil)
	require.ErrorIs(t, err, domain.ErrValidation)

	_, err = svc.Send(ctx, ownerID, "hello", []string{"owner@example.com"})
	require.ErrorIs(t, err, domain.ErrValidation)

	_, err = svc.Send(ctx, ownerID, "hello", []string{"nobody@example.com"})
	require.ErrorIs(t, err, domain.ErrValidation)

	_, err = svc.Send(ctx, ownerID, "hello", []string{"blocked@example.com"})
	require.ErrorIs(t, err, domain.ErrValidation)
}

func TestSend_DeduplicatesRecipients(t *testing.T) {
	store := newFakeStorage()
	svc := NewService(store)

	view, err := svc.Send(t.Context(), ownerID, "hello",
		[]string{"alice@example.com", "ALICE@example.com", aliceID, bobID})
	require.NoError(t, err)
	require.Len(t, view.Recipients, 2)
	require.Equal(t, 2, view.Message.RecipientsCount)
	require.True(t, view.Message.IsOwner)
}

func TestList_ValidatesBoxAndPagination(t *testing.T) {
	svc := NewService(newFakeStorage())

	_, _, err := svc.List(t.Context(), aliceID, "archive", 0, 0)
	require.ErrorIs(t, err, domain.ErrValidation)

	_, _, err = svc.List(t.Context(), aliceID, BoxInbox, MaxLimit+1, 0)
	require.ErrorIs(t, err, domain.ErrValidation)

	_, _, err = svc.List(t.Context(), aliceID, BoxInbox, 10, -1)
	require.ErrorIs(t, err, domain.ErrValidation)
}

func TestMarkRead_RecipientCanReadAndOwnerCannot(t *testing.T) {
	store := newFakeStorage()
	svc := NewService(store)
	ctx := t.Context()

	_, err := svc.Send(ctx, ownerID, "hello", []string{aliceID})
	require.NoError(t, err)

	require.NoError(t, svc.MarkRead(ctx, aliceID, messageID))

	view, err := svc.Get(ctx, ownerID, messageID)
	require.NoError(t, err)
	require.Len(t, view.Recipients, 1)
	require.NotNil(t, view.Recipients[0].ReadAt)

	err = svc.MarkRead(ctx, ownerID, messageID)
	require.ErrorIs(t, err, domain.ErrValidation)
}

func TestShare_OnlyOwnerAndAddsRecipients(t *testing.T) {
	store := newFakeStorage()
	svc := NewService(store)
	ctx := t.Context()

	_, err := svc.Send(ctx, ownerID, "hello", []string{aliceID})
	require.NoError(t, err)

	_, err = svc.Share(ctx, aliceID, messageID, []string{bobID})
	require.ErrorIs(t, err, domain.ErrForbidden)

	recipients, err := svc.Share(ctx, ownerID, messageID, []string{bobID, bobID})
	require.NoError(t, err)
	require.Len(t, recipients, 2)
}

func TestRevoke_OnlyOwner(t *testing.T) {
	store := newFakeStorage()
	svc := NewService(store)
	ctx := t.Context()

	_, err := svc.Send(ctx, ownerID, "hello", []string{aliceID})
	require.NoError(t, err)

	require.ErrorIs(t, svc.Revoke(ctx, aliceID, messageID, aliceID), domain.ErrForbidden)
	require.NoError(t, svc.Revoke(ctx, ownerID, messageID, aliceID))

	_, err = svc.Get(ctx, aliceID, messageID)
	require.ErrorIs(t, err, domain.ErrNotFound)
}

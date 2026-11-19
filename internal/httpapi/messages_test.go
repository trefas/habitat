package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"habitat/internal/domain"
)

type fakeMessageService struct {
	text        string
	recipients  []string
	revokedUser string
	isOwner     bool
}

func (f *fakeMessageService) Send(_ context.Context, ownerID, text string, recipientRefs []string) (*domain.MessageView, error) {
	f.text, f.recipients = text, recipientRefs
	return &domain.MessageView{
		Message: domain.Message{ID: testUserID, OwnerID: ownerID, Text: text, IsOwner: true,
			RecipientsCount: len(recipientRefs), CreatedAt: time.Now(), UpdatedAt: time.Now()},
		Recipients: []domain.MessageRecipient{{UserID: testUserID, Email: "user@example.com"}},
	}, nil
}

func (f *fakeMessageService) List(_ context.Context, _, _ string, _, _ int) ([]domain.Message, int, error) {
	return []domain.Message{{ID: testUserID, Text: "hi", IsOwner: true}}, 1, nil
}

func (f *fakeMessageService) Get(_ context.Context, _, id string) (*domain.MessageView, error) {
	return &domain.MessageView{Message: domain.Message{ID: id, IsOwner: f.isOwner},
		Recipients: []domain.MessageRecipient{{UserID: testUserID, Email: "user@example.com"}}}, nil
}

func (f *fakeMessageService) MarkRead(_ context.Context, _, _ string) error { return nil }

func (f *fakeMessageService) Share(_ context.Context, _, _ string, refs []string) ([]domain.MessageRecipient, error) {
	f.recipients = refs
	return []domain.MessageRecipient{{UserID: testUserID, Email: "user@example.com"}}, nil
}

func (f *fakeMessageService) Revoke(_ context.Context, _, _, granteeID string) error {
	f.revokedUser = granteeID
	return nil
}

func TestCreateMessageHandler(t *testing.T) {
	svc := &fakeMessageService{}
	rec := httptest.NewRecorder()
	req := withClaims(httptest.NewRequest(http.MethodPost, "/api/v1/messages",
		strings.NewReader(`{"text":"Привет","recipients":["alice@example.com"]}`)))

	CreateMessageHandler(svc)(rec, req)

	require.Equal(t, http.StatusCreated, rec.Code)
	require.Equal(t, "Привет", svc.text)
	require.Equal(t, []string{"alice@example.com"}, svc.recipients)
}

func TestListMessagesHandler_InvalidLimit(t *testing.T) {
	svc := &fakeMessageService{}
	rec := httptest.NewRecorder()
	req := withClaims(httptest.NewRequest(http.MethodGet, "/api/v1/messages?limit=abc", nil))

	ListMessagesHandler(svc)(rec, req)

	require.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestListMessageReceiptsHandler_ForbiddenForRecipient(t *testing.T) {
	svc := &fakeMessageService{isOwner: false}
	rec := httptest.NewRecorder()
	req := withClaims(httptest.NewRequest(http.MethodGet, "/api/v1/messages/"+testUserID+"/receipts", nil))
	req.SetPathValue("id", testUserID)

	ListMessageReceiptsHandler(svc)(rec, req)

	require.Equal(t, http.StatusForbidden, rec.Code)
}

func TestRevokeMessageShareHandler(t *testing.T) {
	svc := &fakeMessageService{}
	rec := httptest.NewRecorder()
	req := withClaims(httptest.NewRequest(http.MethodDelete,
		"/api/v1/messages/"+testUserID+"/shares/"+testUserID, nil))
	req.SetPathValue("id", testUserID)
	req.SetPathValue("granteeID", testUserID)

	RevokeMessageShareHandler(svc)(rec, req)

	require.Equal(t, http.StatusNoContent, rec.Code)
	require.Equal(t, testUserID, svc.revokedUser)
}

type fakeNotificationService struct {
	unread bool
}

func (f *fakeNotificationService) List(_ context.Context, userID string, unreadOnly bool, _, _ int) ([]domain.Notification, int, error) {
	f.unread = unreadOnly
	entityID := testUserID
	return []domain.Notification{{
		ID: testUserID, UserID: userID, Kind: domain.NotificationShareReceived,
		EntityID: &entityID, Payload: []byte(`{"entity_type":"message"}`), CreatedAt: time.Now(),
	}}, 1, nil
}

func (f *fakeNotificationService) MarkRead(_ context.Context, _, _ string) error { return nil }

func TestListNotificationsHandler_ParsesUnread(t *testing.T) {
	svc := &fakeNotificationService{}
	rec := httptest.NewRecorder()
	req := withClaims(httptest.NewRequest(http.MethodGet, "/api/v1/notifications?unread=true", nil))

	ListNotificationsHandler(svc)(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.True(t, svc.unread)
	require.Contains(t, rec.Body.String(), "share_received")
}

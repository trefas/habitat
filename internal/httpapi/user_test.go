package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"habitat/internal/domain"
)

const testAdminID = "11111111-1111-1111-1111-111111111111"

type fakeProfileService struct {
	userID      string
	displayName *string
	called      bool
}

func (f *fakeProfileService) UpdateProfile(_ context.Context, userID string, displayName *string) (*domain.User, error) {
	f.called = true
	f.userID, f.displayName = userID, displayName
	return &domain.User{
		ID:          userID,
		Email:       "user@example.com",
		DisplayName: *displayName,
		Role:        domain.RoleUser,
		Status:      domain.StatusActive,
	}, nil
}

func TestUpdateMeHandler_PassesDisplayName(t *testing.T) {
	svc := &fakeProfileService{}
	rec := httptest.NewRecorder()
	req := withClaims(httptest.NewRequest(http.MethodPatch, "/api/v1/me",
		strings.NewReader(`{"display_name":"  New Name  "}`)))

	UpdateMeHandler(svc)(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.True(t, svc.called)
	require.NotNil(t, svc.displayName)
	require.Equal(t, "  New Name  ", *svc.displayName, "trim выполняет сервисный слой")
	require.Equal(t, testAdminID, svc.userID)

	var body userDTO
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.Equal(t, "user@example.com", body.Email)
}

func TestUpdateMeHandler_RejectsUnknownField(t *testing.T) {
	svc := &fakeProfileService{}
	rec := httptest.NewRecorder()
	req := withClaims(httptest.NewRequest(http.MethodPatch, "/api/v1/me",
		strings.NewReader(`{"email":"new@example.com"}`)))

	UpdateMeHandler(svc)(rec, req)

	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.False(t, svc.called)
}

func TestUpdateMeHandler_RequiresClaims(t *testing.T) {
	svc := &fakeProfileService{}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPatch, "/api/v1/me",
		strings.NewReader(`{"display_name":"New Name"}`))

	UpdateMeHandler(svc)(rec, req)

	require.Equal(t, http.StatusUnauthorized, rec.Code)
	require.False(t, svc.called)
}

package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/require"

	"habitat/internal/auth"
	"habitat/internal/domain"
)

const testUserID = "22222222-2222-2222-2222-222222222222"

type fakeAdminService struct {
	filter       domain.UserFilter
	patch        domain.UserPatch
	updateActor  string
	updateTarget string
	deleteActor  string
	deleteTarget string
	called       bool
}

func (f *fakeAdminService) ListUsers(_ context.Context, filter domain.UserFilter) ([]domain.User, int, error) {
	f.called = true
	f.filter = filter
	return []domain.User{{ID: testUserID, Email: "user@example.com", DisplayName: "User",
		Role: domain.RoleUser, Status: domain.StatusActive, CreatedAt: time.Now(), UpdatedAt: time.Now()}}, 1, nil
}

func (f *fakeAdminService) GetUser(_ context.Context, id string) (*domain.User, error) {
	f.called = true
	return &domain.User{ID: id, Email: "user@example.com", Role: domain.RoleUser, Status: domain.StatusActive}, nil
}

func (f *fakeAdminService) UpdateUser(_ context.Context, actorID, targetID string, p domain.UserPatch) (*domain.User, error) {
	f.called = true
	f.updateActor, f.updateTarget, f.patch = actorID, targetID, p
	return &domain.User{ID: targetID, Email: "user@example.com", Role: domain.RoleUser, Status: domain.StatusActive}, nil
}

func (f *fakeAdminService) DeleteUser(_ context.Context, actorID, targetID string) error {
	f.called = true
	f.deleteActor, f.deleteTarget = actorID, targetID
	return nil
}

func withClaims(r *http.Request) *http.Request {
	claims := &auth.Claims{RegisteredClaims: jwt.RegisteredClaims{Subject: "11111111-1111-1111-1111-111111111111"}}
	return r.WithContext(context.WithValue(r.Context(), claimsKey, claims))
}

func TestListUsersHandler_ParsesFilter(t *testing.T) {
	svc := &fakeAdminService{}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/users?search=bob&role=user&status=active&limit=5&offset=10", nil)

	ListUsersHandler(svc)(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, domain.UserFilter{Search: "bob", Role: "user", Status: "active", Limit: 5, Offset: 10}, svc.filter)

	var body struct {
		Items []userDTO `json:"items"`
		Total int       `json:"total"`
		Limit int       `json:"limit"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.Len(t, body.Items, 1)
	require.Equal(t, 1, body.Total)
	require.Equal(t, 5, body.Limit)
}

func TestListUsersHandler_InvalidLimit(t *testing.T) {
	svc := &fakeAdminService{}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/users?limit=abc", nil)

	ListUsersHandler(svc)(rec, req)

	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.False(t, svc.called)
}

func TestUpdateUserHandler_PassesPatch(t *testing.T) {
	svc := &fakeAdminService{}
	rec := httptest.NewRecorder()
	req := withClaims(httptest.NewRequest(http.MethodPatch, "/api/v1/admin/users/"+testUserID,
		strings.NewReader(`{"role":"admin","display_name":"Bob"}`)))
	req.SetPathValue("id", testUserID)

	UpdateUserHandler(svc)(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, testUserID, svc.updateTarget)
	require.NotNil(t, svc.patch.Role)
	require.Equal(t, domain.RoleAdmin, *svc.patch.Role)
	require.NotNil(t, svc.patch.DisplayName)
	require.Equal(t, "Bob", *svc.patch.DisplayName)
}

func TestUpdateUserHandler_RejectsUnknownField(t *testing.T) {
	svc := &fakeAdminService{}
	rec := httptest.NewRecorder()
	req := withClaims(httptest.NewRequest(http.MethodPatch, "/api/v1/admin/users/"+testUserID,
		strings.NewReader(`{"is_admin":true}`)))
	req.SetPathValue("id", testUserID)

	UpdateUserHandler(svc)(rec, req)

	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.False(t, svc.called)
}

func TestDeleteUserHandler_Success(t *testing.T) {
	svc := &fakeAdminService{}
	rec := httptest.NewRecorder()
	req := withClaims(httptest.NewRequest(http.MethodDelete, "/api/v1/admin/users/"+testUserID, nil))
	req.SetPathValue("id", testUserID)

	DeleteUserHandler(svc)(rec, req)

	require.Equal(t, http.StatusNoContent, rec.Code)
	require.Equal(t, testUserID, svc.deleteTarget)
}

func TestMapDomainError_LastAdminConflict(t *testing.T) {
	rec := httptest.NewRecorder()
	mapDomainError(rec, domain.ErrLastAdmin)
	require.Equal(t, http.StatusConflict, rec.Code)
}

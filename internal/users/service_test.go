package users

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"habitat/internal/domain"
)

const (
	adminID      = "11111111-1111-1111-1111-111111111111"
	userID       = "22222222-2222-2222-2222-222222222222"
	otherAdminID = "33333333-3333-3333-3333-333333333333"
	missingID    = "99999999-9999-9999-9999-999999999999"
)

type fakeStorage struct {
	users        map[string]domain.User
	lastFilter   domain.UserFilter
	revokedUsers []string
}

func newFakeStorage() *fakeStorage {
	return &fakeStorage{users: map[string]domain.User{
		adminID: {ID: adminID, Email: "admin@example.com", DisplayName: "Admin",
			Role: domain.RoleAdmin, Status: domain.StatusActive},
		otherAdminID: {ID: otherAdminID, Email: "admin2@example.com", DisplayName: "Admin 2",
			Role: domain.RoleAdmin, Status: domain.StatusActive},
		userID: {ID: userID, Email: "user@example.com", DisplayName: "User",
			Role: domain.RoleUser, Status: domain.StatusActive},
	}}
}

func (f *fakeStorage) ListUsers(_ context.Context, filter domain.UserFilter) ([]domain.User, int, error) {
	f.lastFilter = filter
	list := make([]domain.User, 0, len(f.users))
	for _, u := range f.users {
		list = append(list, u)
	}
	return list, len(list), nil
}

func (f *fakeStorage) UserByID(_ context.Context, id string) (*domain.User, error) {
	u, ok := f.users[id]
	if !ok {
		return nil, domain.ErrNotFound
	}
	return &u, nil
}

func (f *fakeStorage) UpdateUser(_ context.Context, id string, p domain.UserPatch) (*domain.User, error) {
	u, ok := f.users[id]
	if !ok {
		return nil, domain.ErrNotFound
	}
	if p.DisplayName != nil {
		u.DisplayName = *p.DisplayName
	}
	if p.Role != nil {
		u.Role = *p.Role
	}
	if p.Status != nil {
		u.Status = *p.Status
	}
	f.users[id] = u
	return &u, nil
}

func (f *fakeStorage) DeleteUser(_ context.Context, id string) error {
	if _, ok := f.users[id]; !ok {
		return domain.ErrNotFound
	}
	delete(f.users, id)
	return nil
}

func (f *fakeStorage) RevokeAllUserRefreshTokens(_ context.Context, userID string) error {
	f.revokedUsers = append(f.revokedUsers, userID)
	return nil
}

func (f *fakeStorage) CountActiveAdmins(_ context.Context) (int, error) {
	n := 0
	for _, u := range f.users {
		if u.Role == domain.RoleAdmin && u.Status == domain.StatusActive {
			n++
		}
	}
	return n, nil
}

func strPtr(s string) *string { return &s }

func TestListUsers_DefaultsAndValidation(t *testing.T) {
	store := newFakeStorage()
	svc := NewService(store)

	_, _, err := svc.ListUsers(t.Context(), domain.UserFilter{})
	require.NoError(t, err)
	require.Equal(t, DefaultLimit, store.lastFilter.Limit)

	cases := []domain.UserFilter{
		{Role: "superadmin"},
		{Status: "deleted"},
		{Limit: MaxLimit + 1},
		{Limit: -1},
		{Offset: -1},
	}
	for _, f := range cases {
		_, _, err := svc.ListUsers(t.Context(), f)
		require.ErrorIs(t, err, domain.ErrValidation)
	}
}

func TestUpdateUser_RejectsSelfRoleOrStatusChange(t *testing.T) {
	svc := NewService(newFakeStorage())

	_, err := svc.UpdateUser(t.Context(), adminID, adminID, domain.UserPatch{Role: strPtr(domain.RoleUser)})
	require.ErrorIs(t, err, domain.ErrValidation)

	_, err = svc.UpdateUser(t.Context(), adminID, adminID, domain.UserPatch{Status: strPtr(domain.StatusBlocked)})
	require.ErrorIs(t, err, domain.ErrValidation)

	// UUID в верхнем регистре не должен обходить защиту от изменения себя.
	_, err = svc.UpdateUser(t.Context(), adminID, strings.ToUpper(adminID), domain.UserPatch{Role: strPtr(domain.RoleUser)})
	require.ErrorIs(t, err, domain.ErrValidation)
}

func TestUpdateUser_AllowsSelfDisplayNameChange(t *testing.T) {
	svc := NewService(newFakeStorage())

	u, err := svc.UpdateUser(t.Context(), adminID, adminID, domain.UserPatch{DisplayName: strPtr("  New Name  ")})
	require.NoError(t, err)
	require.Equal(t, "New Name", u.DisplayName)
}

func TestUpdateUser_RejectsLastAdminDemotion(t *testing.T) {
	store := newFakeStorage()
	delete(store.users, otherAdminID)
	svc := NewService(store)

	_, err := svc.UpdateUser(t.Context(), otherAdminID, adminID, domain.UserPatch{Role: strPtr(domain.RoleUser)})
	require.ErrorIs(t, err, domain.ErrLastAdmin, "нельзя понизить последнего активного администратора")

	_, err = svc.UpdateUser(t.Context(), otherAdminID, adminID, domain.UserPatch{Status: strPtr(domain.StatusBlocked)})
	require.ErrorIs(t, err, domain.ErrLastAdmin)
}

func TestUpdateUser_RevokesSessionsOnBlockAndRoleChange(t *testing.T) {
	store := newFakeStorage()
	svc := NewService(store)

	_, err := svc.UpdateUser(t.Context(), adminID, userID, domain.UserPatch{Status: strPtr(domain.StatusBlocked)})
	require.NoError(t, err)
	require.Contains(t, store.revokedUsers, userID)

	store.revokedUsers = nil
	_, err = svc.UpdateUser(t.Context(), adminID, userID, domain.UserPatch{Role: strPtr(domain.RoleAdmin)})
	require.NoError(t, err)
	require.Contains(t, store.revokedUsers, userID)

	store.revokedUsers = nil
	_, err = svc.UpdateUser(t.Context(), adminID, userID, domain.UserPatch{Status: strPtr(domain.StatusActive)})
	require.NoError(t, err)
	require.Empty(t, store.revokedUsers, "активация не должна отзывать сессии")
}

func TestUpdateUser_ValidatesPatch(t *testing.T) {
	svc := NewService(newFakeStorage())

	_, err := svc.UpdateUser(t.Context(), adminID, userID, domain.UserPatch{})
	require.ErrorIs(t, err, domain.ErrValidation)

	_, err = svc.UpdateUser(t.Context(), adminID, userID, domain.UserPatch{Role: strPtr("root")})
	require.ErrorIs(t, err, domain.ErrValidation)

	_, err = svc.UpdateUser(t.Context(), adminID, userID, domain.UserPatch{Status: strPtr("deleted")})
	require.ErrorIs(t, err, domain.ErrValidation)

	_, err = svc.UpdateUser(t.Context(), adminID, "not-a-uuid", domain.UserPatch{Role: strPtr(domain.RoleUser)})
	require.ErrorIs(t, err, domain.ErrValidation)
}

func TestDeleteUser_ProtectsSelfAndLastAdmin(t *testing.T) {
	store := newFakeStorage()
	svc := NewService(store)

	err := svc.DeleteUser(t.Context(), adminID, adminID)
	require.ErrorIs(t, err, domain.ErrValidation)

	delete(store.users, otherAdminID)
	err = svc.DeleteUser(t.Context(), otherAdminID, adminID)
	require.ErrorIs(t, err, domain.ErrLastAdmin)

	err = svc.DeleteUser(t.Context(), adminID, userID)
	require.NoError(t, err)
	require.NotContains(t, store.users, userID)
}

func TestGetUser_ValidatesUUID(t *testing.T) {
	svc := NewService(newFakeStorage())

	_, err := svc.GetUser(t.Context(), "not-a-uuid")
	require.ErrorIs(t, err, domain.ErrValidation)

	_, err = svc.GetUser(t.Context(), missingID)
	require.ErrorIs(t, err, domain.ErrNotFound)
}

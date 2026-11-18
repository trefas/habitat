package postgres

import (
	"testing"

	"github.com/stretchr/testify/require"

	"habitat/internal/domain"
)

func strPtr(s string) *string { return &s }

func createTestUser(t *testing.T, store *Storage, email, name string) *domain.User {
	t.Helper()
	u := &domain.User{Email: email, PasswordHash: "x", DisplayName: name}
	require.NoError(t, store.CreateUser(t.Context(), u))
	return u
}

func TestListUsers_FilterAndPagination(t *testing.T) {
	store := setupTestDB(t)
	ctx := t.Context()

	alice := createTestUser(t, store, "alice@test.local", "Alice")
	bob := createTestUser(t, store, "bob@test.local", "Bob")
	createTestUser(t, store, "carol@test.local", "Carol")

	_, err := store.UpdateUser(ctx, alice.ID, domain.UserPatch{Role: strPtr(domain.RoleAdmin)})
	require.NoError(t, err)
	_, err = store.UpdateUser(ctx, bob.ID, domain.UserPatch{Status: strPtr(domain.StatusBlocked)})
	require.NoError(t, err)

	list, total, err := store.ListUsers(ctx, domain.UserFilter{Role: domain.RoleAdmin, Limit: 10})
	require.NoError(t, err)
	require.Equal(t, 1, total)
	require.Len(t, list, 1)
	require.Equal(t, alice.ID, list[0].ID)

	list, total, err = store.ListUsers(ctx, domain.UserFilter{Search: "car", Limit: 10})
	require.NoError(t, err)
	require.Equal(t, 1, total)
	require.Equal(t, "carol@test.local", list[0].Email)

	list, total, err = store.ListUsers(ctx, domain.UserFilter{Status: domain.StatusBlocked, Limit: 10})
	require.NoError(t, err)
	require.Equal(t, 1, total)
	require.Equal(t, bob.ID, list[0].ID)

	list, total, err = store.ListUsers(ctx, domain.UserFilter{Limit: 2})
	require.NoError(t, err)
	require.Equal(t, 3, total)
	require.Len(t, list, 2)

	list, total, err = store.ListUsers(ctx, domain.UserFilter{Limit: 2, Offset: 2})
	require.NoError(t, err)
	require.Equal(t, 3, total)
	require.Len(t, list, 1)

	// За пределами выборки total всё равно должен отражать общее количество.
	list, total, err = store.ListUsers(ctx, domain.UserFilter{Limit: 2, Offset: 10})
	require.NoError(t, err)
	require.Equal(t, 3, total)
	require.Empty(t, list)
}

func TestUpdateUser_ReturnsActualFields(t *testing.T) {
	store := setupTestDB(t)
	ctx := t.Context()

	u := createTestUser(t, store, "update@test.local", "Old Name")
	require.Nil(t, u.LastLoginAt)

	updated, err := store.UpdateUser(ctx, u.ID, domain.UserPatch{
		DisplayName: strPtr("New Name"),
		Role:        strPtr(domain.RoleAdmin),
		Status:      strPtr(domain.StatusBlocked),
	})
	require.NoError(t, err)
	require.Equal(t, "New Name", updated.DisplayName)
	require.Equal(t, domain.RoleAdmin, updated.Role)
	require.Equal(t, domain.StatusBlocked, updated.Status)
	require.False(t, updated.UpdatedAt.Before(u.UpdatedAt))

	_, err = store.UpdateUser(ctx, "99999999-9999-9999-9999-999999999999", domain.UserPatch{DisplayName: strPtr("x")})
	require.ErrorIs(t, err, domain.ErrNotFound)
}

func TestDeleteUser_AndCountActiveAdmins(t *testing.T) {
	store := setupTestDB(t)
	ctx := t.Context()

	admin := createTestUser(t, store, "admin@test.local", "Admin")
	_, err := store.UpdateUser(ctx, admin.ID, domain.UserPatch{Role: strPtr(domain.RoleAdmin)})
	require.NoError(t, err)

	user := createTestUser(t, store, "delete@test.local", "Delete Me")

	n, err := store.CountActiveAdmins(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, n)

	require.NoError(t, store.DeleteUser(ctx, user.ID))
	_, err = store.UserByID(ctx, user.ID)
	require.ErrorIs(t, err, domain.ErrNotFound)

	err = store.DeleteUser(ctx, user.ID)
	require.ErrorIs(t, err, domain.ErrNotFound)
}

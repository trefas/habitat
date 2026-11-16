package httpapi

import (
	"context"
	"encoding/json"
	"net/http"

	"habitat/internal/domain"
)

// UserStorage определяет методы БД, необходимые для работы с профилем пользователя.
// Обратите внимание: ctx context.Context обязателен для согласованности с postgres.Storage
type UserStorage interface {
	UserByID(ctx context.Context, id string) (*domain.User, error)
}

func MeHandler(store UserStorage) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		claims, ok := GetClaims(r.Context())
		if !ok {
			writeError(w, http.StatusUnauthorized, "invalid_token", "Токен не найден в контексте")
			return
		}

		// Передаем контекст запроса в хранилище
		u, err := store.UserByID(r.Context(), claims.Subject)
		if err != nil {
			mapDomainError(w, err)
			return
		}

		// Возвращаем DTO без password_hash
		writeJSON(w, http.StatusOK, map[string]any{
			"id":           u.ID,
			"email":        u.Email,
			"display_name": u.DisplayName,
			"role":         u.Role,
			"status":       u.Status,
			"created_at":   u.CreatedAt,
		})
	}
}

type PasswordService interface {
	ChangePassword(ctx context.Context, userID, currentPassword, newPassword string) error
}

func ChangePasswordHandler(svc PasswordService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		claims, ok := GetClaims(r.Context())
		if !ok {
			writeError(w, http.StatusUnauthorized, "invalid_token", "Токен не найден")
			return
		}

		var req struct {
			CurrentPassword string `json:"current_password"`
			NewPassword     string `json:"new_password"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_json", "Некорректный JSON")
			return
		}

		err := svc.ChangePassword(r.Context(), claims.Subject, req.CurrentPassword, req.NewPassword)
		if err != nil {
			mapDomainError(w, err)
			return
		}

		// 204 No Content — пароль изменен, все сессии отозваны
		w.WriteHeader(http.StatusNoContent)
	}
}

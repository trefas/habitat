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
		writeJSON(w, http.StatusOK, newUserDTO(u))
	}
}

type ProfileService interface {
	UpdateProfile(ctx context.Context, userID string, displayName *string) (*domain.User, error)
}

// UpdateMeHandler изменяет собственный профиль пользователя.
// Разрешено менять только display_name.
func UpdateMeHandler(svc ProfileService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		claims, ok := GetClaims(r.Context())
		if !ok {
			writeError(w, http.StatusUnauthorized, "invalid_token", "Токен не найден")
			return
		}

		var req struct {
			DisplayName *string `json:"display_name"`
		}
		r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
		dec := json.NewDecoder(r.Body)
		dec.DisallowUnknownFields()
		if err := dec.Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_json", "Некорректный JSON")
			return
		}

		u, err := svc.UpdateProfile(r.Context(), claims.Subject, req.DisplayName)
		if err != nil {
			mapDomainError(w, err)
			return
		}

		writeJSON(w, http.StatusOK, newUserDTO(u))
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

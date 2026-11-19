package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"habitat/internal/domain"
)

// defaultPageLimit используется, если limit не передан в запросе.
// Значение совпадает с users.DefaultLimit, но не создаёт зависимости транспорта от сервиса.
const defaultPageLimit = 20

// AdminUserService описывает административные операции над пользователями.
type AdminUserService interface {
	ListUsers(ctx context.Context, f domain.UserFilter) ([]domain.User, int, error)
	GetUser(ctx context.Context, id string) (*domain.User, error)
	UpdateUser(ctx context.Context, actorID, targetID string, p domain.UserPatch) (*domain.User, error)
	DeleteUser(ctx context.Context, actorID, targetID string) error
}

// userDTO — представление пользователя в API без чувствительных полей.
type userDTO struct {
	ID          string     `json:"id"`
	Email       string     `json:"email"`
	DisplayName string     `json:"display_name"`
	Role        string     `json:"role"`
	Status      string     `json:"status"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
	LastLoginAt *time.Time `json:"last_login_at"`
	DeletedAt   *time.Time `json:"deleted_at"`
}

func newUserDTO(u *domain.User) userDTO {
	return userDTO{
		ID:          u.ID,
		Email:       u.Email,
		DisplayName: u.DisplayName,
		Role:        u.Role,
		Status:      u.Status,
		CreatedAt:   u.CreatedAt,
		UpdatedAt:   u.UpdatedAt,
		LastLoginAt: u.LastLoginAt,
		DeletedAt:   u.DeletedAt,
	}
}

// ListUsersHandler возвращает страницу пользователей с фильтрацией по search/role/status.
func ListUsersHandler(svc AdminUserService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		limit, err := queryInt(r, "limit")
		if err != nil {
			writeError(w, http.StatusBadRequest, "validation_error", err.Error())
			return
		}
		offset, err := queryInt(r, "offset")
		if err != nil {
			writeError(w, http.StatusBadRequest, "validation_error", err.Error())
			return
		}

		q := r.URL.Query()
		filter := domain.UserFilter{
			Search: q.Get("search"),
			Role:   q.Get("role"),
			Status: q.Get("status"),
			Limit:  limit,
			Offset: offset,
		}

		list, total, err := svc.ListUsers(r.Context(), filter)
		if err != nil {
			mapDomainError(w, err)
			return
		}

		items := make([]userDTO, 0, len(list))
		for i := range list {
			items = append(items, newUserDTO(&list[i]))
		}

		effectiveLimit := limit
		if effectiveLimit == 0 {
			effectiveLimit = defaultPageLimit
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"items":  items,
			"total":  total,
			"limit":  effectiveLimit,
			"offset": offset,
		})
	}
}

// GetUserHandler возвращает пользователя по идентификатору.
func GetUserHandler(svc AdminUserService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		u, err := svc.GetUser(r.Context(), r.PathValue("id"))
		if err != nil {
			mapDomainError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, newUserDTO(u))
	}
}

// UpdateUserHandler частично обновляет пользователя (display_name, role, status).
func UpdateUserHandler(svc AdminUserService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		claims, ok := GetClaims(r.Context())
		if !ok {
			writeError(w, http.StatusUnauthorized, "invalid_token", "Токен не найден")
			return
		}

		var req struct {
			DisplayName *string `json:"display_name"`
			Role        *string `json:"role"`
			Status      *string `json:"status"`
		}
		r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
		dec := json.NewDecoder(r.Body)
		dec.DisallowUnknownFields()
		if err := dec.Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_json", "Некорректный JSON")
			return
		}

		u, err := svc.UpdateUser(r.Context(), claims.Subject, r.PathValue("id"), domain.UserPatch{
			DisplayName: req.DisplayName,
			Role:        req.Role,
			Status:      req.Status,
		})
		if err != nil {
			mapDomainError(w, err)
			return
		}

		writeJSON(w, http.StatusOK, newUserDTO(u))
	}
}

// DeleteUserHandler удаляет пользователя.
func DeleteUserHandler(svc AdminUserService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		claims, ok := GetClaims(r.Context())
		if !ok {
			writeError(w, http.StatusUnauthorized, "invalid_token", "Токен не найден")
			return
		}

		if err := svc.DeleteUser(r.Context(), claims.Subject, r.PathValue("id")); err != nil {
			mapDomainError(w, err)
			return
		}

		w.WriteHeader(http.StatusNoContent)
	}
}

// queryInt разбирает необязательный целочисленный query-параметр.
func queryInt(r *http.Request, key string) (int, error) {
	raw := r.URL.Query().Get(key)
	if raw == "" {
		return 0, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("параметр %s должен быть целым числом", key)
	}
	return n, nil
}

package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"habitat/internal/domain"
)

// NotificationService описывает работу с входящими уведомлениями.
type NotificationService interface {
	List(ctx context.Context, userID string, unreadOnly bool, limit, offset int) ([]domain.Notification, int, error)
	MarkRead(ctx context.Context, userID, notificationID string) error
}

type notificationDTO struct {
	ID        string          `json:"id"`
	Kind      string          `json:"kind"`
	EntityID  *string         `json:"entity_id"`
	ShareID   *string         `json:"share_id"`
	ActorID   *string         `json:"actor_id"`
	Payload   json.RawMessage `json:"payload"`
	ReadAt    *time.Time      `json:"read_at"`
	IsRead    bool            `json:"is_read"`
	CreatedAt time.Time       `json:"created_at"`
}

func newNotificationDTO(n *domain.Notification) notificationDTO {
	payload := json.RawMessage(n.Payload)
	if len(payload) == 0 {
		payload = json.RawMessage(`{}`)
	}
	return notificationDTO{
		ID:        n.ID,
		Kind:      n.Kind,
		EntityID:  n.EntityID,
		ShareID:   n.ShareID,
		ActorID:   n.ActorID,
		Payload:   payload,
		ReadAt:    n.ReadAt,
		IsRead:    n.ReadAt != nil,
		CreatedAt: n.CreatedAt,
	}
}

// ListNotificationsHandler возвращает уведомления пользователя.
func ListNotificationsHandler(svc NotificationService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		claims, ok := GetClaims(r.Context())
		if !ok {
			writeError(w, http.StatusUnauthorized, "invalid_token", "Токен не найден")
			return
		}

		unreadOnly := false
		if raw := r.URL.Query().Get("unread"); raw != "" {
			v, err := strconv.ParseBool(raw)
			if err != nil {
				writeError(w, http.StatusBadRequest, "validation_error", "параметр unread должен быть boolean")
				return
			}
			unreadOnly = v
		}

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

		list, total, err := svc.List(r.Context(), claims.Subject, unreadOnly, limit, offset)
		if err != nil {
			mapDomainError(w, err)
			return
		}

		items := make([]notificationDTO, 0, len(list))
		for i := range list {
			items = append(items, newNotificationDTO(&list[i]))
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

// MarkNotificationReadHandler отмечает уведомление прочитанным.
func MarkNotificationReadHandler(svc NotificationService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		claims, ok := GetClaims(r.Context())
		if !ok {
			writeError(w, http.StatusUnauthorized, "invalid_token", "Токен не найден")
			return
		}

		if err := svc.MarkRead(r.Context(), claims.Subject, r.PathValue("id")); err != nil {
			mapDomainError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

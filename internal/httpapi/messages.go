package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"habitat/internal/domain"
)

// MessageService описывает сценарии работы с сообщениями.
type MessageService interface {
	Send(ctx context.Context, ownerID, text string, recipientRefs []string) (*domain.MessageView, error)
	List(ctx context.Context, userID, box string, limit, offset int) ([]domain.Message, int, error)
	Get(ctx context.Context, userID, messageID string) (*domain.MessageView, error)
	MarkRead(ctx context.Context, userID, messageID string) error
	Share(ctx context.Context, ownerID, messageID string, recipientRefs []string) ([]domain.MessageRecipient, error)
	Revoke(ctx context.Context, ownerID, messageID, granteeID string) error
}

type personDTO struct {
	ID          string `json:"id"`
	Email       string `json:"email"`
	DisplayName string `json:"display_name"`
}

type messageRecipientDTO struct {
	UserID      string     `json:"user_id"`
	Email       string     `json:"email"`
	DisplayName string     `json:"display_name"`
	ReadAt      *time.Time `json:"read_at"`
	IsRead      bool       `json:"is_read"`
}

type messageDTO struct {
	ID              string                `json:"id"`
	Text            string                `json:"text"`
	Sender          personDTO             `json:"sender"`
	IsOwner         bool                  `json:"is_owner"`
	ReadAt          *time.Time            `json:"read_at"`
	IsRead          bool                  `json:"is_read"`
	RecipientsCount int                   `json:"recipients_count"`
	ReadCount       int                   `json:"read_count"`
	Recipients      []messageRecipientDTO `json:"recipients,omitempty"`
	CreatedAt       time.Time             `json:"created_at"`
	UpdatedAt       time.Time             `json:"updated_at"`
}

func newMessageDTO(view *domain.MessageView) messageDTO {
	m := view.Message
	dto := messageDTO{
		ID:   m.ID,
		Text: m.Text,
		Sender: personDTO{
			ID:          m.OwnerID,
			Email:       m.OwnerEmail,
			DisplayName: m.OwnerDisplayName,
		},
		IsOwner:         m.IsOwner,
		ReadAt:          m.ReadAt,
		IsRead:          m.ReadAt != nil,
		RecipientsCount: m.RecipientsCount,
		ReadCount:       m.ReadCount,
		CreatedAt:       m.CreatedAt,
		UpdatedAt:       m.UpdatedAt,
	}
	if view.Recipients != nil {
		dto.Recipients = make([]messageRecipientDTO, 0, len(view.Recipients))
		for _, r := range view.Recipients {
			dto.Recipients = append(dto.Recipients, messageRecipientDTO{
				UserID:      r.UserID,
				Email:       r.Email,
				DisplayName: r.DisplayName,
				ReadAt:      r.ReadAt,
				IsRead:      r.ReadAt != nil,
			})
		}
	}
	return dto
}

// CreateMessageHandler отправляет сообщение одному или нескольким получателям.
func CreateMessageHandler(svc MessageService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		claims, ok := GetClaims(r.Context())
		if !ok {
			writeError(w, http.StatusUnauthorized, "invalid_token", "Токен не найден")
			return
		}

		var req struct {
			Text       string   `json:"text"`
			Recipients []string `json:"recipients"`
		}
		if !decodeJSON(w, r, &req) {
			return
		}

		view, err := svc.Send(r.Context(), claims.Subject, req.Text, req.Recipients)
		if err != nil {
			mapDomainError(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, newMessageDTO(view))
	}
}

// ListMessagesHandler возвращает входящие, отправленные или все сообщения.
func ListMessagesHandler(svc MessageService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		claims, ok := GetClaims(r.Context())
		if !ok {
			writeError(w, http.StatusUnauthorized, "invalid_token", "Токен не найден")
			return
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

		box := r.URL.Query().Get("box")
		list, total, err := svc.List(r.Context(), claims.Subject, box, limit, offset)
		if err != nil {
			mapDomainError(w, err)
			return
		}

		items := make([]messageDTO, 0, len(list))
		for i := range list {
			items = append(items, newMessageDTO(&domain.MessageView{Message: list[i]}))
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

// GetMessageHandler возвращает сообщение, доступное текущему пользователю.
func GetMessageHandler(svc MessageService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		claims, ok := GetClaims(r.Context())
		if !ok {
			writeError(w, http.StatusUnauthorized, "invalid_token", "Токен не найден")
			return
		}

		view, err := svc.Get(r.Context(), claims.Subject, r.PathValue("id"))
		if err != nil {
			mapDomainError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, newMessageDTO(view))
	}
}

// MarkMessageReadHandler фиксирует прочтение сообщения получателем.
func MarkMessageReadHandler(svc MessageService) http.HandlerFunc {
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

// ListMessageReceiptsHandler показывает владельцу, кто прочитал сообщение.
func ListMessageReceiptsHandler(svc MessageService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		claims, ok := GetClaims(r.Context())
		if !ok {
			writeError(w, http.StatusUnauthorized, "invalid_token", "Токен не найден")
			return
		}

		view, err := svc.Get(r.Context(), claims.Subject, r.PathValue("id"))
		if err != nil {
			mapDomainError(w, err)
			return
		}
		if !view.Message.IsOwner {
			mapDomainError(w, domain.ErrForbidden)
			return
		}

		items := make([]messageRecipientDTO, 0, len(view.Recipients))
		for _, rec := range view.Recipients {
			items = append(items, messageRecipientDTO{
				UserID:      rec.UserID,
				Email:       rec.Email,
				DisplayName: rec.DisplayName,
				ReadAt:      rec.ReadAt,
				IsRead:      rec.ReadAt != nil,
			})
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": items})
	}
}

// ShareMessageHandler добавляет получателей к своему сообщению.
func ShareMessageHandler(svc MessageService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		claims, ok := GetClaims(r.Context())
		if !ok {
			writeError(w, http.StatusUnauthorized, "invalid_token", "Токен не найден")
			return
		}

		var req struct {
			Recipients []string `json:"recipients"`
		}
		if !decodeJSON(w, r, &req) {
			return
		}

		recipients, err := svc.Share(r.Context(), claims.Subject, r.PathValue("id"), req.Recipients)
		if err != nil {
			mapDomainError(w, err)
			return
		}

		items := make([]messageRecipientDTO, 0, len(recipients))
		for _, rec := range recipients {
			items = append(items, messageRecipientDTO{
				UserID:      rec.UserID,
				Email:       rec.Email,
				DisplayName: rec.DisplayName,
				ReadAt:      rec.ReadAt,
				IsRead:      rec.ReadAt != nil,
			})
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": items})
	}
}

// RevokeMessageShareHandler отзывает доступ получателя к сообщению.
func RevokeMessageShareHandler(svc MessageService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		claims, ok := GetClaims(r.Context())
		if !ok {
			writeError(w, http.StatusUnauthorized, "invalid_token", "Токен не найден")
			return
		}

		err := svc.Revoke(r.Context(), claims.Subject, r.PathValue("id"), r.PathValue("granteeID"))
		if err != nil {
			mapDomainError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

// decodeJSON читает JSON с ограничением размера и запретом неизвестных полей.
func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "Некорректный JSON")
		return false
	}
	return true
}

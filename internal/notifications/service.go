// Package notifications реализует входящие уведомления пользователей.
package notifications

import (
	"context"
	"fmt"
	"strings"

	"habitat/internal/domain"
)

const (
	DefaultLimit = 20
	MaxLimit     = 100
)

// Storage описывает операции БД для уведомлений.
type Storage interface {
	ListNotifications(ctx context.Context, userID string, unreadOnly bool, limit, offset int) ([]domain.Notification, int, error)
	MarkNotificationRead(ctx context.Context, userID, notificationID string) error
}

// Service реализует бизнес-логику уведомлений.
type Service struct {
	store Storage
}

// NewService создаёт сервис уведомлений.
func NewService(store Storage) *Service {
	return &Service{store: store}
}

// List возвращает уведомления пользователя.
func (s *Service) List(ctx context.Context, userID string, unreadOnly bool, limit, offset int) ([]domain.Notification, int, error) {
	if limit == 0 {
		limit = DefaultLimit
	}
	if limit < 1 || limit > MaxLimit {
		return nil, 0, fmt.Errorf("%w: limit должен быть от 1 до %d", domain.ErrValidation, MaxLimit)
	}
	if offset < 0 {
		return nil, 0, fmt.Errorf("%w: offset не может быть отрицательным", domain.ErrValidation)
	}
	return s.store.ListNotifications(ctx, userID, unreadOnly, limit, offset)
}

// MarkRead отмечает уведомление прочитанным. Идемпотентно и только своё.
func (s *Service) MarkRead(ctx context.Context, userID, notificationID string) error {
	notificationID = strings.ToLower(notificationID)
	if !domain.IsUUID(notificationID) {
		return fmt.Errorf("%w: некорректный идентификатор уведомления", domain.ErrValidation)
	}
	return s.store.MarkNotificationRead(ctx, userID, notificationID)
}

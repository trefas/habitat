// Package messaging реализует сценарии работы с одиночными сообщениями.
package messaging

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"habitat/internal/domain"
)

const (
	DefaultLimit = 20
	MaxLimit     = 100

	maxTextLength = 20000
)

// Коробки, в которых пользователь может смотреть сообщения.
const (
	BoxInbox = "inbox"
	BoxSent  = "sent"
	BoxAll   = "all"
)

// Storage описывает операции БД, необходимые для сообщений.
type Storage interface {
	UserByID(ctx context.Context, id string) (*domain.User, error)
	UserByEmail(ctx context.Context, email string) (*domain.User, error)

	CreateMessage(ctx context.Context, ownerID, text string, recipients []domain.MessageRecipient) (*domain.Message, error)
	ListMessages(ctx context.Context, userID, box string, limit, offset int) ([]domain.Message, int, error)
	GetMessage(ctx context.Context, userID, id string) (*domain.Message, error)
	ListMessageRecipients(ctx context.Context, ownerID, messageID string) ([]domain.MessageRecipient, error)
	MarkMessageRead(ctx context.Context, userID, messageID string) error
	AddMessageRecipients(ctx context.Context, ownerID, messageID string, recipients []domain.MessageRecipient) error
	RevokeMessageShare(ctx context.Context, ownerID, messageID, granteeID string) error
}

// Service реализует бизнес-логику сообщений.
type Service struct {
	store Storage
}

// NewService создаёт сервис сообщений.
func NewService(store Storage) *Service {
	return &Service{store: store}
}

// Send создаёт сообщение и выдаёт доступ перечисленным получателям.
// recipientRefs — email или UUID получателей.
func (s *Service) Send(ctx context.Context, ownerID, text string, recipientRefs []string) (*domain.MessageView, error) {
	owner, err := s.store.UserByID(ctx, ownerID)
	if err != nil {
		return nil, err
	}
	if owner.Status != domain.StatusActive {
		return nil, domain.ErrUserBlocked
	}

	text = strings.TrimSpace(text)
	if n := utf8.RuneCountInString(text); n < 1 || n > maxTextLength {
		return nil, fmt.Errorf("%w: текст сообщения должен быть от 1 до %d символов", domain.ErrValidation, maxTextLength)
	}

	recipients, err := s.resolveRecipients(ctx, ownerID, recipientRefs, nil)
	if err != nil {
		return nil, err
	}
	if len(recipients) == 0 {
		return nil, fmt.Errorf("%w: нужен хотя бы один получатель", domain.ErrValidation)
	}

	msg, err := s.store.CreateMessage(ctx, ownerID, text, recipients)
	if err != nil {
		return nil, err
	}

	return &domain.MessageView{Message: *msg, Recipients: recipients}, nil
}

// List возвращает сообщения пользователя: входящие, отправленные или все.
func (s *Service) List(ctx context.Context, userID, box string, limit, offset int) ([]domain.Message, int, error) {
	switch box {
	case "":
		box = BoxInbox
	case BoxInbox, BoxSent, BoxAll:
	default:
		return nil, 0, fmt.Errorf("%w: неизвестная папка %q", domain.ErrValidation, box)
	}

	if limit == 0 {
		limit = DefaultLimit
	}
	if limit < 1 || limit > MaxLimit {
		return nil, 0, fmt.Errorf("%w: limit должен быть от 1 до %d", domain.ErrValidation, MaxLimit)
	}
	if offset < 0 {
		return nil, 0, fmt.Errorf("%w: offset не может быть отрицательным", domain.ErrValidation)
	}

	return s.store.ListMessages(ctx, userID, box, limit, offset)
}

// Get возвращает сообщение, если пользователь владелец или получатель.
// Владельцу дополнительно возвращается список получателей и факты прочтения.
func (s *Service) Get(ctx context.Context, userID, messageID string) (*domain.MessageView, error) {
	messageID = strings.ToLower(messageID)
	if !domain.IsUUID(messageID) {
		return nil, fmt.Errorf("%w: некорректный идентификатор сообщения", domain.ErrValidation)
	}

	msg, err := s.store.GetMessage(ctx, userID, messageID)
	if err != nil {
		return nil, err
	}

	view := &domain.MessageView{Message: *msg}
	if msg.IsOwner {
		recipients, err := s.store.ListMessageRecipients(ctx, userID, messageID)
		if err != nil {
			return nil, err
		}
		view.Recipients = recipients
	}
	return view, nil
}

// MarkRead фиксирует факт прочтения сообщения получателем. Идемпотентно.
func (s *Service) MarkRead(ctx context.Context, userID, messageID string) error {
	view, err := s.Get(ctx, userID, messageID)
	if err != nil {
		return err
	}
	if view.Message.IsOwner {
		return fmt.Errorf("%w: владелец не может отметить собственное сообщение прочитанным", domain.ErrValidation)
	}
	return s.store.MarkMessageRead(ctx, userID, view.Message.ID)
}

// Share добавляет получателей к своему сообщению и возвращает актуальный список.
func (s *Service) Share(ctx context.Context, ownerID, messageID string, recipientRefs []string) ([]domain.MessageRecipient, error) {
	view, err := s.Get(ctx, ownerID, messageID)
	if err != nil {
		return nil, err
	}
	if !view.Message.IsOwner {
		return nil, domain.ErrForbidden
	}

	existing := make(map[string]bool, len(view.Recipients))
	for _, r := range view.Recipients {
		existing[r.UserID] = true
	}

	recipients, err := s.resolveRecipients(ctx, ownerID, recipientRefs, existing)
	if err != nil {
		return nil, err
	}
	if len(recipients) > 0 {
		if err := s.store.AddMessageRecipients(ctx, ownerID, view.Message.ID, recipients); err != nil {
			return nil, err
		}
	}

	return s.store.ListMessageRecipients(ctx, ownerID, view.Message.ID)
}

// Revoke отзывает доступ получателя к сообщению.
func (s *Service) Revoke(ctx context.Context, ownerID, messageID, granteeID string) error {
	view, err := s.Get(ctx, ownerID, messageID)
	if err != nil {
		return err
	}
	if !view.Message.IsOwner {
		return domain.ErrForbidden
	}

	granteeID = strings.ToLower(granteeID)
	if !domain.IsUUID(granteeID) {
		return fmt.Errorf("%w: некорректный идентификатор получателя", domain.ErrValidation)
	}
	return s.store.RevokeMessageShare(ctx, ownerID, view.Message.ID, granteeID)
}

// resolveRecipients превращает email/UUID в активных пользователей,
// убирает дубликаты, самого отправителя и уже существующих получателей.
func (s *Service) resolveRecipients(ctx context.Context, ownerID string, refs []string,
	existing map[string]bool) ([]domain.MessageRecipient, error) {

	seen := make(map[string]bool, len(refs))
	out := make([]domain.MessageRecipient, 0, len(refs))

	for _, ref := range refs {
		ref = strings.TrimSpace(ref)
		if ref == "" {
			continue
		}

		var (
			u   *domain.User
			err error
		)
		if domain.IsUUID(ref) {
			u, err = s.store.UserByID(ctx, strings.ToLower(ref))
		} else {
			u, err = s.store.UserByEmail(ctx, domain.NormalizeEmail(ref))
		}
		if err != nil {
			if errors.Is(err, domain.ErrNotFound) {
				return nil, fmt.Errorf("%w: получатель %q не найден", domain.ErrValidation, ref)
			}
			return nil, err
		}

		if u.ID == ownerID {
			return nil, fmt.Errorf("%w: нельзя отправить сообщение самому себе", domain.ErrValidation)
		}
		if u.Status != domain.StatusActive {
			return nil, fmt.Errorf("%w: получатель %q неактивен", domain.ErrValidation, ref)
		}
		if seen[u.ID] || (existing != nil && existing[u.ID]) {
			continue
		}
		seen[u.ID] = true

		out = append(out, domain.MessageRecipient{
			UserID:      u.ID,
			Email:       u.Email,
			DisplayName: u.DisplayName,
		})
	}

	return out, nil
}

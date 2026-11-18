// Package users реализует административное управление пользователями.
package users

import (
	"context"
	"fmt"
	"strings"

	"habitat/internal/domain"
)

const (
	// DefaultLimit — размер страницы по умолчанию.
	DefaultLimit = 20
	// MaxLimit — максимальный размер страницы.
	MaxLimit = 100
	// maxSearchLength — максимальная длина поисковой строки.
	maxSearchLength = 100
	// maxDisplayNameLength — максимальная длина отображаемого имени.
	maxDisplayNameLength = 100
)

// Storage описывает операции БД, необходимые для управления пользователями.
type Storage interface {
	ListUsers(ctx context.Context, f domain.UserFilter) ([]domain.User, int, error)
	UserByID(ctx context.Context, id string) (*domain.User, error)
	UpdateUser(ctx context.Context, id string, p domain.UserPatch) (*domain.User, error)
	DeleteUser(ctx context.Context, id string) error
	RevokeAllUserRefreshTokens(ctx context.Context, userID string) error
	CountActiveAdmins(ctx context.Context) (int, error)
}

// Service реализует бизнес-логику административного CRUD пользователей.
type Service struct {
	store Storage
}

// NewService создает сервис управления пользователями.
func NewService(store Storage) *Service {
	return &Service{store: store}
}

// ListUsers возвращает страницу пользователей с нормализованными параметрами фильтра.
func (s *Service) ListUsers(ctx context.Context, f domain.UserFilter) ([]domain.User, int, error) {
	f.Search = strings.TrimSpace(f.Search)
	if len(f.Search) > maxSearchLength {
		return nil, 0, fmt.Errorf("%w: поисковая строка длиннее %d символов", domain.ErrValidation, maxSearchLength)
	}
	if f.Role != "" && !domain.ValidRole(f.Role) {
		return nil, 0, fmt.Errorf("%w: неизвестная роль %q", domain.ErrValidation, f.Role)
	}
	if f.Status != "" && !domain.ValidStatus(f.Status) {
		return nil, 0, fmt.Errorf("%w: неизвестный статус %q", domain.ErrValidation, f.Status)
	}
	if f.Limit == 0 {
		f.Limit = DefaultLimit
	}
	if f.Limit < 1 || f.Limit > MaxLimit {
		return nil, 0, fmt.Errorf("%w: limit должен быть от 1 до %d", domain.ErrValidation, MaxLimit)
	}
	if f.Offset < 0 {
		return nil, 0, fmt.Errorf("%w: offset не может быть отрицательным", domain.ErrValidation)
	}

	return s.store.ListUsers(ctx, f)
}

// GetUser возвращает пользователя по идентификатору.
func (s *Service) GetUser(ctx context.Context, id string) (*domain.User, error) {
	id = strings.ToLower(id)
	if !domain.IsUUID(id) {
		return nil, fmt.Errorf("%w: некорректный идентификатор пользователя", domain.ErrValidation)
	}
	return s.store.UserByID(ctx, id)
}

// UpdateUser изменяет профиль, роль или статус пользователя.
// actorID — администратор, выполняющий операцию.
func (s *Service) UpdateUser(ctx context.Context, actorID, targetID string, p domain.UserPatch) (*domain.User, error) {
	actorID = strings.ToLower(actorID)
	targetID = strings.ToLower(targetID)
	if !domain.IsUUID(targetID) {
		return nil, fmt.Errorf("%w: некорректный идентификатор пользователя", domain.ErrValidation)
	}
	if p.DisplayName == nil && p.Role == nil && p.Status == nil {
		return nil, fmt.Errorf("%w: не передано ни одного поля для изменения", domain.ErrValidation)
	}

	if p.DisplayName != nil {
		name := strings.TrimSpace(*p.DisplayName)
		if len(name) > maxDisplayNameLength {
			return nil, fmt.Errorf("%w: display_name длиннее %d символов", domain.ErrValidation, maxDisplayNameLength)
		}
		p.DisplayName = &name
	}
	if p.Role != nil && !domain.ValidRole(*p.Role) {
		return nil, fmt.Errorf("%w: неизвестная роль %q", domain.ErrValidation, *p.Role)
	}
	if p.Status != nil && !domain.ValidStatus(*p.Status) {
		return nil, fmt.Errorf("%w: неизвестный статус %q", domain.ErrValidation, *p.Status)
	}

	// Администратор не должен иметь возможности лишить прав или заблокировать сам себя.
	if actorID == targetID && (p.Role != nil || p.Status != nil) {
		return nil, fmt.Errorf("%w: нельзя менять собственную роль или статус", domain.ErrValidation)
	}

	current, err := s.store.UserByID(ctx, targetID)
	if err != nil {
		return nil, err
	}

	// Не позволяем оставить систему без активного администратора.
	removesActiveAdmin := current.Role == domain.RoleAdmin && current.Status == domain.StatusActive &&
		((p.Role != nil && *p.Role != domain.RoleAdmin) ||
			(p.Status != nil && *p.Status != domain.StatusActive))
	if removesActiveAdmin {
		admins, err := s.store.CountActiveAdmins(ctx)
		if err != nil {
			return nil, err
		}
		if admins <= 1 {
			return nil, domain.ErrLastAdmin
		}
	}

	updated, err := s.store.UpdateUser(ctx, targetID, p)
	if err != nil {
		return nil, err
	}

	// Блокировка/деактивация и смена роли должны немедленно прекратить выдачу
	// новых access-токенов по существующим refresh-токенам.
	roleChanged := p.Role != nil && *p.Role != current.Role
	statusDeactivated := p.Status != nil && *p.Status != domain.StatusActive
	if roleChanged || statusDeactivated {
		if err := s.store.RevokeAllUserRefreshTokens(ctx, targetID); err != nil {
			return nil, err
		}
	}

	return updated, nil
}

// DeleteUser удаляет пользователя. Удалить собственную учётную запись нельзя.
func (s *Service) DeleteUser(ctx context.Context, actorID, targetID string) error {
	actorID = strings.ToLower(actorID)
	targetID = strings.ToLower(targetID)
	if !domain.IsUUID(targetID) {
		return fmt.Errorf("%w: некорректный идентификатор пользователя", domain.ErrValidation)
	}
	if actorID == targetID {
		return fmt.Errorf("%w: нельзя удалить собственную учётную запись", domain.ErrValidation)
	}

	target, err := s.store.UserByID(ctx, targetID)
	if err != nil {
		return err
	}

	if target.Role == domain.RoleAdmin && target.Status == domain.StatusActive {
		admins, err := s.store.CountActiveAdmins(ctx)
		if err != nil {
			return err
		}
		if admins <= 1 {
			return domain.ErrLastAdmin
		}
	}

	return s.store.DeleteUser(ctx, targetID)
}

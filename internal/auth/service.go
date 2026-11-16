package auth

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"habitat/internal/domain"
)

// Storage определяет методы БД, необходимые сервису аутентификации.
type Storage interface {
	CreateUser(ctx context.Context, u *domain.User) error
	UserByEmail(ctx context.Context, email string) (*domain.User, error)
	UserByID(ctx context.Context, id string) (*domain.User, error)
	TouchLastLogin(ctx context.Context, id string) error
	UpdatePassword(ctx context.Context, id, newHash string) error        // <-- ДОБАВИТЬ
	RevokeAllUserRefreshTokens(ctx context.Context, userID string) error // <-- ДОБАВИТЬ
	CreateRefreshToken(ctx context.Context, userID string, hash []byte, exp time.Time, m domain.SessionMeta) error
	RotateRefreshToken(ctx context.Context, oldHash, newHash []byte, exp time.Time, m domain.SessionMeta) (string, string, error)
	RevokeFamilyByHash(ctx context.Context, hash []byte) error
}

// Service реализует бизнес-логику аутентификации.
type Service struct {
	store      Storage
	hasher     PasswordHasher
	tokens     TokenManager
	refreshTTL time.Duration
	dummyHash  string // Для выравнивания времени ответа при неверном email
}

// NewService создает новый экземпляр Service.
func NewService(store Storage, hasher PasswordHasher, tokens TokenManager, refreshTTL time.Duration) *Service {
	// Генерируем фиктивный хеш один раз при старте, чтобы время ответа
	// на "пользователь не найден" и "неверный пароль" было одинаковым.
	dummyHash, _ := hasher.Hash("dummy-password-for-timing-attack-prevention")

	return &Service{
		store:      store,
		hasher:     hasher,
		tokens:     tokens,
		refreshTTL: refreshTTL,
		dummyHash:  dummyHash,
	}
}

// Register регистрирует нового пользователя.
func (s *Service) Register(ctx context.Context, email, password, displayName string) (*domain.User, error) {
	email = domain.NormalizeEmail(email)
	if !strings.Contains(email, "@") || len(email) < 3 || len(email) > 254 {
		return nil, fmt.Errorf("%w: некорректный email", domain.ErrValidation)
	}
	if n := len(password); n < 8 || n > 72 { // 72 — жёсткий лимит bcrypt
		return nil, fmt.Errorf("%w: длина пароля должна быть от 8 до 72 байт", domain.ErrValidation)
	}

	hash, err := s.hasher.Hash(password)
	if err != nil {
		return nil, err
	}

	u := &domain.User{
		Email:        email,
		PasswordHash: hash,
		DisplayName:  strings.TrimSpace(displayName),
	}

	if err := s.store.CreateUser(ctx, u); err != nil {
		return nil, err // Ошибка ErrEmailTaken пробросится наверх
	}

	return u, nil
}

// Login аутентифицирует пользователя и возвращает пару токенов.
func (s *Service) Login(ctx context.Context, email, password string, meta domain.SessionMeta) (*domain.TokenPair, *domain.User, error) {
	u, err := s.store.UserByEmail(ctx, domain.NormalizeEmail(email))
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			// Выравнивание времени: делаем вид, что хешируем, даже если пользователя нет
			_ = s.hasher.Compare(s.dummyHash, password)
			return nil, nil, domain.ErrInvalidCredentials
		}
		return nil, nil, err
	}

	if err := s.hasher.Compare(u.PasswordHash, password); err != nil {
		return nil, nil, domain.ErrInvalidCredentials
	}

	if u.Status != "active" {
		return nil, nil, domain.ErrUserBlocked
	}

	// Генерируем refresh-токен
	rawRefresh, hashRefresh, err := NewRefreshToken()
	if err != nil {
		return nil, nil, err
	}

	exp := time.Now().Add(s.refreshTTL)
	if err := s.store.CreateRefreshToken(ctx, u.ID, hashRefresh, exp, meta); err != nil {
		return nil, nil, err
	}

	// Генерируем access-токен
	access, ttl, err := s.tokens.NewAccessToken(u.ID, u.Role)
	if err != nil {
		return nil, nil, err
	}

	// Обновляем время последнего входа (не критично, если упадет, логин не прерываем)
	_ = s.store.TouchLastLogin(ctx, u.ID)

	return &domain.TokenPair{
		AccessToken:  access,
		RefreshToken: rawRefresh,
		ExpiresIn:    ttl,
	}, u, nil
}

// Refresh обновляет пару токенов, используя валидный refresh-токен.
func (s *Service) Refresh(ctx context.Context, rawRefresh string, meta domain.SessionMeta) (*domain.TokenPair, error) {
	newRaw, newHash, err := NewRefreshToken()
	if err != nil {
		return nil, err
	}

	// ВАЖНО: используем auth.HashRefreshToken
	userID, role, err := s.store.RotateRefreshToken(ctx, HashRefreshToken(rawRefresh), newHash, time.Now().Add(s.refreshTTL), meta)
	if err != nil {
		return nil, err
	}

	access, ttl, err := s.tokens.NewAccessToken(userID, role)
	if err != nil {
		return nil, err
	}

	return &domain.TokenPair{
		AccessToken:  access,
		RefreshToken: newRaw,
		ExpiresIn:    ttl,
	}, nil
}

// Logout отзывает всю семью refresh-токенов.
func (s *Service) Logout(ctx context.Context, rawRefresh string) error {
	// ВАЖНО: используем auth.HashRefreshToken
	return s.store.RevokeFamilyByHash(ctx, HashRefreshToken(rawRefresh))
}

// ChangePassword меняет пароль пользователя и отзывает ВСЕ его сессии.
func (s *Service) ChangePassword(ctx context.Context, userID, currentPassword, newPassword string) error {
	// 1. Получаем пользователя, чтобы проверить текущий пароль
	u, err := s.store.UserByID(ctx, userID)
	if err != nil {
		return err
	}

	// 2. Проверяем текущий пароль
	if err := s.hasher.Compare(u.PasswordHash, currentPassword); err != nil {
		return domain.ErrInvalidCredentials
	}

	// 3. Валидируем новый пароль
	if n := len(newPassword); n < 8 || n > 72 {
		return fmt.Errorf("%w: длина пароля должна быть от 8 до 72 байт", domain.ErrValidation)
	}

	// 4. Хешируем новый пароль
	newHash, err := s.hasher.Hash(newPassword)
	if err != nil {
		return err
	}

	// 5. Обновляем пароль в БД
	if err := s.store.UpdatePassword(ctx, userID, newHash); err != nil {
		return err
	}

	// 6. КРИТИЧЕСКИ ВАЖНО: Отзываем все активные сессии (refresh-токены)
	return s.store.RevokeAllUserRefreshTokens(ctx, userID)
}

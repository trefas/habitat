package domain

import (
	"errors"
	"strings"
	"time"
)

const (
	RoleUser  = "user"
	RoleAdmin = "admin"

	StatusPending = "pending"
	StatusActive  = "active"
	StatusBlocked = "blocked"
)

type User struct {
	ID           string
	Email        string
	PasswordHash string
	DisplayName  string
	Role         string
	Status       string
	CreatedAt    time.Time
	UpdatedAt    time.Time
	LastLoginAt  *time.Time
	DeletedAt    *time.Time
}

// UserFilter описывает параметры выборки пользователей для администратора.
type UserFilter struct {
	Search string
	Role   string
	Status string
	Limit  int
	Offset int
}

// UserPatch содержит поля пользователя, которые разрешено изменить.
// nil означает «поле не передано и менять его не нужно».
type UserPatch struct {
	DisplayName *string
	Role        *string
	Status      *string
}

type SessionMeta struct {
	UserAgent string
	IP        string
}

type TokenPair struct {
	AccessToken  string
	RefreshToken string
	ExpiresIn    int64 // в секундах
}

var (
	ErrNotFound           = errors.New("not found")
	ErrValidation         = errors.New("validation error")
	ErrEmailTaken         = errors.New("email already taken")
	ErrInvalidCredentials = errors.New("invalid credentials")
	ErrInvalidRefresh     = errors.New("invalid refresh token")
	ErrRefreshReused      = errors.New("refresh token reuse detected")
	ErrUserBlocked        = errors.New("user is not active")
	ErrRefreshExpired     = errors.New("refresh token expired")
	ErrLastAdmin          = errors.New("cannot remove the last active admin")
	ErrForbidden          = errors.New("forbidden")
)

// ValidRole сообщает, допустима ли роль пользователя.
func ValidRole(role string) bool {
	return role == RoleUser || role == RoleAdmin
}

// ValidStatus сообщает, допустим ли статус пользователя.
func ValidStatus(status string) bool {
	return status == StatusPending || status == StatusActive || status == StatusBlocked
}

// IsUUID проверяет строку на соответствие каноническому формату UUID (8-4-4-4-12).
func IsUUID(s string) bool {
	if len(s) != 36 {
		return false
	}
	for i, c := range s {
		switch i {
		case 8, 13, 18, 23:
			if c != '-' {
				return false
			}
		default:
			isHex := (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')
			if !isHex {
				return false
			}
		}
	}
	return true
}

// NormalizeEmail приводит email к нижнему регистру и убирает пробелы по краям.
func NormalizeEmail(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

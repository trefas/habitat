package domain

import (
	"errors"
	"strings"
	"time"
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
)

// NormalizeEmail приводит email к нижнему регистру и убирает пробелы по краям.
func NormalizeEmail(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

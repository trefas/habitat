package auth

import (
	"encoding/base64"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/require"
)

func TestParseAccess_RejectsAlgNone(t *testing.T) {
	tm := NewTokenManager(strings.Repeat("k", 32), time.Minute)

	enc := base64.RawURLEncoding.EncodeToString
	header := enc([]byte(`{"alg":"none","typ":"JWT"}`))
	payload := enc(fmt.Appendf(nil, `{"sub":"123","role":"admin","iss":"habitat-api","exp":%d}`,
		time.Now().Add(time.Hour).Unix()))

	fakeToken := header + "." + payload + "."

	_, err := tm.ParseAccess(fakeToken)
	require.Error(t, err, "токен с alg=none должен быть категорически отклонен")
}

func TestParseAccess_RejectsExpired(t *testing.T) {
	// Создаем менеджер с отрицательным TTL, чтобы токен сразу считался истёкшим
	tm := NewTokenManager(strings.Repeat("k", 32), -time.Minute)

	tok, _, err := tm.NewAccessToken("user-123", "user")
	require.NoError(t, err)

	_, err = tm.ParseAccess(tok)
	require.Error(t, err, "истёкший токен должен быть отклонен")

	// Лучшая практика в Go: проверяем тип ошибки, а не текст
	require.ErrorIs(t, err, jwt.ErrTokenExpired, "ошибка должна быть именно jwt.ErrTokenExpired")
}

func TestParseAccess_RejectsWrongIssuer(t *testing.T) {
	tm := NewTokenManager(strings.Repeat("k", 32), time.Minute)

	now := time.Now()
	claims := Claims{
		Role: "user",
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   "123",
			Issuer:    "evil-api", // Неправильный издатель
			ExpiresAt: jwt.NewNumericDate(now.Add(time.Hour)),
		},
	}
	tok, _ := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(strings.Repeat("k", 32)))

	_, err := tm.ParseAccess(tok)
	require.Error(t, err, "токен с неправильным iss должен быть отклонен")
}

func TestNewRefreshToken(t *testing.T) {
	raw, hash, err := NewRefreshToken()
	require.NoError(t, err)
	require.NotEmpty(t, raw, "raw токен не должен быть пустым")
	require.Len(t, hash, 32, "хеш должен быть ровно 32 байта (SHA-256)")
}

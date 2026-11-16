package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// Claims расширяет стандартные claims JWT, добавляя роль пользователя.
type Claims struct {
	Role string `json:"role"`
	jwt.RegisteredClaims
}

// TokenManager отвечает за создание и валидацию JWT.
type TokenManager struct {
	secret    []byte
	accessTTL time.Duration
	issuer    string
}

// NewTokenManager создает новый экземпляр TokenManager.
func NewTokenManager(secret string, ttl time.Duration) TokenManager {
	return TokenManager{
		secret:    []byte(secret),
		accessTTL: ttl,
		issuer:    "habitat-api",
	}
}

// NewAccessToken генерирует новый JWT access-токен.
func (m TokenManager) NewAccessToken(userID, role string) (string, int64, error) {
	now := time.Now()

	// Генерируем уникальный jti (JWT ID) для аудита
	jtiBytes := make([]byte, 16)
	if _, err := rand.Read(jtiBytes); err != nil {
		return "", 0, err
	}

	claims := Claims{
		Role: role,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   userID,
			Issuer:    m.issuer,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(m.accessTTL)),
			ID:        base64.RawURLEncoding.EncodeToString(jtiBytes),
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signedString, err := token.SignedString(m.secret)
	return signedString, int64(m.accessTTL.Seconds()), err
}

// ParseAccess проверяет и парсит JWT access-токен.
func (m TokenManager) ParseAccess(raw string) (*Claims, error) {
	var claims Claims

	// Ключевые проверки безопасности при парсинге:
	_, err := jwt.ParseWithClaims(raw, &claims, func(*jwt.Token) (any, error) {
		return m.secret, nil
	},
		jwt.WithValidMethods([]string{"HS256"}), // ЗАПРЕЩАЕМ alg: none и другие алгоритмы
		jwt.WithIssuer(m.issuer),                // Проверяем издателя
		jwt.WithExpirationRequired(),            // Требовать наличие exp
	)

	if err != nil {
		return nil, err
	}
	return &claims, nil
}

// NewRefreshToken генерирует криптографически стойкий refresh-токен и его SHA-256 хеш.
// Сам токен возвращается только один раз (клиенту), в БД сохраняется только hash.
func NewRefreshToken() (raw string, hash []byte, err error) {
	b := make([]byte, 32) // 256 бит энтропии
	if _, err := rand.Read(b); err != nil {
		return "", nil, err
	}

	raw = base64.RawURLEncoding.EncodeToString(b)
	sum := sha256.Sum256([]byte(raw))
	return raw, sum[:], nil
}

// HashRefreshToken вычисляет SHA-256 хеш от сырого refresh-токена.
func HashRefreshToken(raw string) []byte {
	sum := sha256.Sum256([]byte(raw))
	return sum[:]
}

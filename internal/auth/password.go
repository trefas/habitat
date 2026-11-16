package auth

import (
	"golang.org/x/crypto/bcrypt"
)

// PasswordHasher определяет интерфейс для хеширования и проверки паролей.
type PasswordHasher interface {
	Hash(plain string) (string, error)
	Compare(hash, plain string) error
}

// BcryptHasher реализует PasswordHasher с использованием bcrypt.
type BcryptHasher struct {
	Cost int
}

// Hash генерирует bcrypt-хеш из открытого текста.
func (h BcryptHasher) Hash(plain string) (string, error) {
	// bcrypt.GenerateFromPassword возвращает []byte, мы конвертируем в string для удобства хранения
	b, err := bcrypt.GenerateFromPassword([]byte(plain), h.Cost)
	return string(b), err
}

// Compare сравнивает хеш и открытый текст. Возвращает ошибку, если они не совпадают.
func (h BcryptHasher) Compare(hash, plain string) error {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(plain))
}

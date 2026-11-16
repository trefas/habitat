package auth

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBcryptHasher(t *testing.T) {
	// Используем Cost=4 для быстрых unit-тестов. В продакшене (config) будет 12.
	h := BcryptHasher{Cost: 4}
	pwd := "my_super_secret_password"

	// 1. Хешируем один и тот же пароль дважды
	hash1, err := h.Hash(pwd)
	require.NoError(t, err)

	hash2, err := h.Hash(pwd)
	require.NoError(t, err)

	// 2. Проверяем, что хеши разные (благодаря случайной соли)
	require.NotEqual(t, hash1, hash2, "Хеши одного пароля должны отличаться из-за соли")

	// 3. Проверяем успешное сравнение
	require.NoError(t, h.Compare(hash1, pwd), "Верный пароль должен проходить проверку")

	// 4. Проверяем отказ при неверном пароле
	require.Error(t, h.Compare(hash1, "wrong_password"), "Неверный пароль должен вызывать ошибку")
}

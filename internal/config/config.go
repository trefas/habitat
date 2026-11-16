package config

import (
	"fmt"
	"log"
	"os"
	"strings"
)

// Config хранит всю конфигурацию приложения
type Config struct {
	DBHost     string
	DBUser     string
	DBName     string
	DBPort     string
	DBPassword string
}

// Load читает конфигурацию из окружения
func Load() (*Config, error) {
	cfg := &Config{
		DBHost:     getEnv("DB_HOST", "localhost"),
		DBUser:     getEnv("DB_USER", "habitat"),
		DBName:     getEnv("DB_NAME", "habitat"),
		DBPort:     getEnv("DB_PORT", "5432"),
		DBPassword: getEnv("DB_PASSWORD", ""), // ← дефолт пустой, как в оригинале
	}

	// Читаем пароль из файла (Kubernetes/Podman secrets)
	if secretPath := getEnv("DB_PASSWORD_FILE", ""); secretPath != "" {
		if pwd, err := readSecret(secretPath); err == nil {
			cfg.DBPassword = pwd
		} else {
			log.Printf("Warning: could not read secret file: %v", err)
		}
	}

	return cfg, nil
}

// DSN возвращает строку подключения к БД
func (c *Config) DSN() string {
	return fmt.Sprintf("postgres://%s:%s@%s:%s/%s?sslmode=disable",
		c.DBUser, c.DBPassword, c.DBHost, c.DBPort, c.DBName)
}

// readSecret читает секрет из файла
func readSecret(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(data)), nil
}

// getEnv читает переменную окружения с дефолтом
func getEnv(key, defaultValue string) string {
	if value, exists := os.LookupEnv(key); exists {
		return value
	}
	return defaultValue
}

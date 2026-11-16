package config

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	DBHost     string
	DBUser     string
	DBName     string
	DBPort     string
	DBPassword string

	MigrationsPath  string
	HTTPAddr        string
	JWTSecret       string
	AccessTokenTTL  time.Duration
	RefreshTokenTTL time.Duration
	BcryptCost      int
}

func Load() (*Config, error) {
	cfg := &Config{
		DBHost:          getEnv("DB_HOST", "localhost"),
		DBUser:          getEnv("DB_USER", "habitat"),
		DBName:          getEnv("DB_NAME", "habitat"),
		DBPort:          getEnv("DB_PORT", "5432"),
		MigrationsPath:  getEnv("MIGRATIONS_PATH", "file:///app/migrations"),
		HTTPAddr:        getEnv("HTTP_ADDR", ":8080"),
		AccessTokenTTL:  getDurationEnv("ACCESS_TOKEN_TTL", 15*time.Minute),
		RefreshTokenTTL: getDurationEnv("REFRESH_TOKEN_TTL", 720*time.Hour),
		BcryptCost:      getIntEnv("BCRYPT_COST", 12),
	}

	// Читаем пароль БД
	cfg.DBPassword = getEnv("DB_PASSWORD", "")
	if secretPath := getEnv("DB_PASSWORD_FILE", ""); secretPath != "" {
		if pwd, err := readSecret(secretPath); err == nil {
			cfg.DBPassword = pwd
		} else {
			return nil, fmt.Errorf("чтение секрета БД: %w", err)
		}
	}

	// Читаем JWT Secret
	cfg.JWTSecret = getEnv("JWT_SECRET", "")
	if secretPath := getEnv("JWT_SECRET_FILE", ""); secretPath != "" {
		if secret, err := readSecret(secretPath); err == nil {
			cfg.JWTSecret = secret
		} else {
			return nil, fmt.Errorf("чтение секрета JWT: %w", err)
		}
	}

	// Валидация
	if cfg.DBPassword == "" {
		return nil, errors.New("DB_PASSWORD или DB_PASSWORD_FILE обязательны")
	}
	if len(cfg.JWTSecret) < 32 {
		return nil, errors.New("JWT_SECRET должен быть минимум 32 символа")
	}
	if cfg.BcryptCost < 10 || cfg.BcryptCost > 14 {
		return nil, errors.New("BCRYPT_COST должен быть от 10 до 14")
	}

	return cfg, nil
}

// DSN безопасно экранирует пароль для URL
func (c *Config) DSN() string {
	u := &url.URL{
		Scheme:   "postgres",
		User:     url.UserPassword(c.DBUser, c.DBPassword),
		Host:     net.JoinHostPort(c.DBHost, c.DBPort),
		Path:     "/" + c.DBName,
		RawQuery: url.Values{"sslmode": []string{"disable"}}.Encode(),
	}
	return u.String()
}

// readSecret читает секрет из файла и удаляет все лишние пробельные символы по краям
func readSecret(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(data)), nil
}

func getEnv(key, defaultValue string) string {
	if value, exists := os.LookupEnv(key); exists {
		return value
	}
	return defaultValue
}

func getDurationEnv(key string, defaultValue time.Duration) time.Duration {
	if value, exists := os.LookupEnv(key); exists {
		if d, err := time.ParseDuration(value); err == nil {
			return d
		}
	}
	return defaultValue
}

func getIntEnv(key string, defaultValue int) int {
	if value, exists := os.LookupEnv(key); exists {
		if i, err := strconv.Atoi(value); err == nil {
			return i
		}
	}
	return defaultValue
}

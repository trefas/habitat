package config

import (
	"os"
	"path/filepath"
	"testing"
)

// TestGetEnv проверяет функцию getEnv
func TestGetEnv(t *testing.T) {
	os.Setenv("TEST_VAR", "test_value")
	defer os.Unsetenv("TEST_VAR")

	tests := []struct {
		name         string
		key          string
		defaultValue string
		expected     string
	}{
		{
			name:         "Variable exists",
			key:          "TEST_VAR",
			defaultValue: "default",
			expected:     "test_value",
		},
		{
			name:         "Variable does not exist",
			key:          "NON_EXISTENT_VAR",
			defaultValue: "default_value",
			expected:     "default_value",
		},
		{
			name:         "Empty default value",
			key:          "ANOTHER_MISSING_VAR",
			defaultValue: "",
			expected:     "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := getEnv(tt.key, tt.defaultValue)
			if result != tt.expected {
				t.Errorf("getEnv(%s, %s) = %s; want %s", tt.key, tt.defaultValue, result, tt.expected)
			}
		})
	}
}

// TestReadSecret проверяет функцию readSecret
func TestReadSecret(t *testing.T) {
	tmpDir := t.TempDir()
	secretFile := filepath.Join(tmpDir, "test_secret")

	err := os.WriteFile(secretFile, []byte("  my_secret_password\n  "), 0644)
	if err != nil {
		t.Fatalf("Failed to create test secret file: %v", err)
	}

	result, err := readSecret(secretFile)
	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}

	expected := "my_secret_password"
	if result != expected {
		t.Errorf("Expected '%s', got '%s'", expected, result)
	}

	_, err = readSecret("/non/existent/file")
	if err == nil {
		t.Error("Expected error for non-existent file, got nil")
	}
}
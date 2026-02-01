// Package config provides environment variable loading for the RFP platform.
package config

import (
	"os"
	"strconv"

	"github.com/joho/godotenv"
)

// Config holds all configuration for the RFP platform services.
type Config struct {
	// Database
	DatabaseURL string

	// Gemini API
	GeminiAPIKey string

	// R2 Storage
	R2AccountID       string
	R2AccessKeyID     string
	R2SecretAccessKey string
	R2Bucket          string

	// Client App
	SessionSecret string
	BaseURL       string

	// Email
	SMTPHost string
	SMTPPort int
	SMTPUser string
	SMTPPass string
	SMTPFrom string

	// Optional
	LogLevel string
}

// Load reads configuration from environment variables.
// It attempts to load a .env file first (for local development).
func Load() *Config {
	// Load .env file if present (ignore errors - file may not exist in production)
	_ = godotenv.Load()

	return &Config{
		DatabaseURL:       getEnv("DATABASE_URL", ""),
		GeminiAPIKey:      getEnv("GEMINI_API_KEY", ""),
		R2AccountID:       getEnv("R2_ACCOUNT_ID", ""),
		R2AccessKeyID:     getEnv("R2_ACCESS_KEY_ID", ""),
		R2SecretAccessKey: getEnv("R2_SECRET_ACCESS_KEY", ""),
		R2Bucket:          getEnv("R2_BUCKET", "rfp-documents"),
		SessionSecret:     getEnv("SESSION_SECRET", ""),
		BaseURL:           getEnv("BASE_URL", "http://localhost:8080"),
		SMTPHost:          getEnv("SMTP_HOST", ""),
		SMTPPort:          getEnvInt("SMTP_PORT", 587),
		SMTPUser:          getEnv("SMTP_USER", ""),
		SMTPPass:          getEnv("SMTP_PASS", ""),
		SMTPFrom:          getEnv("SMTP_FROM", ""),
		LogLevel:          getEnv("LOG_LEVEL", "info"),
	}
}

func getEnv(key, defaultVal string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return defaultVal
}

func getEnvInt(key string, defaultVal int) int {
	if val := os.Getenv(key); val != "" {
		if i, err := strconv.Atoi(val); err == nil {
			return i
		}
	}
	return defaultVal
}

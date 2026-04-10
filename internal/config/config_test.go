package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestLoadFromEnvironment_SMTP(t *testing.T) {
	t.Setenv("SMTP_HOST", "mail.example.com")
	t.Setenv("SMTP_PORT", "2525")
	t.Setenv("SMTP_USERNAME", "user")
	t.Setenv("SMTP_PASSWORD", "pass")
	t.Setenv("SMTP_FROM", "noreply@example.com")

	cfg := Default()
	cfg.LoadFromEnvironment()

	assert.Equal(t, "mail.example.com", cfg.SMTP.Host)
	assert.Equal(t, "2525", cfg.SMTP.Port)
	assert.Equal(t, "user", cfg.SMTP.Username)
	assert.Equal(t, "pass", cfg.SMTP.Password)
	assert.Equal(t, "noreply@example.com", cfg.SMTP.From)
}

func TestLoadFromEnvironment_SMTP_NotSet(t *testing.T) {
	cfg := Default()
	cfg.LoadFromEnvironment()

	assert.Empty(t, cfg.SMTP.Host)
	assert.Empty(t, cfg.SMTP.Port)
}

package email

import (
	"context"
	"testing"

	"weos/internal/config"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewSMTPSender_EmptyHost_ReturnsNil(t *testing.T) {
	sender := NewSMTPSender(config.SMTPConfig{})
	assert.Nil(t, sender)
}

func TestNewSMTPSender_WithHost_ReturnsSender(t *testing.T) {
	sender := NewSMTPSender(config.SMTPConfig{
		Host: "smtp.example.com",
		From: "test@example.com",
	})
	require.NotNil(t, sender)
	assert.True(t, sender.Configured())
	assert.Equal(t, "587", sender.port, "should default port to 587")
}

func TestNewSMTPSender_CustomPort(t *testing.T) {
	sender := NewSMTPSender(config.SMTPConfig{
		Host: "smtp.example.com",
		Port: "465",
	})
	require.NotNil(t, sender)
	assert.Equal(t, "465", sender.port)
}

type testLogger struct {
	warnings []string
}

func (l *testLogger) Info(_ context.Context, _ string, _ ...interface{})  {}
func (l *testLogger) Error(_ context.Context, _ string, _ ...interface{}) {}
func (l *testLogger) Warn(_ context.Context, msg string, _ ...interface{}) {
	l.warnings = append(l.warnings, msg)
}

func TestLogSender_NotConfigured(t *testing.T) {
	logger := &testLogger{}
	sender := &LogSender{logger: logger}
	assert.False(t, sender.Configured())
}

func TestLogSender_SendLogsWarning(t *testing.T) {
	logger := &testLogger{}
	sender := &LogSender{logger: logger}
	err := sender.Send(context.Background(), "user@example.com", "Hello", "body")
	require.NoError(t, err)
	require.Len(t, logger.warnings, 1)
	assert.Contains(t, logger.warnings[0], "SMTP not configured")
}

func TestProvideEmailSender_NoSMTP_ReturnsLogSender(t *testing.T) {
	logger := &testLogger{}
	cfg := config.Config{}
	sender := ProvideEmailSender(cfg, logger)
	assert.False(t, sender.Configured())
}

func TestProvideEmailSender_WithSMTP_ReturnsSMTPSender(t *testing.T) {
	logger := &testLogger{}
	cfg := config.Config{
		SMTP: config.SMTPConfig{
			Host: "smtp.example.com",
			From: "noreply@example.com",
		},
	}
	sender := ProvideEmailSender(cfg, logger)
	assert.True(t, sender.Configured())
}

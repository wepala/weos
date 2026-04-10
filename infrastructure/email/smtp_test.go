package email

import (
	"context"
	"testing"

	"weos/internal/config"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewSMTPSender_EmptyHost_ReturnsNil(t *testing.T) {
	sender := NewSMTPSender(config.SMTPConfig{From: "a@b.com"})
	assert.Nil(t, sender)
}

func TestNewSMTPSender_EmptyFrom_ReturnsNil(t *testing.T) {
	sender := NewSMTPSender(config.SMTPConfig{Host: "smtp.example.com"})
	assert.Nil(t, sender)
}

func TestNewSMTPSender_InvalidFromAddress_ReturnsNil(t *testing.T) {
	sender := NewSMTPSender(config.SMTPConfig{
		Host: "smtp.example.com",
		From: "not-an-email",
	})
	assert.Nil(t, sender)
}

func TestNewSMTPSender_FromWithCRLF_ReturnsNil(t *testing.T) {
	sender := NewSMTPSender(config.SMTPConfig{
		Host: "smtp.example.com",
		From: "evil@example.com\r\nBcc: spy@evil.com",
	})
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
	assert.Equal(t, "test@example.com", sender.envelopeFrom)
}

func TestNewSMTPSender_DisplayNameFrom(t *testing.T) {
	sender := NewSMTPSender(config.SMTPConfig{
		Host: "smtp.example.com",
		From: "WeOS Admin <admin@example.com>",
	})
	require.NotNil(t, sender)
	assert.Equal(t, "admin@example.com", sender.envelopeFrom)
	assert.Contains(t, sender.headerFrom, "admin@example.com")
}

func TestNewSMTPSender_CustomPort(t *testing.T) {
	sender := NewSMTPSender(config.SMTPConfig{
		Host: "smtp.example.com",
		Port: "2525",
		From: "test@example.com",
	})
	require.NotNil(t, sender)
	assert.Equal(t, "2525", sender.port)
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

func TestSMTPSender_Send_RejectsInvalidTo(t *testing.T) {
	sender := NewSMTPSender(config.SMTPConfig{
		Host: "smtp.example.com",
		From: "test@example.com",
	})
	require.NotNil(t, sender)
	err := sender.Send(context.Background(), "not-an-address", "Subject", "body")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid recipient address")
}

func TestSMTPSender_Send_RejectsCRLFInTo(t *testing.T) {
	sender := NewSMTPSender(config.SMTPConfig{
		Host: "smtp.example.com",
		From: "test@example.com",
	})
	require.NotNil(t, sender)
	err := sender.Send(context.Background(), "evil@example.com\r\nBcc: spy@evil.com", "Subject", "body")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid recipient address")
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

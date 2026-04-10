package email

import (
	"context"
	"testing"

	"weos/internal/config"
)

func TestNewSMTPSender_EmptyHost_ReturnsNil(t *testing.T) {
	sender := NewSMTPSender(config.SMTPConfig{From: "a@b.com"})
	if sender != nil {
		t.Fatal("expected nil sender when Host is empty")
	}
}

func TestNewSMTPSender_EmptyFrom_ReturnsNil(t *testing.T) {
	sender := NewSMTPSender(config.SMTPConfig{Host: "smtp.example.com"})
	if sender != nil {
		t.Fatal("expected nil sender when From is empty")
	}
}

func TestNewSMTPSender_InvalidFromAddress_ReturnsNil(t *testing.T) {
	sender := NewSMTPSender(config.SMTPConfig{
		Host: "smtp.example.com",
		From: "not-an-email",
	})
	if sender != nil {
		t.Fatal("expected nil sender for invalid From address")
	}
}

func TestNewSMTPSender_FromWithCRLF_ReturnsNil(t *testing.T) {
	sender := NewSMTPSender(config.SMTPConfig{
		Host: "smtp.example.com",
		From: "evil@example.com\r\nBcc: spy@evil.com",
	})
	if sender != nil {
		t.Fatal("expected nil sender for From with CRLF")
	}
}

func TestNewSMTPSender_WithHost_ReturnsSender(t *testing.T) {
	sender := NewSMTPSender(config.SMTPConfig{
		Host: "smtp.example.com",
		From: "test@example.com",
	})
	if sender == nil {
		t.Fatal("expected non-nil sender")
	}
	if !sender.Configured() {
		t.Fatal("expected Configured() to return true")
	}
	if sender.port != "587" {
		t.Fatalf("expected default port 587, got %s", sender.port)
	}
	if sender.envelopeFrom != "test@example.com" {
		t.Fatalf("expected envelopeFrom test@example.com, got %s", sender.envelopeFrom)
	}
}

func TestNewSMTPSender_DisplayNameFrom(t *testing.T) {
	sender := NewSMTPSender(config.SMTPConfig{
		Host: "smtp.example.com",
		From: "WeOS Admin <admin@example.com>",
	})
	if sender == nil {
		t.Fatal("expected non-nil sender")
	}
	if sender.envelopeFrom != "admin@example.com" {
		t.Fatalf("expected envelopeFrom admin@example.com, got %s", sender.envelopeFrom)
	}
}

func TestNewSMTPSender_CustomPort(t *testing.T) {
	sender := NewSMTPSender(config.SMTPConfig{
		Host: "smtp.example.com",
		Port: "2525",
		From: "test@example.com",
	})
	if sender == nil {
		t.Fatal("expected non-nil sender")
	}
	if sender.port != "2525" {
		t.Fatalf("expected port 2525, got %s", sender.port)
	}
}

func TestNewSMTPSender_Port465_ReturnsNil(t *testing.T) {
	sender := NewSMTPSender(config.SMTPConfig{
		Host: "smtp.example.com",
		Port: "465",
		From: "test@example.com",
	})
	if sender != nil {
		t.Fatal("expected nil sender for port 465 (implicit TLS not supported)")
	}
}

func TestNewSMTPSender_HostWithPort_ReturnsNil(t *testing.T) {
	sender := NewSMTPSender(config.SMTPConfig{
		Host: "smtp.example.com:587",
		From: "test@example.com",
	})
	if sender != nil {
		t.Fatal("expected nil sender when Host contains a port")
	}
}

func TestNewSMTPSender_InvalidPort_ReturnsNil(t *testing.T) {
	for _, port := range []string{"abc", "0", "99999", "-1"} {
		sender := NewSMTPSender(config.SMTPConfig{
			Host: "smtp.example.com",
			Port: port,
			From: "test@example.com",
		})
		if sender != nil {
			t.Fatalf("expected nil sender for invalid port %q", port)
		}
	}
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
	if sender.Configured() {
		t.Fatal("expected Configured() to return false")
	}
}

func TestLogSender_SendLogsWarning(t *testing.T) {
	logger := &testLogger{}
	sender := &LogSender{logger: logger}
	err := sender.Send(context.Background(), "user@example.com", "Hello", "body")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(logger.warnings) != 1 {
		t.Fatalf("expected 1 warning, got %d", len(logger.warnings))
	}
	if logger.warnings[0] != "email not sent: SMTP not configured" {
		t.Fatalf("unexpected warning: %s", logger.warnings[0])
	}
}

func TestSMTPSender_Send_RejectsInvalidTo(t *testing.T) {
	sender := NewSMTPSender(config.SMTPConfig{
		Host: "smtp.example.com",
		From: "test@example.com",
	})
	if sender == nil {
		t.Fatal("expected non-nil sender")
	}
	err := sender.Send(context.Background(), "not-an-address", "Subject", "body")
	if err == nil {
		t.Fatal("expected error for invalid To address")
	}
}

func TestSMTPSender_Send_RejectsCRLFInSubject(t *testing.T) {
	sender := NewSMTPSender(config.SMTPConfig{
		Host: "smtp.example.com",
		From: "test@example.com",
	})
	if sender == nil {
		t.Fatal("expected non-nil sender")
	}
	err := sender.Send(context.Background(), "user@example.com", "Hello\r\nBcc: spy@evil.com", "body")
	if err == nil {
		t.Fatal("expected error for subject with CRLF")
	}
}

func TestSMTPSender_Send_RejectsCRLFInTo(t *testing.T) {
	sender := NewSMTPSender(config.SMTPConfig{
		Host: "smtp.example.com",
		From: "test@example.com",
	})
	if sender == nil {
		t.Fatal("expected non-nil sender")
	}
	err := sender.Send(context.Background(), "evil@example.com\r\nBcc: spy@evil.com", "Subject", "body")
	if err == nil {
		t.Fatal("expected error for To with CRLF")
	}
}

func TestProvideEmailSender_NoSMTP_ReturnsLogSender(t *testing.T) {
	logger := &testLogger{}
	cfg := config.Config{}
	sender := ProvideEmailSender(cfg, logger)
	if sender.Configured() {
		t.Fatal("expected Configured() to return false when SMTP not set")
	}
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
	if !sender.Configured() {
		t.Fatal("expected Configured() to return true when SMTP is set")
	}
}

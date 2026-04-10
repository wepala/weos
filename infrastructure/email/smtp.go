// Copyright (C) 2026 Wepala, LLC
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with this program.  If not, see <https://www.gnu.org/licenses/>.

package email

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/mail"
	"net/smtp"
	"strings"
	"time"

	"weos/domain/entities"
	"weos/internal/config"
)

const (
	defaultPort       = "587"
	dialTimeout       = 30 * time.Second
	readWriteDeadline = 60 * time.Second
)

// SMTPSender sends email via an SMTP server.
type SMTPSender struct {
	host     string
	port     string
	username string
	password string
	from     string
}

// NewSMTPSender creates a sender from the SMTP config section.
// Returns a configured sender when both Host and From are set and From is a
// valid email address, or nil otherwise.
func NewSMTPSender(cfg config.SMTPConfig) *SMTPSender {
	if cfg.Host == "" || cfg.From == "" {
		return nil
	}
	if _, err := mail.ParseAddress(cfg.From); err != nil {
		return nil
	}
	port := cfg.Port
	if port == "" {
		port = defaultPort
	}
	return &SMTPSender{
		host:     cfg.Host,
		port:     port,
		username: cfg.Username,
		password: cfg.Password,
		from:     cfg.From,
	}
}

func (s *SMTPSender) Send(ctx context.Context, to, subject, body string) error {
	if strings.ContainsAny(to, "\r\n") || strings.ContainsAny(subject, "\r\n") {
		return fmt.Errorf("email header contains invalid characters")
	}

	msg := fmt.Sprintf("From: %s\r\nTo: %s\r\nSubject: %s\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=UTF-8\r\n\r\n%s",
		s.from, to, subject, body)

	addr := s.host + ":" + s.port

	return s.sendWithContext(ctx, addr, to, []byte(msg))
}

func (s *SMTPSender) sendWithContext(ctx context.Context, addr, to string, msg []byte) error {
	dialer := net.Dialer{Timeout: dialTimeout}
	conn, err := dialer.DialContext(ctx, "tcp", addr)
	if err != nil {
		return fmt.Errorf("smtp dial: %w", err)
	}

	deadline := time.Now().Add(readWriteDeadline)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	if err := conn.SetDeadline(deadline); err != nil {
		conn.Close()
		return fmt.Errorf("smtp set deadline: %w", err)
	}

	c, err := smtp.NewClient(conn, s.host)
	if err != nil {
		conn.Close()
		return fmt.Errorf("smtp new client: %w", err)
	}
	defer c.Close()

	// Attempt STARTTLS if supported.
	if ok, _ := c.Extension("STARTTLS"); ok {
		if err := c.StartTLS(&tls.Config{ServerName: s.host}); err != nil {
			return fmt.Errorf("smtp starttls: %w", err)
		}
	}

	if s.username != "" {
		auth := smtp.PlainAuth("", s.username, s.password, s.host)
		if err := c.Auth(auth); err != nil {
			return fmt.Errorf("smtp auth: %w", err)
		}
	}

	if err := c.Mail(s.from); err != nil {
		return fmt.Errorf("smtp mail from: %w", err)
	}
	if err := c.Rcpt(to); err != nil {
		return fmt.Errorf("smtp rcpt to: %w", err)
	}

	w, err := c.Data()
	if err != nil {
		return fmt.Errorf("smtp data: %w", err)
	}
	if _, err := w.Write(msg); err != nil {
		return fmt.Errorf("smtp write: %w", err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("smtp data close: %w", err)
	}

	return c.Quit()
}

func (s *SMTPSender) Configured() bool { return true }

// ProvideEmailSender returns an SMTPSender when SMTP is configured,
// or a LogSender that warns on each send attempt otherwise.
func ProvideEmailSender(cfg config.Config, logger entities.Logger) entities.EmailSender {
	if sender := NewSMTPSender(cfg.SMTP); sender != nil {
		return sender
	}
	return &LogSender{logger: logger}
}

// LogSender is a no-op sender that logs a warning when Send is called.
type LogSender struct {
	logger entities.Logger
}

func (l *LogSender) Send(ctx context.Context, to, subject, _ string) error {
	l.logger.Warn(ctx, "email not sent: SMTP not configured",
		"to", to, "subject", subject)
	return nil
}

func (l *LogSender) Configured() bool { return false }

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
	"fmt"
	"net/smtp"
	"strings"

	"weos/domain/entities"
	"weos/internal/config"
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
// Returns a configured sender when Host is set, or nil otherwise.
func NewSMTPSender(cfg config.SMTPConfig) *SMTPSender {
	if cfg.Host == "" || cfg.From == "" {
		return nil
	}
	port := cfg.Port
	if port == "" {
		port = "587"
	}
	return &SMTPSender{
		host:     cfg.Host,
		port:     port,
		username: cfg.Username,
		password: cfg.Password,
		from:     cfg.From,
	}
}

func (s *SMTPSender) Send(_ context.Context, to, subject, body string) error {
	if strings.ContainsAny(to, "\r\n") || strings.ContainsAny(subject, "\r\n") {
		return fmt.Errorf("email header contains invalid characters")
	}

	msg := fmt.Sprintf("From: %s\r\nTo: %s\r\nSubject: %s\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=UTF-8\r\n\r\n%s",
		s.from, to, subject, body)

	addr := s.host + ":" + s.port

	var auth smtp.Auth
	if s.username != "" {
		auth = smtp.PlainAuth("", s.username, s.password, s.host)
	}

	return smtp.SendMail(addr, auth, s.from, []string{to}, []byte(msg))
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

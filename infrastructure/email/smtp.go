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
	"mime"
	"net"
	"net/mail"
	"net/smtp"
	"strconv"
	"strings"
	"time"

	"weos/domain/entities"
	"weos/internal/config"
)

const (
	// defaultPort is the SMTP submission port (STARTTLS).
	// Implicit TLS (port 465) is not currently supported.
	defaultPort       = "587"
	dialTimeout       = 30 * time.Second
	readWriteDeadline = 60 * time.Second
)

// SMTPSender sends email via an SMTP server using STARTTLS.
//
// Only the STARTTLS flow (typically port 587) is supported. Implicit TLS
// (port 465 / SMTPS) is not implemented; configuring port 465 will fail
// because the server expects a TLS handshake immediately on connect.
//
// After the TCP connection is established, an I/O deadline (60 s or the
// context deadline, whichever is sooner) governs the remainder of the SMTP
// transaction. Context cancellation without a deadline will not interrupt
// an in-progress transaction until that ceiling is reached.
type SMTPSender struct {
	host         string
	port         string
	username     string
	password     string
	envelopeFrom string // bare email for SMTP MAIL FROM / RCPT TO
	headerFrom   string // formatted for the From: header (may include display name)
}

// NewSMTPSender creates a sender from the SMTP config section.
// Returns a configured sender when both Host and From are set and From is a
// valid email address, or nil otherwise.
func NewSMTPSender(cfg config.SMTPConfig) *SMTPSender {
	if cfg.Host == "" || cfg.From == "" {
		return nil
	}
	parsed, err := mail.ParseAddress(cfg.From)
	if err != nil {
		return nil
	}
	port := cfg.Port
	if port == "" {
		port = defaultPort
	}
	p, err := strconv.Atoi(port)
	if err != nil || p < 1 || p > 65535 || p == 465 {
		return nil // invalid port, or implicit TLS (465) which is not supported
	}
	return &SMTPSender{
		host:         cfg.Host,
		port:         port,
		username:     cfg.Username,
		password:     cfg.Password,
		envelopeFrom: parsed.Address,
		headerFrom:   parsed.String(),
	}
}

func (s *SMTPSender) Send(ctx context.Context, to, subject, body string) error {
	parsedTo, err := mail.ParseAddress(to)
	if err != nil {
		return fmt.Errorf("invalid recipient address: %w", err)
	}
	if strings.ContainsAny(subject, "\r\n") {
		return fmt.Errorf("email subject contains invalid characters")
	}

	// RFC 2047 Q-encode subject for non-ASCII safety.
	encodedSubject := mime.QEncoding.Encode("UTF-8", subject)

	msg := fmt.Sprintf("From: %s\r\nTo: %s\r\nSubject: %s\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=UTF-8\r\n\r\n%s",
		s.headerFrom, parsedTo.String(), encodedSubject, body)

	addr := net.JoinHostPort(s.host, s.port)

	return s.sendWithContext(ctx, addr, parsedTo.Address, []byte(msg))
}

func (s *SMTPSender) sendWithContext(ctx context.Context, addr, rcpt string, msg []byte) error {
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
		_ = conn.Close()
		return fmt.Errorf("smtp set deadline: %w", err)
	}

	c, err := smtp.NewClient(conn, s.host)
	if err != nil {
		_ = conn.Close()
		return fmt.Errorf("smtp new client: %w", err)
	}
	defer func() { _ = c.Close() }()

	// Require STARTTLS — fail closed if the server does not support it.
	if ok, _ := c.Extension("STARTTLS"); !ok {
		return fmt.Errorf("smtp starttls: server does not advertise STARTTLS")
	}
	if err := c.StartTLS(&tls.Config{ServerName: s.host}); err != nil {
		return fmt.Errorf("smtp starttls: %w", err)
	}

	if s.username != "" {
		auth := smtp.PlainAuth("", s.username, s.password, s.host)
		if err := c.Auth(auth); err != nil {
			return fmt.Errorf("smtp auth: %w", err)
		}
	}

	if err := c.Mail(s.envelopeFrom); err != nil {
		return fmt.Errorf("smtp mail from: %w", err)
	}
	if err := c.Rcpt(rcpt); err != nil {
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

func (l *LogSender) Send(ctx context.Context, _, _, _ string) error {
	l.logger.Warn(ctx, "email not sent: SMTP not configured")
	return nil
}

func (l *LogSender) Configured() bool { return false }

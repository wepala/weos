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

package application

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/wepala/weos/v3/domain/entities"
	"github.com/wepala/weos/v3/domain/repositories"

	"github.com/akeemphilbert/pericarp/pkg/auth"
)

// NotificationTypeSlug is the resource type slug for a notification. The
// notifications preset installs this type; the service and its consumers key
// off this one constant. Defined here (not in the preset package) so the
// application package can reference it without an import cycle.
const NotificationTypeSlug = "notification"

// notificationPageSize bounds each internal projection read when counting or
// bulk-marking. A dedicated projection COUNT would remove the need to page,
// but paging keeps unread-count and mark-all correct and driver-agnostic
// (SQLite and PostgreSQL) without a boolean-column filter.
const notificationPageSize = 100

// notificationMaxLimit caps a caller-supplied inbox page so a request cannot
// pull an unbounded number of rows in one call.
const notificationMaxLimit = 200

// occurredAtLayout stores occurredAt as a fixed-width UTC timestamp with a
// constant 9-digit fraction, so a lexical descending sort of the string column
// is a true chronological descending sort. time.RFC3339Nano trims trailing
// zeros (variable width), which would invert order for sub-second-apart
// notifications within the same second.
const occurredAtLayout = "2006-01-02T15:04:05.000000000Z07:00"

// NotificationInput is the payload a producing service supplies to emit a
// notification from a domain event. Recipient is the agent the notification is
// addressed to; DedupeKey is the stable key that makes production idempotent.
type NotificationInput struct {
	// Recipient MUST be the auth-resolved AgentID of the target user (the same
	// identity SoftAuth/JWT put on the request context). A notification is
	// owned by, and its inbox scoped to, this exact AgentID — any other value
	// (email, display name) makes the notification invisible to every inbox.
	Recipient   string
	AccountID   string // recipient's account, if the producer knows it
	Kind        string // free-form category, e.g. "import.completed"
	Title       string
	Body        string
	ActionURL   string // optional CTA link
	ActionLabel string // optional CTA label
	TaskRef     string // optional reference (URN/id) to a related resource
	OccurredAt  time.Time
	DedupeKey   string // stable per-signal key; empty disables idempotency
}

// NotificationView is the inbox-facing shape of a notification.
type NotificationView struct {
	ID          string `json:"id"`
	Recipient   string `json:"recipient"`
	Kind        string `json:"kind,omitempty"`
	Title       string `json:"title"`
	Body        string `json:"body,omitempty"`
	ActionURL   string `json:"actionUrl,omitempty"`
	ActionLabel string `json:"actionLabel,omitempty"`
	TaskRef     string `json:"taskRef,omitempty"`
	OccurredAt  string `json:"occurredAt,omitempty"`
	Read        bool   `json:"read"`
}

// NotificationService is the generic notification store + inbox. Services call
// Notify to emit a notification; the recipient's inbox operations (List,
// UnreadCount, MarkRead, MarkAllRead) act on the authenticated caller carried
// in ctx and are scoped to that user by the standard ownership model.
type NotificationService interface {
	// Notify emits a notification addressed to in.Recipient. For normal
	// sequential delivery it is idempotent on (Recipient, DedupeKey):
	// re-emitting the same signal returns the existing notification instead of
	// creating a duplicate. The guarantee is a check-then-act lookup, not a DB
	// uniqueness constraint (weos has no projection unique-index support), so
	// two truly concurrent emits of the same (Recipient, DedupeKey) can still
	// race to two rows; a future projection unique index would harden this.
	Notify(ctx context.Context, in NotificationInput) (*entities.Resource, error)
	// List returns the caller's notifications, newest first, up to limit.
	List(ctx context.Context, limit int) ([]NotificationView, error)
	// UnreadCount returns how many of the caller's notifications are unread.
	UnreadCount(ctx context.Context) (int, error)
	// MarkRead marks one of the caller's notifications read. It returns
	// entities.ErrAccessDenied if the notification is not the caller's.
	MarkRead(ctx context.Context, id string) (*NotificationView, error)
	// MarkAllRead marks every unread notification of the caller read and
	// returns how many were changed.
	MarkAllRead(ctx context.Context) (int, error)
}

// notificationData is the persisted, schema-shaped body of a notification.
type notificationData struct {
	Recipient   string `json:"recipient"`
	Kind        string `json:"kind,omitempty"`
	Title       string `json:"title"`
	Body        string `json:"body,omitempty"`
	ActionURL   string `json:"actionUrl,omitempty"`
	ActionLabel string `json:"actionLabel,omitempty"`
	TaskRef     string `json:"taskRef,omitempty"`
	OccurredAt  string `json:"occurredAt,omitempty"`
	Read        bool   `json:"read"`
	DedupeKey   string `json:"dedupeKey,omitempty"`
}

type notificationService struct {
	resources ResourceService
	logger    entities.Logger
}

// ProvideNotificationService wires the notification store on top of the
// ResourceService, so production and mark-read run through the full behavior /
// event-sourcing pipeline and inbox reads inherit the ownership visibility
// scope.
func ProvideNotificationService(rs ResourceService, logger entities.Logger) NotificationService {
	return &notificationService{resources: rs, logger: logger}
}

func (s *notificationService) Notify(
	ctx context.Context, in NotificationInput,
) (*entities.Resource, error) {
	if in.Recipient == "" {
		return nil, fmt.Errorf("notification recipient is required")
	}
	if in.Title == "" {
		return nil, fmt.Errorf("notification title is required")
	}

	storedKey := in.DedupeKey
	if storedKey != "" {
		// Namespace the stored key by recipient so idempotency is per-recipient
		// and the lookup needs no JSON-LD parse: any row with this composite key
		// is definitively this recipient's prior delivery.
		storedKey = dedupeKey(in.Recipient, in.DedupeKey)
		if existing, err := s.findByDedupeKey(ctx, storedKey); err != nil {
			s.logger.Error(ctx, "notification dedup lookup failed",
				"recipient", in.Recipient, "error", err)
			return nil, err
		} else if existing != nil {
			return existing, nil
		}
	}

	occurred := in.OccurredAt
	if occurred.IsZero() {
		occurred = time.Now()
	}
	data, err := json.Marshal(notificationData{
		Recipient:   in.Recipient,
		Kind:        in.Kind,
		Title:       in.Title,
		Body:        in.Body,
		ActionURL:   in.ActionURL,
		ActionLabel: in.ActionLabel,
		TaskRef:     in.TaskRef,
		OccurredAt:  occurred.UTC().Format(occurredAtLayout),
		Read:        false,
		DedupeKey:   storedKey,
	})
	if err != nil {
		return nil, fmt.Errorf("marshal notification: %w", err)
	}

	// A notification is owned by its recipient: creating it under the
	// recipient's identity makes the standard ownership visibility scope show
	// it only in that user's inbox and lets only that user mark it read — no
	// ACL bypass anywhere on the inbox path.
	ownerCtx := auth.ContextWithAgent(ctx, &auth.Identity{
		AgentID:         in.Recipient,
		ActiveAccountID: in.AccountID,
		AccountIDs:      []string{in.AccountID},
	})
	created, err := s.resources.Create(ownerCtx, CreateResourceCommand{
		TypeSlug: NotificationTypeSlug,
		Data:     data,
	})
	if err != nil {
		s.logger.Error(ctx, "failed to create notification",
			"recipient", in.Recipient, "kind", in.Kind, "error", err)
		return nil, fmt.Errorf("create notification: %w", err)
	}
	return created, nil
}

// findByDedupeKey looks up a prior notification by its stored (composite) key.
// ListByField is unscoped by design, so this finds the earlier delivery
// regardless of which identity (or none) is emitting the current signal.
func (s *notificationService) findByDedupeKey(
	ctx context.Context, storedKey string,
) (*entities.Resource, error) {
	page, err := s.resources.ListByField(ctx, NotificationTypeSlug, "dedupeKey", storedKey)
	if err != nil {
		return nil, fmt.Errorf("notification dedup lookup: %w", err)
	}
	if len(page.Data) > 0 {
		return page.Data[0], nil
	}
	return nil, nil
}

func (s *notificationService) List(
	ctx context.Context, limit int,
) ([]NotificationView, error) {
	switch {
	case limit <= 0:
		limit = notificationPageSize
	case limit > notificationMaxLimit:
		limit = notificationMaxLimit
	}
	page, err := s.resources.ListFlat(ctx, NotificationTypeSlug, "", limit, newestFirst())
	if err != nil {
		s.logger.Error(ctx, "failed to list notifications", "error", err)
		return nil, fmt.Errorf("list notifications: %w", err)
	}
	views := make([]NotificationView, 0, len(page.Data))
	for _, row := range page.Data {
		views = append(views, viewFromFlat(row))
	}
	return views, nil
}

func (s *notificationService) UnreadCount(ctx context.Context) (int, error) {
	count := 0
	err := s.forEachFlat(ctx, func(row map[string]any) error {
		if !flatBool(row, "read") {
			count++
		}
		return nil
	})
	return count, err
}

func (s *notificationService) MarkAllRead(ctx context.Context) (int, error) {
	var unreadIDs []string
	err := s.forEachFlat(ctx, func(row map[string]any) error {
		if !flatBool(row, "read") {
			unreadIDs = append(unreadIDs, flatString(row, "id"))
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	marked := 0
	for _, id := range unreadIDs {
		if _, err := s.MarkRead(ctx, id); err != nil {
			return marked, err
		}
		marked++
	}
	return marked, nil
}

func (s *notificationService) MarkRead(
	ctx context.Context, id string,
) (*NotificationView, error) {
	row, err := s.resources.GetFlat(ctx, NotificationTypeSlug, id)
	if err != nil {
		return nil, err
	}
	view := viewFromFlat(row)
	// A notification is personal to its recipient. GetFlat/Update alone would
	// let an account admin/owner read or mutate a member's notification via the
	// ResourceService admin bypass; refuse unless the caller IS the recipient.
	// This closes both the content read (the returned view) and the mutate,
	// and keeps the mark path consistent with the personal-only List path.
	caller := auth.AgentFromCtx(ctx)
	if caller == nil || view.Recipient != caller.AgentID {
		return nil, entities.ErrAccessDenied
	}
	if view.Read {
		return &view, nil // already read — idempotent
	}
	body, err := json.Marshal(dataFromFlat(row, true))
	if err != nil {
		return nil, fmt.Errorf("marshal notification update: %w", err)
	}
	if _, err := s.resources.Update(ctx, UpdateResourceCommand{ID: id, Data: body}); err != nil {
		s.logger.Error(ctx, "failed to mark notification read", "id", id, "error", err)
		return nil, err
	}
	view.Read = true
	return &view, nil
}

// forEachFlat pages through the caller's notifications (visibility-scoped) and
// invokes fn for each row.
func (s *notificationService) forEachFlat(
	ctx context.Context, fn func(row map[string]any) error,
) error {
	cursor := ""
	for {
		page, err := s.resources.ListFlat(ctx, NotificationTypeSlug, cursor, notificationPageSize, newestFirst())
		if err != nil {
			s.logger.Error(ctx, "failed to scan notifications", "error", err)
			return fmt.Errorf("scan notifications: %w", err)
		}
		for _, row := range page.Data {
			if err := fn(row); err != nil {
				return err
			}
		}
		if !page.HasMore || page.Cursor == "" {
			return nil
		}
		cursor = page.Cursor
	}
}

func newestFirst() repositories.SortOptions {
	return repositories.SortOptions{SortBy: "occurredAt", SortOrder: "desc"}
}

// dedupeKey namespaces a caller-supplied key by recipient.
func dedupeKey(recipient, key string) string {
	return recipient + "\x1f" + key // 0x1f = unit separator
}

func viewFromFlat(row map[string]any) NotificationView {
	return NotificationView{
		ID:          flatString(row, "id"),
		Recipient:   flatString(row, "recipient"),
		Kind:        flatString(row, "kind"),
		Title:       flatString(row, "title"),
		Body:        flatString(row, "body"),
		ActionURL:   flatString(row, "actionUrl"),
		ActionLabel: flatString(row, "actionLabel"),
		TaskRef:     flatString(row, "taskRef"),
		OccurredAt:  flatString(row, "occurredAt"),
		Read:        flatBool(row, "read"),
	}
}

func dataFromFlat(row map[string]any, read bool) notificationData {
	return notificationData{
		Recipient:   flatString(row, "recipient"),
		Kind:        flatString(row, "kind"),
		Title:       flatString(row, "title"),
		Body:        flatString(row, "body"),
		ActionURL:   flatString(row, "actionUrl"),
		ActionLabel: flatString(row, "actionLabel"),
		TaskRef:     flatString(row, "taskRef"),
		OccurredAt:  flatString(row, "occurredAt"),
		Read:        read,
		DedupeKey:   flatString(row, "dedupeKey"),
	}
}

// flatString reads a projection column as a string across driver
// representations (SQLite/PostgreSQL).
func flatString(row map[string]any, key string) string {
	v, ok := row[key]
	if !ok || v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return fmt.Sprint(v)
}

// flatBool reads a projection boolean column, tolerating the int64 (SQLite) and
// bool (PostgreSQL) shapes GORM scans a boolean column into.
func flatBool(row map[string]any, key string) bool {
	switch v := row[key].(type) {
	case bool:
		return v
	case int64:
		return v != 0
	case int:
		return v != 0
	case float64:
		return v != 0
	case string:
		return v == "true" || v == "1"
	default:
		return false
	}
}

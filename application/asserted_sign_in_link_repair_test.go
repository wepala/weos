package application

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/akeemphilbert/pericarp/pkg/auth/domain/entities"
	esinfra "github.com/akeemphilbert/pericarp/pkg/eventsourcing/infrastructure"
)

// storeCredentialRows deletes credential rows from the memory store, or fails
// every delete with err.
type storeCredentialRows struct {
	s       *memoryAuthStore
	err     error
	deleted []string
}

func (r *storeCredentialRows) DeleteCredentialRow(_ context.Context, id string) error {
	if r.err != nil {
		return r.err
	}
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	kept := make([]*entities.Credential, 0, len(r.s.creds))
	for _, c := range r.s.creds {
		if c.GetID() != id {
			kept = append(kept, c)
		}
	}
	r.s.creds = kept
	r.deleted = append(r.deleted, id)
	return nil
}

func requireLineField(t *testing.T, line signInLogLine, key string, want any) {
	t.Helper()
	if got := line.field(key); got != want {
		t.Fatalf("log line %q has %s = %v, want %v (fields %v)", line.msg, key, got, want, line.fields)
	}
}

// The event store fails after the linked credential's row is saved. The row is
// taken back, so no link exists that a projection rebuild would silently drop,
// and one ERROR line names the owner and the credential.
func TestAssertedSignInTakesBackALinkWhoseCreationCannotBeRecorded(t *testing.T) {
	s := newMemoryAuthStore()
	s.seedPerson(t, "agent-dana", "Dana Whitfield", "google", googleSub, "dana.whitfield@harborlegal.example")
	rows := &storeCredentialRows{s: s}
	logs := &signInLogs{}
	svc := newTestAssertedSignInWith(s, func(cfg *AssertedSignInConfig) {
		cfg.LinkByEmail = true
		cfg.EventStore = failingEventStore{esinfra.NewMemoryStore()}
		cfg.CredentialRows = rows
		cfg.Logger = logs
	})

	_, err := svc.SignIn(context.Background(), dana("apple", appleSub))
	if err == nil || errors.Is(err, ErrAmbiguousOwner) || errors.Is(err, ErrUnprovenOwner) {
		t.Fatalf("err = %v, want the event store failure", err)
	}
	if len(rows.deleted) != 1 {
		t.Fatalf("deleted %v, want the one linked credential", rows.deleted)
	}
	if left := s.credentialFor("apple", appleSub); left != nil {
		t.Fatalf("the linked credential's row %s is still in the store without its event", left.GetID())
	}
	if s.createCount() != 0 {
		t.Fatalf("a failed link fell through to creating a person")
	}
	lines := logs.all()
	if len(lines) != 1 || lines[0].level != "error" {
		t.Fatalf("expected one error line, got:\n%s", logs.text())
	}
	requireLineField(t, lines[0], "owner_agent_id", "agent-dana")
	requireLineField(t, lines[0], "credential_id", rows.deleted[0])
	requireLineField(t, lines[0], "row_deleted", true)
	requireNoRawIdentity(t, logs, appleSub, "dana.whitfield@harborlegal.example")

	// With the event store back, the person's next sign-in links again and
	// records the link's creation.
	events := esinfra.NewMemoryStore()
	retry := newTestAssertedSignInWith(s, func(cfg *AssertedSignInConfig) {
		cfg.LinkByEmail = true
		cfg.EventStore = events
		cfg.CredentialRows = rows
	})
	got, err := retry.SignIn(context.Background(), dana("apple", appleSub))
	if err != nil || got.NewAccount || got.Agent.GetID() != "agent-dana" {
		t.Fatalf("the retry: agent=%v new=%v err=%v, want a link to agent-dana", got.Agent, got.NewAccount, err)
	}
	linked := s.credentialFor("apple", appleSub)
	if ids := events.GetAllAggregateIDs(); linked == nil || len(ids) != 1 || ids[0] != linked.GetID() {
		t.Fatalf("the retry recorded events for %v, want only the linked credential", ids)
	}
}

// When the row cannot be taken back — the delete fails, or nothing is wired to
// delete it — the ERROR line is followed by a repair line naming both ids.
func TestAssertedSignInNamesALinkRowItCouldNotTakeBackForRepair(t *testing.T) {
	cases := map[string]func(s *memoryAuthStore) *storeCredentialRows{
		"the delete fails": func(s *memoryAuthStore) *storeCredentialRows {
			return &storeCredentialRows{s: s, err: errors.New("database unavailable")}
		},
		"no deleter is wired": func(*memoryAuthStore) *storeCredentialRows { return nil },
	}
	for name, deleter := range cases {
		t.Run(name, func(t *testing.T) {
			s := newMemoryAuthStore()
			s.seedPerson(t, "agent-dana", "Dana Whitfield", "google", googleSub, "dana.whitfield@harborlegal.example")
			logs := &signInLogs{}
			svc := newTestAssertedSignInWith(s, func(cfg *AssertedSignInConfig) {
				cfg.LinkByEmail = true
				cfg.EventStore = failingEventStore{esinfra.NewMemoryStore()}
				if rows := deleter(s); rows != nil {
					cfg.CredentialRows = rows
				}
				cfg.Logger = logs
			})

			if _, err := svc.SignIn(context.Background(), dana("apple", appleSub)); err == nil {
				t.Fatalf("a link whose creation could not be recorded signed the person in")
			}
			left := s.credentialFor("apple", appleSub)
			if left == nil {
				t.Fatalf("the row is gone although nothing could delete it")
			}
			lines := logs.all()
			if len(lines) != 2 || lines[0].level != "error" || lines[1].level != "error" {
				t.Fatalf("expected two error lines, got:\n%s", logs.text())
			}
			requireLineField(t, lines[0], "owner_agent_id", "agent-dana")
			requireLineField(t, lines[0], "credential_id", left.GetID())
			requireLineField(t, lines[0], "row_deleted", false)
			if !strings.HasPrefix(lines[1].msg, "repair:") {
				t.Fatalf("the second line %q is not a repair line", lines[1].msg)
			}
			requireLineField(t, lines[1], "owner_agent_id", "agent-dana")
			requireLineField(t, lines[1], "credential_id", left.GetID())
			requireNoRawIdentity(t, logs, appleSub, "dana.whitfield@harborlegal.example")
		})
	}
}

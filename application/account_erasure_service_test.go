package application

import (
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/wepala/weos/v3/domain/repositories"
	"github.com/wepala/weos/v3/domain/services"

	authentities "github.com/akeemphilbert/pericarp/pkg/auth/domain/entities"
	authrepos "github.com/akeemphilbert/pericarp/pkg/auth/domain/repositories"
	esinfra "github.com/akeemphilbert/pericarp/pkg/eventsourcing/infrastructure"
)

// erasureHarness records what each port saw, in order, so a test can say not
// only that a step ran but that it ran after the lock and before the purge.
type erasureHarness struct {
	accounts  *erasureAccounts
	locks     *erasureLocks
	purger    *erasurePurger
	files     *erasureFiles
	graphs    *erasureGraphs
	mu        sync.Mutex
	positions map[string]int64
	head      int64
	steps     *[]string
}

func (h *erasureHarness) setPosition(group string, position int64) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.positions[group] = position
}

func (h *erasureHarness) checkpoints(context.Context) (map[string]int64, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make(map[string]int64, len(h.positions))
	for k, v := range h.positions {
		out[k] = v
	}
	return out, nil
}

type erasureAccounts struct {
	authrepos.AccountRepository
	account *authentities.Account
	steps   *[]string
}

func (a *erasureAccounts) FindByID(_ context.Context, id string) (*authentities.Account, error) {
	if a.account == nil || a.account.GetID() != id {
		return nil, nil
	}
	return a.account, nil
}

func (a *erasureAccounts) Save(_ context.Context, account *authentities.Account) error {
	a.account = account
	*a.steps = append(*a.steps, "deactivate")
	return nil
}

type erasureLocks struct {
	locked map[string]string
	steps  *[]string
}

func (l *erasureLocks) Lock(_ context.Context, accountID, requestedBy string) error {
	if _, already := l.locked[accountID]; !already {
		l.locked[accountID] = requestedBy
	}
	*l.steps = append(*l.steps, "lock")
	return nil
}

func (l *erasureLocks) IsLocked(_ context.Context, accountID string) (bool, error) {
	_, ok := l.locked[accountID]
	return ok, nil
}

type erasurePurger struct {
	urns   []string
	purged bool
	steps  *[]string
}

func (p *erasurePurger) Enumerate(context.Context, string) (*repositories.AccountEnumeration, error) {
	*p.steps = append(*p.steps, "enumerate")
	return &repositories.AccountEnumeration{ResourceURNs: p.urns, Members: 2}, nil
}

func (p *erasurePurger) Purge(context.Context, string) (*repositories.PurgeReport, error) {
	*p.steps = append(*p.steps, "purge")
	p.purged = true
	return &repositories.PurgeReport{Members: 2, Resources: len(p.urns), Events: 7, DeletedAgents: []string{"ops"}}, nil
}

type erasureFiles struct {
	err     error
	deleted []string
	steps   *[]string
}

func (f *erasureFiles) Upload(context.Context, services.UploadParams, io.Reader) (*services.UploadResult, error) {
	return nil, errors.New("not used")
}

func (f *erasureFiles) DeleteAccountFolder(_ context.Context, accountID string) error {
	*f.steps = append(*f.steps, "files")
	if f.err != nil {
		return f.err
	}
	f.deleted = append(f.deleted, accountID)
	return nil
}

type erasureGraphs struct {
	fakeStores
	dropped  []string
	subjects []string
	steps    *[]string
}

func (g *erasureGraphs) DropAccount(_ context.Context, accountID string, subjects []string) error {
	*g.steps = append(*g.steps, "graph")
	g.dropped = append(g.dropped, accountID)
	g.subjects = subjects
	return nil
}

func newErasureHarness(t *testing.T) *erasureHarness {
	t.Helper()
	steps := &[]string{}
	account, err := (&authentities.Account{}).With("acct-harbor", "Harbor Legal", authentities.AccountTypePersonal)
	if err != nil {
		t.Fatal(err)
	}
	return &erasureHarness{
		accounts:  &erasureAccounts{account: account, steps: steps},
		locks:     &erasureLocks{locked: map[string]string{}, steps: steps},
		purger:    &erasurePurger{urns: []string{"urn:recipe:1", "urn:pantry:1"}, steps: steps},
		files:     &erasureFiles{steps: steps},
		graphs:    &erasureGraphs{steps: steps},
		positions: map[string]int64{},
		steps:     steps,
	}
}

func (h *erasureHarness) service(drainTimeout time.Duration) *AccountErasureService {
	store := esinfra.NewMemoryStore()
	svc := NewAccountErasureService(h.accounts, h.locks, h.purger, h.files, h.graphs,
		headOf{store, h.head}, h.checkpoints, drainTimeout, noopWorkerLogger{})
	svc.drainPoll = 5 * time.Millisecond
	return svc
}

// headOf wraps the memory store so a test can stage a feed head without
// appending events.
type headOf struct {
	*esinfra.MemoryStore
	head int64
}

func (h headOf) HeadPosition(context.Context) (int64, error) { return h.head, nil }

func TestAccountErasure_RunsTheSequenceLockDrainEnumerateStoresThenPurge(t *testing.T) {
	h := newErasureHarness(t)
	h.head = 10
	h.positions = map[string]int64{"oxigraph": 10, "display-values": 12}

	result, err := h.service(time.Second).Erase(context.Background(), EraseAccountCommand{AccountID: "acct-harbor", RequestedBy: "ops"})
	if err != nil {
		t.Fatalf("Erase: %v", err)
	}
	want := []string{"lock", "deactivate", "enumerate", "files", "graph", "purge"}
	if strings.Join(*h.steps, ",") != strings.Join(want, ",") {
		t.Fatalf("steps = %v, want %v", *h.steps, want)
	}
	if result.MembersLost != 2 || result.Resources != 2 || result.Events != 7 {
		t.Errorf("result = %+v, want 2 members, 2 resources, 7 events", result)
	}
	if h.accounts.account.Active() {
		t.Error("the account was not deactivated")
	}
	if h.locks.locked["acct-harbor"] != "ops" {
		t.Errorf("lock recorded %q as the requester, want ops", h.locks.locked["acct-harbor"])
	}
	if len(h.graphs.subjects) != 2 || h.graphs.subjects[0] != "urn:recipe:1" {
		t.Errorf("the graph drop was handed subjects %v, want the enumerated URNs", h.graphs.subjects)
	}
}

// The second @unit-pinned scenario: a background projection that never
// catches up fails the deletion and keeps the lock, with every store intact.
func TestAccountErasure_DrainTimeoutFailsTheDeletionAndKeepsTheLock(t *testing.T) {
	h := newErasureHarness(t)
	h.head = 10
	h.positions = map[string]int64{"oxigraph": 3, "display-values": 10}

	_, err := h.service(50*time.Millisecond).Erase(context.Background(), EraseAccountCommand{AccountID: "acct-harbor"})
	if !errors.Is(err, ErrErasureDrainTimeout) {
		t.Fatalf("Erase error = %v, want ErrErasureDrainTimeout", err)
	}
	if !strings.Contains(err.Error(), "oxigraph") {
		t.Errorf("the failure does not name the group that never caught up: %v", err)
	}
	if locked, _ := h.locks.IsLocked(context.Background(), "acct-harbor"); !locked {
		t.Error("the lock was released on a drain timeout")
	}
	if h.accounts.account.Active() {
		t.Error("the account was left active after the lock was taken")
	}
	for _, step := range *h.steps {
		if step == "enumerate" || step == "files" || step == "graph" || step == "purge" {
			t.Errorf("step %q ran even though the drain never finished", step)
		}
	}
	if len(h.files.deleted) != 0 || len(h.graphs.dropped) != 0 || h.purger.purged {
		t.Error("a store was touched after the drain timed out")
	}
}

func TestAccountErasure_DrainWaitsForALateGroupThenProceeds(t *testing.T) {
	h := newErasureHarness(t)
	h.head = 10
	h.positions = map[string]int64{"oxigraph": 8}
	go func() {
		time.Sleep(20 * time.Millisecond)
		h.setPosition("oxigraph", 10)
	}()
	if _, err := h.service(time.Second).Erase(context.Background(), EraseAccountCommand{AccountID: "acct-harbor"}); err != nil {
		t.Fatalf("Erase after the group caught up: %v", err)
	}
	if !h.purger.purged {
		t.Error("the purge did not run once the group reached the head")
	}
}

func TestAccountErasure_NoCheckpointRowsMeansNothingToDrain(t *testing.T) {
	h := newErasureHarness(t)
	h.head = 10
	h.positions = map[string]int64{}
	if _, err := h.service(10*time.Millisecond).Erase(context.Background(), EraseAccountCommand{AccountID: "acct-harbor"}); err != nil {
		t.Fatalf("Erase with no subscriber groups: %v", err)
	}
}

func TestAccountErasure_AFailedFileDeleteKeepsTheLockAndTheSQLState(t *testing.T) {
	h := newErasureHarness(t)
	h.files.err = errors.New("bucket refused")

	_, err := h.service(time.Second).Erase(context.Background(), EraseAccountCommand{AccountID: "acct-harbor"})
	if err == nil || !strings.Contains(err.Error(), "bucket refused") {
		t.Fatalf("Erase error = %v, want the file store's failure", err)
	}
	if h.purger.purged {
		t.Error("the SQL purge ran after the file store failed, so the re-run has nothing to enumerate")
	}
	if len(h.graphs.dropped) != 0 {
		t.Error("the graph was dropped after the file store failed")
	}
	if locked, _ := h.locks.IsLocked(context.Background(), "acct-harbor"); !locked {
		t.Error("the lock was released after a failed step")
	}

	// The re-run finishes: the lock is taken again, the account is already
	// inactive, and the stores are asked again.
	h.files.err = nil
	*h.steps = nil
	if _, err := h.service(time.Second).Erase(context.Background(), EraseAccountCommand{AccountID: "acct-harbor"}); err != nil {
		t.Fatalf("re-run: %v", err)
	}
	want := []string{"lock", "enumerate", "files", "graph", "purge"}
	if strings.Join(*h.steps, ",") != strings.Join(want, ",") {
		t.Fatalf("re-run steps = %v, want %v (no second deactivation)", *h.steps, want)
	}
}

func TestAccountErasure_UnknownAccountIsNotFound(t *testing.T) {
	h := newErasureHarness(t)
	_, err := h.service(time.Second).Erase(context.Background(), EraseAccountCommand{AccountID: "acct-nobody"})
	if !errors.Is(err, ErrAccountNotFound) {
		t.Fatalf("Erase error = %v, want ErrAccountNotFound", err)
	}
	if len(*h.steps) != 0 {
		t.Errorf("steps %v ran for an account that does not exist", *h.steps)
	}
}

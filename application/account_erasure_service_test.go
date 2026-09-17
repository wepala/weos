package application

import (
	"context"
	"errors"
	"io"
	"reflect"
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
	// written is when each row was last written; a row absent here was
	// written just now.
	written map[string]time.Time
	running []string
	head    int64
	steps   *[]string
	// participants are the embedding service's steps, in the order the
	// service is wired with them.
	participants []AccountErasureParticipant
}

func (h *erasureHarness) setPosition(group string, position int64) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.positions[group] = position
	delete(h.written, group)
}

// setStalePosition stages a row that stopped moving long ago.
func (h *erasureHarness) setStalePosition(group string, position int64, age time.Duration) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.positions[group] = position
	h.written[group] = time.Now().Add(-age)
}

func (h *erasureHarness) checkpoints(context.Context) ([]SubscriberCheckpoint, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]SubscriberCheckpoint, 0, len(h.positions))
	for name, position := range h.positions {
		updated, ok := h.written[name]
		if !ok {
			updated = time.Now()
		}
		out = append(out, SubscriberCheckpoint{Name: name, Position: position, UpdatedAt: updated})
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
	purges int
	// remains is what each Remains call answers, in order; past the end it
	// answers false. gone makes Purge answer ErrNothingToPurge.
	remains []bool
	gone    bool
	steps   *[]string
}

func (p *erasurePurger) Remains(context.Context, string) (bool, error) {
	if len(p.remains) == 0 {
		return false, nil
	}
	answer := p.remains[0]
	p.remains = p.remains[1:]
	return answer, nil
}

func (p *erasurePurger) Enumerate(context.Context, string) (*repositories.AccountEnumeration, error) {
	*p.steps = append(*p.steps, "enumerate")
	return &repositories.AccountEnumeration{ResourceURNs: p.urns, Members: 2}, nil
}

func (p *erasurePurger) Purge(context.Context, string) (*repositories.PurgeReport, error) {
	*p.steps = append(*p.steps, "purge")
	if p.gone {
		return nil, repositories.ErrNothingToPurge
	}
	p.purged = true
	p.purges++
	return &repositories.PurgeReport{Members: 2, Resources: len(p.urns), Events: 7, DeletedAgents: []string{"ops"},
		Groupings: []repositories.AccountGrouping{{AgentID: "ops", RoleID: "owner"}, {AgentID: "counsel", RoleID: "member"}}}, nil
}

// revokedRoles records what the erasure asked the enforcer to forget.
type revokedRoles struct {
	calls []string
}

func (r *revokedRoles) RevokeAccountRole(agentID, roleID, accountID string) error {
	r.calls = append(r.calls, agentID+":"+roleID+"@"+accountID)
	return nil
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
		written:   map[string]time.Time{},
		steps:     steps,
	}
}

func (h *erasureHarness) service(drainTimeout time.Duration) *AccountErasureService {
	store := esinfra.NewMemoryStore()
	svc := NewAccountErasureService(AccountErasureDeps{
		Accounts: h.accounts, Locks: h.locks, Purger: h.purger, Files: h.files, Graphs: h.graphs,
		EventStore: headOf{store, h.head}, Checkpoints: h.checkpoints,
		RunningGroups: func() []string { return h.running },
		DrainTimeout:  drainTimeout, StaleAfter: time.Minute, Logger: noopWorkerLogger{},
		Participants: h.participants,
	})
	svc.drainPoll = 5 * time.Millisecond
	svc.frozenGrace = 20 * time.Millisecond
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

// wm-gyfdi: a checkpoint row left behind by a group nobody runs any more
// must not fail every deletion on the instance. It is told from a live row
// by being both unowned here and unwritten for longer than the staleness
// window.
func TestAccountErasure_DrainSetsAsideAFrozenRowOfAGroupNobodyRuns(t *testing.T) {
	h := newErasureHarness(t)
	h.head = 10
	h.setStalePosition("oxigraph", 3, time.Hour) // turned off long ago; nothing here runs it
	h.setPosition("display-values", 10)

	if _, err := h.service(500*time.Millisecond).Erase(context.Background(), EraseAccountCommand{AccountID: "acct-harbor"}); err != nil {
		t.Fatalf("Erase waited on a checkpoint no group advances: %v", err)
	}
	if !h.purger.purged {
		t.Error("the purge did not run past the frozen row")
	}
}

func TestAccountErasure_DrainWaitsForAGroupThisProcessRunsHoweverOldItsRow(t *testing.T) {
	h := newErasureHarness(t)
	h.head = 10
	h.running = []string{"oxigraph"}
	h.setStalePosition("oxigraph", 3, time.Hour) // idle for an hour, but alive here and behind

	_, err := h.service(80*time.Millisecond).Erase(context.Background(), EraseAccountCommand{AccountID: "acct-harbor"})
	if !errors.Is(err, ErrErasureDrainTimeout) {
		t.Fatalf("Erase error = %v, want ErrErasureDrainTimeout for a running group that is behind", err)
	}
	if h.purger.purged {
		t.Error("the purge ran ahead of a group this process runs")
	}
}

func TestAccountErasure_DrainWaitsForARowAnotherProcessIsStillWriting(t *testing.T) {
	h := newErasureHarness(t)
	h.head = 10
	h.setPosition("oxigraph", 3) // not run here, but written just now: a worker elsewhere is alive

	_, err := h.service(80*time.Millisecond).Erase(context.Background(), EraseAccountCommand{AccountID: "acct-harbor"})
	if !errors.Is(err, ErrErasureDrainTimeout) {
		t.Fatalf("Erase error = %v, want ErrErasureDrainTimeout for a fresh row that is behind", err)
	}
}

func TestAccountErasure_SkipDrainIsTheOperatorsOverride(t *testing.T) {
	h := newErasureHarness(t)
	h.head = 10
	h.running = []string{"oxigraph"}
	h.setPosition("oxigraph", 3)

	_, err := h.service(80*time.Millisecond).Erase(context.Background(),
		EraseAccountCommand{AccountID: "acct-harbor", RequestedBy: "operator", SkipDrain: true})
	if err != nil {
		t.Fatalf("Erase with SkipDrain: %v", err)
	}
	if !h.purger.purged {
		t.Error("the purge did not run when the drain was skipped")
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

// wm-mpj0l: the run is detached from the request. A client that hangs up
// mid-walk cancels the request's context; the erasure carries on and finishes.
func TestAccountErasure_ACancelledRequestDoesNotAbortTheRun(t *testing.T) {
	h := newErasureHarness(t)
	h.head = 10
	h.setPosition("oxigraph", 8)
	go func() {
		time.Sleep(30 * time.Millisecond)
		h.setPosition("oxigraph", 10)
	}()
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // the client is already gone when the service starts
	if _, err := h.service(time.Second).Erase(ctx, EraseAccountCommand{AccountID: "acct-harbor"}); err != nil {
		t.Fatalf("Erase under a canceled request context: %v", err)
	}
	if !h.purger.purged {
		t.Error("the purge did not run after the request's context was canceled")
	}
}

// blockingPurger holds the purge until released, so a second run can arrive
// while the first is still inside the sequence.
type blockingPurger struct {
	erasurePurger
	once    sync.Once
	entered chan struct{}
	release chan struct{}
}

func (p *blockingPurger) Purge(ctx context.Context, id string) (*repositories.PurgeReport, error) {
	p.once.Do(func() {
		close(p.entered)
		<-p.release
	})
	return p.erasurePurger.Purge(ctx, id)
}

func TestAccountErasure_ASecondRunOfTheSameAccountIsRefusedWhileTheFirstRuns(t *testing.T) {
	h := newErasureHarness(t)
	blocking := &blockingPurger{erasurePurger: *h.purger, entered: make(chan struct{}), release: make(chan struct{})}
	h.purger = &blocking.erasurePurger
	svc := NewAccountErasureService(AccountErasureDeps{
		Accounts: h.accounts, Locks: h.locks, Purger: blocking, Files: h.files, Graphs: h.graphs,
		EventStore: headOf{esinfra.NewMemoryStore(), h.head}, Checkpoints: h.checkpoints,
		DrainTimeout: time.Second, Logger: noopWorkerLogger{},
	})
	svc.drainPoll = 5 * time.Millisecond

	first := make(chan error, 1)
	go func() {
		_, err := svc.Erase(context.Background(), EraseAccountCommand{AccountID: "acct-harbor"})
		first <- err
	}()
	<-blocking.entered
	_, err := svc.Erase(context.Background(), EraseAccountCommand{AccountID: "acct-harbor"})
	if !errors.Is(err, ErrErasureInProgress) {
		t.Fatalf("second Erase error = %v, want ErrErasureInProgress", err)
	}
	close(blocking.release)
	if err := <-first; err != nil {
		t.Fatalf("the first run failed: %v", err)
	}
	// Once the first run has returned the account may be erased again.
	if _, err := svc.Erase(context.Background(), EraseAccountCommand{AccountID: "acct-harbor"}); errors.Is(err, ErrErasureInProgress) {
		t.Fatal("the account stayed marked in progress after the run returned")
	}
}

// wm-mnry2: a request admitted before the lock can commit after the purge.
// What it left is swept again in the same run.
func TestAccountErasure_RowsThatLandAfterThePurgeAreSweptAgain(t *testing.T) {
	h := newErasureHarness(t)
	h.purger.remains = []bool{true, false}
	result, err := h.service(time.Second).Erase(context.Background(), EraseAccountCommand{AccountID: "acct-harbor"})
	if err != nil {
		t.Fatalf("Erase: %v", err)
	}
	if h.purger.purges != 2 {
		t.Fatalf("the purge ran %d time(s), want 2: once, and once more for what landed after it", h.purger.purges)
	}
	if result.Events != 14 {
		t.Errorf("result counts %d events, want both sweeps' (14)", result.Events)
	}
	want := []string{"lock", "deactivate", "enumerate", "files", "graph", "purge", "enumerate", "files", "graph", "purge"}
	if strings.Join(*h.steps, ",") != strings.Join(want, ",") {
		t.Fatalf("steps = %v, want %v", *h.steps, want)
	}
}

func TestAccountErasure_RowsThatKeepArrivingFailTheRunAfterTheBound(t *testing.T) {
	h := newErasureHarness(t)
	h.purger.remains = []bool{true, true, true, true}
	_, err := h.service(time.Second).Erase(context.Background(), EraseAccountCommand{AccountID: "acct-harbor"})
	if err == nil || !strings.Contains(err.Error(), "kept arriving") {
		t.Fatalf("Erase error = %v, want the bound reported", err)
	}
	if h.purger.purges != 1+orphanSweeps {
		t.Errorf("the purge ran %d time(s), want %d", h.purger.purges, 1+orphanSweeps)
	}
}

// wm-mnry2: the orphans of an account whose row is already gone are swept
// by a run named after it, with no lock to take.
func TestAccountErasure_OrphansOfAGoneAccountAreSweptWithoutALock(t *testing.T) {
	h := newErasureHarness(t)
	h.accounts.account = nil
	h.purger.remains = []bool{true, false}
	if _, err := h.service(time.Second).Erase(context.Background(), EraseAccountCommand{AccountID: "acct-harbor"}); err != nil {
		t.Fatalf("Erase of a gone account's orphans: %v", err)
	}
	want := []string{"enumerate", "files", "graph", "purge"}
	if strings.Join(*h.steps, ",") != strings.Join(want, ",") {
		t.Fatalf("steps = %v, want %v (no lock, no deactivation)", *h.steps, want)
	}
	if len(h.locks.locked) != 0 {
		t.Error("a lock was taken for an account with no row")
	}
}

// wm-4cysr: the second of two deletions that arrived together finds the
// purge already done and is told the account is not found.
func TestAccountErasure_ASecondDeletionThatFindsNothingLeftIsNotFound(t *testing.T) {
	h := newErasureHarness(t)
	h.purger.gone = true
	_, err := h.service(time.Second).Erase(context.Background(), EraseAccountCommand{AccountID: "acct-harbor"})
	if !errors.Is(err, ErrAccountNotFound) {
		t.Fatalf("Erase error = %v, want ErrAccountNotFound", err)
	}
}

// wm-wrnzb: the purge deletes the account's grouping rows; the running
// enforcer's copy of them is revoked through the checker.
func TestAccountErasure_RevokesTheAccountsRolesFromTheRunningEnforcer(t *testing.T) {
	h := newErasureHarness(t)
	revoked := &revokedRoles{}
	svc := NewAccountErasureService(AccountErasureDeps{
		Accounts: h.accounts, Locks: h.locks, Purger: h.purger, Files: h.files, Graphs: h.graphs,
		EventStore: headOf{esinfra.NewMemoryStore(), h.head}, Checkpoints: h.checkpoints,
		DrainTimeout: time.Second, Roles: revoked, Logger: noopWorkerLogger{},
	})
	svc.drainPoll = 5 * time.Millisecond
	if _, err := svc.Erase(context.Background(), EraseAccountCommand{AccountID: "acct-harbor"}); err != nil {
		t.Fatalf("Erase: %v", err)
	}
	want := "ops:owner@acct-harbor,counsel:member@acct-harbor"
	if strings.Join(revoked.calls, ",") != want {
		t.Fatalf("revoked %v, want %s", revoked.calls, want)
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

// recordingParticipant is an embedding service's step: it records that it
// ran, in the harness's one step list, so a test can say where in the
// sequence it ran.
type recordingParticipant struct {
	name  string
	err   error
	steps *[]string
	// seen is what the participant was told about the account.
	seen ErasingAccount
	// deadline is whether the context it was handed had one, and whether it
	// was still live. A participant is the last thing to run before anything
	// is removed, so both matter.
	hadDeadline bool
	wasLive     bool
	ran         int
}

func (p *recordingParticipant) Name() string { return p.name }

func (p *recordingParticipant) BeforeAccountErased(ctx context.Context, account ErasingAccount) error {
	*p.steps = append(*p.steps, "participant:"+p.name)
	p.seen = account
	_, p.hadDeadline = ctx.Deadline()
	p.wasLive = ctx.Err() == nil
	p.ran++
	return p.err
}

// wm-j2sg5: an embedding service's step runs inside the erasure, after the
// lock and the drain and before the first step that removes anything, so it
// sees the account's data whole.
func TestAccountErasure_ParticipantsRunBeforeAnythingIsRemoved(t *testing.T) {
	h := newErasureHarness(t)
	h.head = 10
	h.positions = map[string]int64{"oxigraph": 10}
	first := &recordingParticipant{name: "bank-links", steps: h.steps}
	second := &recordingParticipant{name: "identity-tokens", steps: h.steps}
	h.participants = []AccountErasureParticipant{first, second}

	if _, err := h.service(time.Second).Erase(context.Background(),
		EraseAccountCommand{AccountID: "acct-harbor", RequestedBy: "ops"}); err != nil {
		t.Fatalf("Erase: %v", err)
	}
	want := []string{"lock", "deactivate", "participant:bank-links", "participant:identity-tokens",
		"enumerate", "files", "graph", "purge"}
	if strings.Join(*h.steps, ",") != strings.Join(want, ",") {
		t.Fatalf("steps = %v, want %v", *h.steps, want)
	}
	if first.seen.AccountID != "acct-harbor" || first.seen.RequestedBy != "ops" {
		t.Errorf("the participant was told %+v, want the account and who asked", first.seen)
	}
	if !first.hadDeadline || !first.wasLive {
		t.Errorf("the participant's context had deadline=%v live=%v, want the erasure's own live deadline",
			first.hadDeadline, first.wasLive)
	}
}

// A participant that fails stops the erasure where it stands: nothing of the
// account's is removed, the lock is kept, and the caller is told which step
// failed rather than being answered 2xx.
func TestAccountErasure_AFailedParticipantAbortsWithNothingErased(t *testing.T) {
	h := newErasureHarness(t)
	refused := errors.New("the aggregator refused to unlink the item")
	ran := &recordingParticipant{name: "bank-links", steps: h.steps}
	failing := &recordingParticipant{name: "identity-tokens", err: refused, steps: h.steps}
	after := &recordingParticipant{name: "zz-never-runs", steps: h.steps}
	h.participants = []AccountErasureParticipant{ran, failing, after}

	_, err := h.service(time.Second).Erase(context.Background(),
		EraseAccountCommand{AccountID: "acct-harbor", RequestedBy: "ops"})
	if !errors.Is(err, ErrErasureParticipantFailed) {
		t.Fatalf("Erase error = %v, want ErrErasureParticipantFailed", err)
	}
	if !errors.Is(err, refused) {
		t.Errorf("the failure does not carry the participant's own error: %v", err)
	}
	if !strings.Contains(err.Error(), "identity-tokens") {
		t.Errorf("the failure does not name the participant that failed: %v", err)
	}
	if after.ran != 0 {
		t.Error("a participant after the failing one ran")
	}
	for _, step := range *h.steps {
		if step == "enumerate" || step == "files" || step == "graph" || step == "purge" {
			t.Errorf("step %q ran after a participant failed", step)
		}
	}
	if len(h.files.deleted) != 0 || len(h.graphs.dropped) != 0 || h.purger.purged {
		t.Error("a store was touched after a participant failed")
	}
	if locked, _ := h.locks.IsLocked(context.Background(), "acct-harbor"); !locked {
		t.Error("the lock was released after a participant failed")
	}
	if h.accounts.account.Active() {
		t.Error("the account was left active after a participant failed")
	}

	// The deletion can be run again, and it runs every participant again.
	failing.err = nil
	*h.steps = nil
	if _, err := h.service(time.Second).Erase(context.Background(),
		EraseAccountCommand{AccountID: "acct-harbor"}); err != nil {
		t.Fatalf("re-run: %v", err)
	}
	if ran.ran != 2 || failing.ran != 2 || after.ran != 1 {
		t.Errorf("re-run ran the participants %d/%d/%d times, want 2/2/1", ran.ran, failing.ran, after.ran)
	}
}

// An instance that registers no participant gets the sequence it always had.
func TestAccountErasure_NoParticipantsLeavesTheSequenceUnchanged(t *testing.T) {
	h := newErasureHarness(t)
	h.head = 10
	h.positions = map[string]int64{"oxigraph": 10}
	h.participants = nil

	result, err := h.service(time.Second).Erase(context.Background(),
		EraseAccountCommand{AccountID: "acct-harbor", RequestedBy: "ops"})
	if err != nil {
		t.Fatalf("Erase with no participants: %v", err)
	}
	want := []string{"lock", "deactivate", "enumerate", "files", "graph", "purge"}
	if strings.Join(*h.steps, ",") != strings.Join(want, ",") {
		t.Fatalf("steps = %v, want %v", *h.steps, want)
	}
	if result.MembersLost != 2 || result.Resources != 2 || result.Events != 7 {
		t.Errorf("result = %+v, want the sequence's usual counts", result)
	}
}

// Fx shuffles the members of a value group, so the container path fixes the
// run order by name — the same order on every process that has the same
// participants.
func TestAccountErasure_ContainerParticipantsRunInNameOrder(t *testing.T) {
	steps := &[]string{}
	shuffled := []AccountErasureParticipant{
		&recordingParticipant{name: "identity-tokens", steps: steps},
		nil,
		&recordingParticipant{name: "bank-links", steps: steps},
	}
	sorted := sortParticipantsByName(shuffled)
	if len(sorted) != 2 {
		t.Fatalf("sorted %d participants, want the 2 non-nil ones", len(sorted))
	}
	if sorted[0].Name() != "bank-links" || sorted[1].Name() != "identity-tokens" {
		t.Fatalf("order = %s,%s, want bank-links,identity-tokens", sorted[0].Name(), sorted[1].Name())
	}
}

// A participant that names itself nothing is still named in the failure, by
// its type, so an operator can tell which step did not finish.
func TestAccountErasure_AnUnnamedParticipantIsNamedByItsType(t *testing.T) {
	h := newErasureHarness(t)
	h.participants = []AccountErasureParticipant{
		&recordingParticipant{err: errors.New("no"), steps: h.steps},
	}
	_, err := h.service(time.Second).Erase(context.Background(), EraseAccountCommand{AccountID: "acct-harbor"})
	if !errors.Is(err, ErrErasureParticipantFailed) {
		t.Fatalf("Erase error = %v, want ErrErasureParticipantFailed", err)
	}
	if !strings.Contains(err.Error(), "recordingParticipant") {
		t.Errorf("the failure names no participant: %v", err)
	}
}

// The helper's value-group name and the name the service's Fx params collect
// have to be the same string. A drift between them is silent: the container
// still builds, the binary still registers its participant, and the step
// never runs.
func TestAccountErasure_TheParticipantGroupTagMatchesTheCollector(t *testing.T) {
	field, ok := reflect.TypeOf(AccountErasureParams{}).FieldByName("Participants")
	if !ok {
		t.Fatal("AccountErasureParams has no Participants field")
	}
	want := strings.TrimSuffix(strings.TrimPrefix(accountErasureParticipantTag, `group:"`), `"`)
	if got := field.Tag.Get("group"); got != want {
		t.Fatalf("the params collect group %q, want %q — a participant registered with AsAccountErasureParticipant would never run", got, want)
	}
}

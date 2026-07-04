package e2e

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/wepala/weos/v3/api/handlers"
	"github.com/wepala/weos/v3/application"
	"github.com/wepala/weos/v3/application/presets"
	"github.com/wepala/weos/v3/domain/entities"
	weosgorm "github.com/wepala/weos/v3/infrastructure/database/gorm"
	"github.com/wepala/weos/v3/infrastructure/logging"
	"github.com/wepala/weos/v3/internal/config"

	pericarpdomain "github.com/akeemphilbert/pericarp/pkg/eventsourcing/domain"
	"github.com/akeemphilbert/pericarp/pkg/eventsourcing/subscriptions"
	"github.com/cucumber/godog"
	_ "github.com/glebarez/go-sqlite" // registers the "sqlite" database/sql driver for the lock-holding connection
	"github.com/labstack/echo/v4"
	"go.uber.org/fx"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
)

// TestSQLiteWriteSerialization runs the write-gate acceptance scenarios
// (feature #426): an in-process write gate serializes SQLite write transactions
// so a concurrent burst queues instead of failing with "database is locked",
// while reads and server boot stay responsive and a genuinely stuck database
// still surfaces a bounded-retry error. Filter on demand with:
// GODOG_TAGS=@wip go test ./tests/e2e/ -run TestSQLiteWriteSerialization -v
func TestSQLiteWriteSerialization(t *testing.T) {
	tags := "~@wip"
	if override := os.Getenv("GODOG_TAGS"); override != "" {
		tags = override
	}
	suite := godog.TestSuite{
		Name:                "sqlite-write-serialization",
		ScenarioInitializer: initSQLiteWriteSerializationScenario,
		Options: &godog.Options{
			Format:   "pretty",
			Paths:    []string{"features/sqlite_write_serialization.feature"},
			Tags:     tags,
			Strict:   true,
			TestingT: t,
		},
	}
	if suite.Run() != 0 {
		t.Fatal("sqlite write-serialization acceptance scenarios failed")
	}
}

// burstSubscribers are the checkpointed subscriber groups the write-burst
// scenarios expect to be running on file-based SQLite (all three register
// unconditionally on SQLite — lexical-index because glebarez ships FTS5).
var burstSubscribers = []string{"lexical-index", "event-references", "display-values"}

// sqliteWriteWorld boots the full application against a file-backed SQLite
// database and drives it the way the feature's scenarios describe: concurrent
// bursts through the service layer, checkpoint polling for the async
// subscribers, a health server for the restart scenario, and a second raw
// connection to simulate external write-lock contention.
type sqliteWriteWorld struct {
	tmpDir string
	dbPath string
	dsn    string

	app         *fx.App
	rs          application.ResourceService
	rts         application.ResourceTypeService
	eventStore  pericarpdomain.EventStore
	checkpoints subscriptions.CheckpointStore
	manager     *application.Manager

	// observerLogger replaces the app's entities.Logger so the "database is
	// locked" assertion can inspect everything the app (and its workers) logged.
	observerLogger entities.Logger
	logs           *observer.ObservedLogs

	healthServer *httptest.Server
	restartAt    time.Time

	resources      map[string]string // name -> URN, for the read-during-burst scenario
	createErrs     []error           // per-goroutine results of the concurrent burst
	createErr      error             // result of a single create (retry / lock scenarios)
	headAfterBurst int64

	// sustained-burst control (reads-are-served-during-a-burst scenario).
	burstStop       chan struct{}
	burstWG         sync.WaitGroup
	burstActive     atomic.Bool
	readErr         error
	readDuringBurst bool

	// busy-retry observation (transient-BUSY and stays-locked scenarios).
	busyAttempts atomic.Int64
	rawLockDB    *sql.DB
	rawLockTx    *sql.Tx
	rawLockOnce  sync.Once
}

func initSQLiteWriteSerializationScenario(sc *godog.ScenarioContext) {
	w := &sqliteWriteWorld{resources: map[string]string{}}

	sc.After(func(ctx context.Context, _ *godog.Scenario, _ error) (context.Context, error) {
		w.teardown()
		return ctx, nil
	})

	sc.Step(`^a clean WeOS instance backed by a file-based SQLite database$`, w.aCleanInstance)
	sc.Step(`^the "([^"]*)" preset is installed$`, w.presetInstalled)
	sc.Step(`^the checkpointed subscribers "([^"]*)", "([^"]*)" and "([^"]*)" are running$`, w.subscribersRunning)
	sc.Step(`^(\d+) "([^"]*)" resources are created concurrently within one second$`, w.createConcurrently)
	sc.Step(`^every create succeeds$`, w.everyCreateSucceeds)
	sc.Step(`^no "([^"]*)" error is logged during the burst$`, w.noErrorLoggedDuringBurst)
	sc.Step(`^the subscribers finish processing the burst$`, w.subscribersFinishBurst)
	sc.Step(`^each subscriber's checkpoint has advanced past every event in the burst$`, w.checkpointsAdvancedPastBurst)
	sc.Step(`^the event store holds a backlog of (\d+) events no subscriber has processed$`, w.backlogOfEvents)
	sc.Step(`^the server is restarted$`, w.serverRestarted)
	sc.Step(`^the health endpoint reports healthy within (\d+) seconds of boot$`, w.healthyWithinSeconds)
	sc.Step(`^every subscriber eventually processes the backlog$`, w.everySubscriberProcessesBacklog)
	sc.Step(`^a "([^"]*)" named "([^"]*)" exists$`, w.aResourceNamedExists)
	sc.Step(`^a sustained burst of concurrent writes is in flight$`, w.sustainedBurstInFlight)
	sc.Step(`^I fetch the project "([^"]*)" during the burst$`, w.fetchProjectDuringBurst)
	sc.Step(`^the read succeeds while the burst is still in flight$`, w.readSucceedsWhileBurst)
	sc.Step(`^the next write to the database transiently fails with SQLITE_BUSY$`, w.nextWriteTransientlyBusy)
	sc.Step(`^I create a "([^"]*)" named "([^"]*)"$`, w.iCreateResourceNamed)
	sc.Step(`^the create succeeds$`, w.theCreateSucceeds)
	sc.Step(`^the write was attempted more than once$`, w.writeAttemptedMoreThanOnce)
	sc.Step(`^the database stays locked for longer than the retry budget allows$`, w.databaseStaysLocked)
	sc.Step(`^the create fails with a database-locked error$`, w.createFailsWithLockedError)
	sc.Step(`^the write gave up after a bounded number of attempts$`, w.writeGaveUpBounded)
}

// --- Background ---

func (w *sqliteWriteWorld) aCleanInstance() error {
	dir, err := os.MkdirTemp("", "weos-sqlite-write-e2e-")
	if err != nil {
		return err
	}
	w.tmpDir = dir
	w.dbPath = filepath.Join(dir, "test.db")
	// A fast busy_timeout keeps the BUSY-retry scenarios quick. The provider
	// preserves any pragma already present in the DSN, so this 100ms wins over
	// the 15s worker default (sqliteDSNWithWorkerPragmas).
	w.dsn = w.dbPath + "?_pragma=busy_timeout(100)"

	// Capture at Info level: the production logger emits Info and above, and a
	// database-locked failure surfaces as an Error entry — both are recorded.
	core, logs := observer.New(zapcore.InfoLevel)
	w.logs = logs
	w.observerLogger = logging.NewZapLogger(zap.New(core))

	// The clean instance boots with workers OFF; the "subscribers are running"
	// step brings them up, and the backlog scenario seeds against this quiet
	// process before restarting it with workers on.
	return w.bootApp(false)
}

// bootApp (re)starts the fx application on the world's fixed DSN. The database
// file persists across reboots, so the preset and any seeded data survive a
// workers-off -> workers-on transition.
func (w *sqliteWriteWorld) bootApp(runWorkers bool) error {
	w.stopApp()
	cfg := config.Default()
	cfg.DatabaseDSN = w.dsn
	cfg.LogLevel = "error"
	cfg.Worker.RunInProcess = runWorkers

	var rs application.ResourceService
	var rts application.ResourceTypeService
	var es pericarpdomain.EventStore
	var cp subscriptions.CheckpointStore
	var mgr *application.Manager

	app := fx.New(
		fx.NopLogger,
		application.Module(cfg, presets.NewDefaultRegistry()),
		fx.Decorate(func(entities.Logger) entities.Logger { return w.observerLogger }),
		fx.Populate(&rs, &rts, &es, &cp, &mgr),
	)
	startCtx, cancel := context.WithTimeout(context.Background(), fx.DefaultTimeout)
	defer cancel()
	if err := app.Start(startCtx); err != nil {
		return fmt.Errorf("failed to start app: %w", err)
	}
	w.app = app
	w.rs = rs
	w.rts = rts
	w.eventStore = es
	w.checkpoints = cp
	w.manager = mgr
	return nil
}

func (w *sqliteWriteWorld) presetInstalled(ctx context.Context, name string) error {
	if w.rts == nil {
		return fmt.Errorf("application not booted")
	}
	if _, err := w.rts.InstallPreset(ctx, name, true); err != nil {
		return fmt.Errorf("failed to install %q preset: %w", name, err)
	}
	return nil
}

func (w *sqliteWriteWorld) subscribersRunning(a, b, c string) error {
	// Bring the process up with background subscribers enabled. The preset and
	// any prior writes persist in the same database file across the reboot.
	if err := w.bootApp(true); err != nil {
		return err
	}
	have := map[string]bool{}
	for _, n := range w.manager.Names() {
		have[n] = true
	}
	for _, want := range []string{a, b, c} {
		if !have[want] {
			return fmt.Errorf("subscriber %q is not registered (have %v)", want, w.manager.Names())
		}
	}
	return nil
}

// --- Concurrent burst ---

func (w *sqliteWriteWorld) createConcurrently(count int, typeSlug string) error {
	errs := make([]error, count)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < count; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start // release all goroutines together so the writes truly race
			errs[i] = w.createResource(context.Background(), typeSlug, fmt.Sprintf("Burst %s %02d", typeSlug, i))
		}(i)
	}
	close(start)
	wg.Wait()
	w.createErrs = errs

	head, err := w.eventStore.HeadPosition(context.Background())
	if err != nil {
		return fmt.Errorf("read event store head after burst: %w", err)
	}
	w.headAfterBurst = head
	return nil
}

func (w *sqliteWriteWorld) everyCreateSucceeds() error {
	for i, err := range w.createErrs {
		if err != nil {
			return fmt.Errorf("create %d of %d failed: %w", i, len(w.createErrs), err)
		}
	}
	return nil
}

func (w *sqliteWriteWorld) noErrorLoggedDuringBurst(phrase string) error {
	needles := []string{strings.ToLower(phrase), "sqlite_busy"}
	for _, e := range w.logs.All() {
		hay := strings.ToLower(e.Message)
		for k, v := range e.ContextMap() {
			hay += " " + strings.ToLower(k) + " " + strings.ToLower(fmt.Sprint(v))
		}
		for _, needle := range needles {
			if strings.Contains(hay, needle) {
				return fmt.Errorf("a log entry surfaced a locked-database error: %q %v", e.Message, e.ContextMap())
			}
		}
	}
	for i, err := range w.createErrs {
		if err == nil {
			continue
		}
		msg := strings.ToLower(err.Error())
		for _, needle := range needles {
			if strings.Contains(msg, needle) {
				return fmt.Errorf("create %d returned a locked-database error: %w", i, err)
			}
		}
	}
	return nil
}

func (w *sqliteWriteWorld) subscribersFinishBurst() error {
	return w.waitCheckpointsAtLeast(context.Background(), w.headAfterBurst, 15*time.Second)
}

func (w *sqliteWriteWorld) checkpointsAdvancedPastBurst() error {
	return w.waitCheckpointsAtLeast(context.Background(), w.headAfterBurst, 15*time.Second)
}

// waitCheckpointsAtLeast polls every burst subscriber's checkpoint until each
// has advanced to or past head, or the timeout elapses.
func (w *sqliteWriteWorld) waitCheckpointsAtLeast(ctx context.Context, head int64, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		status := make([]string, 0, len(burstSubscribers))
		behind := false
		for _, name := range burstSubscribers {
			pos, err := w.checkpoints.Position(ctx, name)
			if err != nil {
				return fmt.Errorf("read checkpoint %q: %w", name, err)
			}
			status = append(status, fmt.Sprintf("%s=%d", name, pos))
			if pos < head {
				behind = true
			}
		}
		if !behind {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("subscribers did not reach the burst head %d within %s: %s",
				head, timeout, strings.Join(status, " "))
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// --- Restart / backlog ---

func (w *sqliteWriteWorld) backlogOfEvents(count int) error {
	ctx := context.Background()
	for i := 0; i < count; i++ {
		if err := w.createResource(ctx, "task", fmt.Sprintf("Backlog Task %03d", i)); err != nil {
			return fmt.Errorf("seed backlog task %d: %w", i, err)
		}
	}
	head, err := w.eventStore.HeadPosition(ctx)
	if err != nil {
		return fmt.Errorf("read event store head after seeding backlog: %w", err)
	}
	w.headAfterBurst = head
	// Stop this workers-off process but keep the database file so the restart
	// replays the backlog no subscriber has yet consumed.
	w.stopApp()
	return nil
}

func (w *sqliteWriteWorld) serverRestarted() error {
	// Measure "within N seconds of boot" from just before the process starts.
	w.restartAt = time.Now()
	if err := w.bootApp(true); err != nil {
		return err
	}
	e := echo.New()
	e.HideBanner = true
	e.GET("/api/health", handlers.HealthHandler)
	w.healthServer = httptest.NewServer(e)
	return nil
}

func (w *sqliteWriteWorld) healthyWithinSeconds(seconds int) error {
	if w.healthServer == nil {
		return fmt.Errorf("no health server is running")
	}
	budget := time.Duration(seconds) * time.Second
	deadline := w.restartAt.Add(budget)
	url := w.healthServer.URL + "/api/health"
	for {
		resp, err := http.Get(url)
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				if elapsed := time.Since(w.restartAt); elapsed > budget {
					return fmt.Errorf("health became ready in %s, over the %s budget", elapsed, budget)
				}
				return nil
			}
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("health endpoint did not report healthy within %s of boot", budget)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func (w *sqliteWriteWorld) everySubscriberProcessesBacklog() error {
	return w.waitCheckpointsAtLeast(context.Background(), w.headAfterBurst, 30*time.Second)
}

// --- Reads during a burst ---

func (w *sqliteWriteWorld) aResourceNamedExists(typeSlug, name string) error {
	data := json.RawMessage(fmt.Sprintf(`{"name":%q}`, name))
	res, err := w.rs.Create(context.Background(), application.CreateResourceCommand{TypeSlug: typeSlug, Data: data})
	if err != nil {
		return fmt.Errorf("failed to create %s %q: %w", typeSlug, name, err)
	}
	w.resources[name] = res.GetID()
	return nil
}

func (w *sqliteWriteWorld) sustainedBurstInFlight() error {
	w.startBurst()
	return nil
}

func (w *sqliteWriteWorld) fetchProjectDuringBurst(name string) error {
	id, ok := w.resources[name]
	if !ok {
		return fmt.Errorf("no known resource named %q", name)
	}
	// Record that the burst was active at read time; reads bypass the write
	// gate, so the fetch should return without waiting on the write queue.
	w.readDuringBurst = w.burstActive.Load()
	_, w.readErr = w.rs.GetByID(context.Background(), id)
	return nil
}

func (w *sqliteWriteWorld) readSucceedsWhileBurst() error {
	if !w.readDuringBurst {
		return fmt.Errorf("the read was not issued while a burst was active")
	}
	if !w.burstActive.Load() {
		return fmt.Errorf("the burst ended before the read could be validated as in-flight")
	}
	err := w.readErr
	w.stopBurst()
	if err != nil {
		return fmt.Errorf("the read failed while the burst was in flight: %w", err)
	}
	return nil
}

func (w *sqliteWriteWorld) startBurst() {
	w.burstStop = make(chan struct{})
	w.burstActive.Store(true)
	for i := 0; i < 8; i++ {
		w.burstWG.Add(1)
		go func(id int) {
			defer w.burstWG.Done()
			for c := 0; ; c++ {
				select {
				case <-w.burstStop:
					return
				default:
				}
				_ = w.createResource(context.Background(), "task", fmt.Sprintf("Sustained %d-%d", id, c))
			}
		}(i)
	}
}

func (w *sqliteWriteWorld) stopBurst() {
	if w.burstStop == nil {
		return
	}
	close(w.burstStop)
	w.burstWG.Wait()
	w.burstActive.Store(false)
	w.burstStop = nil
}

// --- Busy-retry / stays-locked ---

func (w *sqliteWriteWorld) nextWriteTransientlyBusy() error {
	w.observeBusyRetries()
	// Hold the write lock from a second connection, then release it well inside
	// the retry budget so a retried BeginTx eventually lands.
	return w.holdWriteLock(250 * time.Millisecond)
}

func (w *sqliteWriteWorld) databaseStaysLocked() error {
	w.observeBusyRetries()
	// Hold the lock past the whole retry budget; teardown releases it after the
	// create has failed.
	return w.holdWriteLock(0)
}

func (w *sqliteWriteWorld) iCreateResourceNamed(typeSlug, name string) error {
	w.busyAttempts.Store(0)
	w.createErr = w.createResource(context.Background(), typeSlug, name)
	return nil
}

func (w *sqliteWriteWorld) theCreateSucceeds() error {
	if w.createErr != nil {
		return fmt.Errorf("expected the create to succeed, got: %w", w.createErr)
	}
	return nil
}

func (w *sqliteWriteWorld) writeAttemptedMoreThanOnce() error {
	if n := w.busyAttempts.Load(); n <= 1 {
		return fmt.Errorf("expected more than one write attempt, got %d", n)
	}
	return nil
}

func (w *sqliteWriteWorld) createFailsWithLockedError() error {
	if w.createErr == nil {
		return fmt.Errorf("expected the create to fail with a database-locked error, but it succeeded")
	}
	if !strings.Contains(strings.ToLower(w.createErr.Error()), "lock") {
		return fmt.Errorf("expected a database-locked error, got: %w", w.createErr)
	}
	return nil
}

func (w *sqliteWriteWorld) writeGaveUpBounded() error {
	if w.createErr == nil {
		return fmt.Errorf("expected the create to fail after exhausting its retries")
	}
	if !strings.Contains(w.createErr.Error(), "gave up after 5 attempts") {
		return fmt.Errorf("expected a bounded 'gave up after 5 attempts' error, got: %w", w.createErr)
	}
	if n := w.busyAttempts.Load(); n != 5 {
		return fmt.Errorf("expected exactly 5 write attempts, got %d", n)
	}
	return nil
}

func (w *sqliteWriteWorld) observeBusyRetries() {
	w.busyAttempts.Store(0)
	weosgorm.SetBusyRetryObserver(func(int, error) { w.busyAttempts.Add(1) })
}

// holdWriteLock opens a second raw connection to the same database file and
// holds SQLite's single write lock via a BEGIN IMMEDIATE transaction. When
// release > 0 the lock is dropped after that delay from a goroutine; otherwise
// it is held until teardown.
func (w *sqliteWriteWorld) holdWriteLock(release time.Duration) error {
	raw, err := sql.Open("sqlite", w.dbPath+"?_pragma=busy_timeout(0)&_txlock=immediate")
	if err != nil {
		return fmt.Errorf("open lock-holding connection: %w", err)
	}
	raw.SetMaxOpenConns(1)
	if _, err := raw.Exec(`CREATE TABLE IF NOT EXISTS _write_lock_probe (x INTEGER)`); err != nil {
		_ = raw.Close()
		return fmt.Errorf("create lock-probe table: %w", err)
	}
	tx, err := raw.BeginTx(context.Background(), nil) // BEGIN IMMEDIATE takes the write lock
	if err != nil {
		_ = raw.Close()
		return fmt.Errorf("begin lock-holding transaction: %w", err)
	}
	if _, err := tx.Exec(`INSERT INTO _write_lock_probe (x) VALUES (1)`); err != nil {
		_ = tx.Rollback()
		_ = raw.Close()
		return fmt.Errorf("write inside lock-holding transaction: %w", err)
	}
	w.rawLockDB = raw
	w.rawLockTx = tx
	if release > 0 {
		go func() {
			time.Sleep(release)
			w.releaseLock()
		}()
	}
	return nil
}

func (w *sqliteWriteWorld) releaseLock() {
	w.rawLockOnce.Do(func() {
		if w.rawLockTx != nil {
			_ = w.rawLockTx.Rollback()
		}
	})
}

// --- Shared helpers ---

func (w *sqliteWriteWorld) createResource(ctx context.Context, typeSlug, name string) error {
	data := json.RawMessage(fmt.Sprintf(`{"name":%q}`, name))
	_, err := w.rs.Create(ctx, application.CreateResourceCommand{TypeSlug: typeSlug, Data: data})
	return err
}

func (w *sqliteWriteWorld) stopApp() {
	if w.app == nil {
		return
	}
	stopCtx, cancel := context.WithTimeout(context.Background(), fx.DefaultTimeout)
	defer cancel()
	_ = w.app.Stop(stopCtx)
	w.app = nil
}

func (w *sqliteWriteWorld) teardown() {
	w.stopBurst()
	w.releaseLock()
	if w.rawLockDB != nil {
		_ = w.rawLockDB.Close()
		w.rawLockDB = nil
	}
	weosgorm.SetBusyRetryObserver(nil)
	if w.healthServer != nil {
		w.healthServer.Close()
		w.healthServer = nil
	}
	w.stopApp()
	if w.tmpDir != "" {
		_ = os.RemoveAll(w.tmpDir)
		w.tmpDir = ""
	}
}

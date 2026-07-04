package e2e

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/wepala/weos/v3/application"
	"github.com/wepala/weos/v3/application/presets"
	"github.com/wepala/weos/v3/internal/config"

	pericarpdomain "github.com/akeemphilbert/pericarp/pkg/eventsourcing/domain"
	"github.com/cucumber/godog"
	"go.uber.org/fx"
)

// TestPostgresWriteConcurrency proves the SQLite write-serialization fix
// (feature #426) leaves the PostgreSQL path untouched: the connection pool
// stays concurrent and the busy-retry never engages. It requires a real
// PostgreSQL database supplied via TEST_POSTGRES_DSN and cleanly skips
// otherwise. Filter on demand with:
// GODOG_TAGS=@wip go test ./tests/e2e/ -run TestPostgresWriteConcurrency -v
func TestPostgresWriteConcurrency(t *testing.T) {
	if os.Getenv("TEST_POSTGRES_DSN") == "" {
		t.Skip("TEST_POSTGRES_DSN not set — postgres write-concurrency scenarios skipped")
	}
	tags := "~@wip"
	if override := os.Getenv("GODOG_TAGS"); override != "" {
		tags = override
	}
	suite := godog.TestSuite{
		Name:                "postgres-write-concurrency",
		ScenarioInitializer: initPostgresWriteConcurrencyScenario,
		Options: &godog.Options{
			Format:   "pretty",
			Paths:    []string{"features/postgres_write_concurrency.feature"},
			Tags:     tags,
			Strict:   true,
			TestingT: t,
		},
	}
	if suite.Run() != 0 {
		t.Fatal("postgres write-concurrency acceptance scenarios failed")
	}
}

// armedEventStore decorates the pericarp event store so a single scenario can
// force one write to fail with a permanent (non-BUSY) error and count how many
// times the write was attempted. It proves the PostgreSQL path does not retry.
type armedEventStore struct {
	pericarpdomain.EventStore
	armed       atomic.Bool
	appendCalls atomic.Int64
}

func (s *armedEventStore) Append(ctx context.Context, aggregateID string, expectedVersion int,
	events ...pericarpdomain.EventEnvelope[any]) error {
	s.appendCalls.Add(1)
	if s.armed.Load() {
		// A non-recoverable error (not SQLITE_BUSY): the busy-retry must ignore
		// it entirely, so the write is attempted exactly once.
		return fmt.Errorf("permanent write failure: non-recoverable")
	}
	return s.EventStore.Append(ctx, aggregateID, expectedVersion, events...)
}

// pgWorld boots the full application against a real PostgreSQL database. Each
// scenario boots a fresh app; the assertions are independent of any residual
// data (concurrent-connection sampling and the armed-append count), and the
// preset install is idempotent, so no per-scenario schema teardown is needed.
type pgWorld struct {
	app       *fx.App
	rs        application.ResourceService
	rts       application.ResourceTypeService
	sqlDB     *sql.DB
	failStore *armedEventStore

	createErrs []error
	createErr  error
	maxInUse   int64
}

func initPostgresWriteConcurrencyScenario(sc *godog.ScenarioContext) {
	w := &pgWorld{failStore: &armedEventStore{}}

	sc.After(func(ctx context.Context, _ *godog.Scenario, _ error) (context.Context, error) {
		w.teardown()
		return ctx, nil
	})

	sc.Step(`^a clean WeOS instance backed by a PostgreSQL database$`, w.aCleanInstance)
	sc.Step(`^the "([^"]*)" preset is installed$`, w.presetInstalled)
	sc.Step(`^(\d+) "([^"]*)" resources are created concurrently within one second$`, w.createConcurrently)
	sc.Step(`^every create succeeds$`, w.everyCreateSucceeds)
	sc.Step(`^more than one database connection served the writes at the same time$`, w.moreThanOneConnection)
	sc.Step(`^the next write to the database fails with a non-recoverable error$`, w.nextWriteFailsPermanently)
	sc.Step(`^I create a "([^"]*)" named "([^"]*)"$`, w.iCreateResourceNamed)
	sc.Step(`^the create fails$`, w.theCreateFails)
	sc.Step(`^the write was attempted exactly once$`, w.writeAttemptedExactlyOnce)
}

// --- Background ---

func (w *pgWorld) aCleanInstance() error {
	cfg := config.Default()
	cfg.DatabaseDSN = os.Getenv("TEST_POSTGRES_DSN")
	cfg.LogLevel = "error"

	var rs application.ResourceService
	var rts application.ResourceTypeService
	var sqlDB *sql.DB

	app := fx.New(
		fx.NopLogger,
		application.Module(cfg, presets.NewDefaultRegistry()),
		// Wrap the event store so the failing-write scenario can arm a single
		// permanent Append failure and count attempts. Inert until armed.
		fx.Decorate(func(inner pericarpdomain.EventStore) pericarpdomain.EventStore {
			w.failStore.EventStore = inner
			return w.failStore
		}),
		fx.Populate(&rs, &rts, &sqlDB),
	)
	startCtx, cancel := context.WithTimeout(context.Background(), fx.DefaultTimeout)
	defer cancel()
	if err := app.Start(startCtx); err != nil {
		return fmt.Errorf("failed to start app: %w", err)
	}
	w.app = app
	w.rs = rs
	w.rts = rts
	w.sqlDB = sqlDB
	return nil
}

func (w *pgWorld) presetInstalled(ctx context.Context, name string) error {
	if w.rts == nil {
		return fmt.Errorf("application not booted")
	}
	if _, err := w.rts.InstallPreset(ctx, name, true); err != nil {
		return fmt.Errorf("failed to install %q preset: %w", name, err)
	}
	return nil
}

// --- Concurrency ---

func (w *pgWorld) createConcurrently(count int, typeSlug string) error {
	var maxInUse atomic.Int64
	stop := make(chan struct{})
	var sampler sync.WaitGroup
	sampler.Add(1)
	go func() {
		defer sampler.Done()
		ticker := time.NewTicker(time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				inUse := int64(w.sqlDB.Stats().InUse)
				for {
					cur := maxInUse.Load()
					if inUse <= cur || maxInUse.CompareAndSwap(cur, inUse) {
						break
					}
				}
			}
		}
	}()

	errs := make([]error, count)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < count; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			errs[i] = w.createResource(context.Background(), typeSlug, fmt.Sprintf("Burst %s %02d", typeSlug, i))
		}(i)
	}
	close(start)
	wg.Wait()
	close(stop)
	sampler.Wait()

	w.createErrs = errs
	w.maxInUse = maxInUse.Load()
	return nil
}

func (w *pgWorld) everyCreateSucceeds() error {
	for i, err := range w.createErrs {
		if err != nil {
			return fmt.Errorf("create %d of %d failed: %w", i, len(w.createErrs), err)
		}
	}
	return nil
}

func (w *pgWorld) moreThanOneConnection() error {
	if w.maxInUse <= 1 {
		return fmt.Errorf("expected more than one connection in use during the burst, peak was %d", w.maxInUse)
	}
	return nil
}

// --- Non-recoverable failure is not retried ---

func (w *pgWorld) nextWriteFailsPermanently() error {
	w.failStore.appendCalls.Store(0)
	w.failStore.armed.Store(true)
	return nil
}

func (w *pgWorld) iCreateResourceNamed(typeSlug, name string) error {
	w.failStore.appendCalls.Store(0)
	w.createErr = w.createResource(context.Background(), typeSlug, name)
	return nil
}

func (w *pgWorld) theCreateFails() error {
	if w.createErr == nil {
		return fmt.Errorf("expected the create to fail, but it succeeded")
	}
	return nil
}

func (w *pgWorld) writeAttemptedExactlyOnce() error {
	if n := w.failStore.appendCalls.Load(); n != 1 {
		return fmt.Errorf("expected the write to be attempted exactly once, got %d attempts", n)
	}
	return nil
}

// --- Helpers ---

func (w *pgWorld) createResource(ctx context.Context, typeSlug, name string) error {
	data := json.RawMessage(fmt.Sprintf(`{"name":%q}`, name))
	_, err := w.rs.Create(ctx, application.CreateResourceCommand{TypeSlug: typeSlug, Data: data})
	return err
}

func (w *pgWorld) teardown() {
	w.failStore.armed.Store(false)
	if w.app != nil {
		stopCtx, cancel := context.WithTimeout(context.Background(), fx.DefaultTimeout)
		defer cancel()
		_ = w.app.Stop(stopCtx)
		w.app = nil
	}
}

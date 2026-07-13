package application

import (
	"context"
	"sync"
)

// BehaviorScratch is a per-call scratch bag a ResourceBehavior can use to pass
// data between hook phases of the SAME Create/Update call. For example, a
// BeforeCreate hook can strip a field out of the payload (so it never reaches
// schema validation or the projection) and stash it here for that same call's
// AfterCreate hook to consume once the entity has an ID.
//
// Why this exists rather than a field on the behavior: behaviors are
// process-wide singletons shared across concurrent Create/Update calls, so
// per-call state cannot live on the behavior itself without racing. The scratch
// is seeded fresh on the context at the top of each Create/Update, so every hook
// in one call shares one scratch and different calls never see each other's.
type BehaviorScratch struct {
	mu     sync.Mutex
	values map[string]any
}

// Set stores v under key, overwriting any previous value. Safe for concurrent
// use, though within a single call hooks run sequentially.
func (b *BehaviorScratch) Set(key string, v any) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.values == nil {
		b.values = make(map[string]any)
	}
	b.values[key] = v
}

// Get returns the value stored under key and whether it was present.
func (b *BehaviorScratch) Get(key string) (any, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	v, ok := b.values[key]
	return v, ok
}

type behaviorScratchKey struct{}

// ScratchFromContext returns the scratch bag seeded for the current
// Create/Update call, or nil if there is none. It is nil when called outside a
// resource-service Create/Update (e.g. from a List or a direct repository
// access), so callers must nil-check before use.
func ScratchFromContext(ctx context.Context) *BehaviorScratch {
	s, _ := ctx.Value(behaviorScratchKey{}).(*BehaviorScratch)
	return s
}

// withBehaviorScratch seeds a fresh scratch bag on ctx. Called once near the top
// of Create and Update so every behavior hook in that call shares one scratch.
func withBehaviorScratch(ctx context.Context) context.Context {
	return context.WithValue(ctx, behaviorScratchKey{}, &BehaviorScratch{})
}

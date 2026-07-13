package application

import (
	"context"
	"sync"
	"testing"
)

func TestBehaviorScratch_SetGetRoundTrip(t *testing.T) {
	var s BehaviorScratch
	s.Set("stripped", 42)

	got, ok := s.Get("stripped")
	if !ok {
		t.Fatal("expected key to be present")
	}
	if got != 42 {
		t.Fatalf("got %v, want 42", got)
	}

	if _, ok := s.Get("missing"); ok {
		t.Fatal("expected missing key to be absent")
	}
}

func TestScratchFromContext_BareContextReturnsNil(t *testing.T) {
	if s := ScratchFromContext(context.Background()); s != nil {
		t.Fatalf("expected nil scratch on bare context, got %v", s)
	}
}

func TestScratchFromContext_SeededIsUsable(t *testing.T) {
	ctx := withBehaviorScratch(context.Background())
	s := ScratchFromContext(ctx)
	if s == nil {
		t.Fatal("expected seeded scratch, got nil")
	}
	s.Set("k", "v")
	if got, ok := ScratchFromContext(ctx).Get("k"); !ok || got != "v" {
		t.Fatalf("expected round-trip through context, got %v ok=%v", got, ok)
	}
}

func TestWithBehaviorScratch_CallsAreIndependent(t *testing.T) {
	ctx1 := withBehaviorScratch(context.Background())
	ScratchFromContext(ctx1).Set("shared", "call-1")

	ctx2 := withBehaviorScratch(context.Background())
	if _, ok := ScratchFromContext(ctx2).Get("shared"); ok {
		t.Fatal("second call's scratch must not see the first call's data")
	}
}

func TestBehaviorScratch_ConcurrentUseIsRaceFree(t *testing.T) {
	var s BehaviorScratch
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(2)
		go func(n int) {
			defer wg.Done()
			s.Set("key", n)
		}(i)
		go func() {
			defer wg.Done()
			s.Get("key")
		}()
	}
	wg.Wait()
}

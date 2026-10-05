package cache_test

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/teandresmith/sqlgen/cache"
)

type stateRecord struct {
	from, to cache.CircuitState
}

func newStateRecorder() (func(from, to cache.CircuitState), func() []stateRecord) {
	var mu sync.Mutex
	var records []stateRecord
	cb := func(from, to cache.CircuitState) {
		mu.Lock()
		records = append(records, stateRecord{from, to})
		mu.Unlock()
	}
	snap := func() []stateRecord {
		mu.Lock()
		defer mu.Unlock()
		out := make([]stateRecord, len(records))
		copy(out, records)
		return out
	}
	return cb, snap
}

// --- Case (a) ---
func TestBreaker_ClosedRecordSuccessResetsCounter(t *testing.T) {
	b := cache.NewBreaker(cache.BreakerConfig{
		FailureThreshold:  3,
		ProbeInterval:     10 * time.Millisecond,
		HalfOpenMaxProbes: 1,
	})
	b.RecordFailure()
	b.RecordFailure()
	b.RecordSuccess() // must reset the 2-failure streak
	// Two more failures should not trip (counter is 2, not 4).
	b.RecordFailure()
	b.RecordFailure()
	if got := b.State(); got != cache.StateClosed {
		t.Errorf("State() = %v, want Closed after RecordSuccess reset", got)
	}
}

// --- Case (b) ---
func TestBreaker_ClosedAdvancesToNMinusOneWithoutTripping(t *testing.T) {
	b := cache.NewBreaker(cache.BreakerConfig{
		FailureThreshold:  5,
		ProbeInterval:     10 * time.Millisecond,
		HalfOpenMaxProbes: 1,
	})
	for range 4 {
		b.RecordFailure()
	}
	if got := b.State(); got != cache.StateClosed {
		t.Errorf("State() after %d < N failures = %v, want Closed", 4, got)
	}
}

// --- Case (c) ---
func TestBreaker_NthFailureTripsAndFiresStateChange(t *testing.T) {
	onChange, snap := newStateRecorder()
	b := cache.NewBreaker(cache.BreakerConfig{
		FailureThreshold:  3,
		ProbeInterval:     time.Hour,
		HalfOpenMaxProbes: 1,
		OnStateChange:     onChange,
	})
	for range 3 {
		b.RecordFailure()
	}
	if got := b.State(); got != cache.StateOpen {
		t.Errorf("State() after Nth failure = %v, want Open", got)
	}
	records := snap()
	if len(records) != 1 || records[0] != (stateRecord{cache.StateClosed, cache.StateOpen}) {
		t.Errorf("OnStateChange records = %v, want [{Closed Open}]", records)
	}
}

// --- Case (d) ---
func TestBreaker_OpenAllowReturnsFalseUntilProbeIntervalElapses(t *testing.T) {
	b := cache.NewBreaker(cache.BreakerConfig{
		FailureThreshold:  1,
		ProbeInterval:     50 * time.Millisecond,
		HalfOpenMaxProbes: 1,
	})
	b.RecordFailure() // trip immediately
	if got := b.State(); got != cache.StateOpen {
		t.Fatalf("State() after tripping = %v, want Open", got)
	}
	if b.Allow() {
		t.Errorf("Allow() immediately after trip = true, want false")
	}
	if got := b.State(); got != cache.StateOpen {
		t.Errorf("State() after early Allow() = %v, want Open (no transition)", got)
	}
}

// --- Case (e) ---
func TestBreaker_AllowPostProbeIntervalTransitionsToHalfOpen(t *testing.T) {
	onChange, snap := newStateRecorder()
	b := cache.NewBreaker(cache.BreakerConfig{
		FailureThreshold:  1,
		ProbeInterval:     10 * time.Millisecond,
		HalfOpenMaxProbes: 1,
		OnStateChange:     onChange,
	})
	b.RecordFailure()
	time.Sleep(25 * time.Millisecond)
	if !b.Allow() {
		t.Fatalf("Allow() after ProbeInterval = false, want true (HalfOpen probe)")
	}
	if got := b.State(); got != cache.StateHalfOpen {
		t.Errorf("State() after Allow() post-interval = %v, want HalfOpen", got)
	}
	records := snap()
	// Expect Closed→Open then Open→HalfOpen.
	want := []stateRecord{
		{cache.StateClosed, cache.StateOpen},
		{cache.StateOpen, cache.StateHalfOpen},
	}
	if len(records) != len(want) {
		t.Fatalf("OnStateChange records = %v, want %v", records, want)
	}
	for i := range records {
		if records[i] != want[i] {
			t.Errorf("OnStateChange[%d] = %v, want %v", i, records[i], want[i])
		}
	}
}

// --- Case (f) ---
func TestBreaker_HalfOpenSuccessReturnsToClosed(t *testing.T) {
	b := cache.NewBreaker(cache.BreakerConfig{
		FailureThreshold:  2,
		ProbeInterval:     10 * time.Millisecond,
		HalfOpenMaxProbes: 1,
	})
	b.RecordFailure()
	b.RecordFailure()
	time.Sleep(25 * time.Millisecond)
	if !b.Allow() {
		t.Fatalf("Allow() post-interval = false, want true")
	}
	b.RecordSuccess()
	if got := b.State(); got != cache.StateClosed {
		t.Errorf("State() after HalfOpen success = %v, want Closed", got)
	}
	// Counter must be reset: N-1 failures must not trip.
	b.RecordFailure()
	if got := b.State(); got != cache.StateClosed {
		t.Errorf("State() after single post-close failure = %v, want Closed (counter reset)", got)
	}
}

// --- Case (g) ---
func TestBreaker_HalfOpenFailureReopensImmediately(t *testing.T) {
	onChange, snap := newStateRecorder()
	b := cache.NewBreaker(cache.BreakerConfig{
		FailureThreshold:  1,
		ProbeInterval:     10 * time.Millisecond,
		HalfOpenMaxProbes: 1,
		OnStateChange:     onChange,
	})
	b.RecordFailure()
	time.Sleep(25 * time.Millisecond)
	if !b.Allow() {
		t.Fatalf("Allow() post-interval = false, want true")
	}
	b.RecordFailure()
	if got := b.State(); got != cache.StateOpen {
		t.Errorf("State() after HalfOpen failure = %v, want Open", got)
	}
	// Final record should be HalfOpen → Open.
	records := snap()
	last := records[len(records)-1]
	if last != (stateRecord{cache.StateHalfOpen, cache.StateOpen}) {
		t.Errorf("last OnStateChange = %v, want {HalfOpen Open}", last)
	}
}

// --- Case (h) ---
func TestBreaker_HalfOpenProbesInFlightSaturationBypassesFollowers(t *testing.T) {
	b := cache.NewBreaker(cache.BreakerConfig{
		FailureThreshold:  1,
		ProbeInterval:     10 * time.Millisecond,
		HalfOpenMaxProbes: 2,
	})
	b.RecordFailure()
	time.Sleep(25 * time.Millisecond)
	// First two Allow() calls in HalfOpen must succeed.
	if !b.Allow() {
		t.Fatalf("1st Allow() in HalfOpen = false, want true")
	}
	if !b.Allow() {
		t.Fatalf("2nd Allow() in HalfOpen = false, want true")
	}
	// 3rd caller must be bypassed.
	if b.Allow() {
		t.Errorf("3rd Allow() in HalfOpen = true, want false (saturation)")
	}
	if got := b.State(); got != cache.StateHalfOpen {
		t.Errorf("State() still after saturation = %v, want HalfOpen", got)
	}
}

// --- Case (i) ---
func TestBreaker_OpenIgnoresRecordSuccessAndRecordFailure(t *testing.T) {
	onChange, snap := newStateRecorder()
	b := cache.NewBreaker(cache.BreakerConfig{
		FailureThreshold:  1,
		ProbeInterval:     time.Hour, // do not auto-transition
		HalfOpenMaxProbes: 1,
		OnStateChange:     onChange,
	})
	b.RecordFailure() // trip Closed → Open
	b.RecordSuccess() // must be ignored
	b.RecordFailure() // must be ignored
	if got := b.State(); got != cache.StateOpen {
		t.Errorf("State() after ignored records = %v, want Open", got)
	}
	records := snap()
	// Only Closed → Open is allowed.
	if len(records) != 1 {
		t.Errorf("OnStateChange records = %v, want only [{Closed Open}]", records)
	}
}

// --- Case (j) ---
func TestBreaker_ConcurrentAllowAndRecordRaceFree(t *testing.T) {
	b := cache.NewBreaker(cache.BreakerConfig{
		FailureThreshold:  100,
		ProbeInterval:     5 * time.Millisecond,
		HalfOpenMaxProbes: 4,
	})

	var wg sync.WaitGroup
	var ops atomic.Int64
	for range 50 {
		wg.Add(3)
		go func() {
			defer wg.Done()
			for range 100 {
				b.Allow()
				ops.Add(1)
			}
		}()
		go func() {
			defer wg.Done()
			for range 100 {
				b.RecordSuccess()
				ops.Add(1)
			}
		}()
		go func() {
			defer wg.Done()
			for range 100 {
				b.RecordFailure()
				ops.Add(1)
			}
		}()
	}
	wg.Wait()

	// State must be a valid enum value after the storm.
	switch b.State() {
	case cache.StateClosed, cache.StateOpen, cache.StateHalfOpen:
		// ok
	default:
		t.Errorf("State() after concurrent storm = %v, want valid state", b.State())
	}
	if ops.Load() == 0 {
		t.Errorf("expected ops > 0, got 0")
	}
}

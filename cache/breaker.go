package cache

import (
	"sync"
	"time"
)

// CircuitState represents the state of the cache circuit breaker.
type CircuitState int

// Circuit breaker states.
const (
	StateClosed CircuitState = iota
	StateOpen
	StateHalfOpen
)

// String returns the human-readable name of the state.
func (s CircuitState) String() string {
	switch s {
	case StateClosed:
		return "closed"
	case StateOpen:
		return "open"
	case StateHalfOpen:
		return "half_open"
	default:
		return "unknown"
	}
}

// BreakerConfig configures a Breaker.
//
// FailureThreshold is the number of consecutive failures in Closed that
// trip the breaker to Open. ProbeInterval is how long Open waits before
// Allow() transitions to HalfOpen. HalfOpenMaxProbes caps the number of
// concurrent probes allowed in HalfOpen; additional callers bypass.
// OnStateChange, if non-nil, is invoked outside the Breaker's internal
// lock after every state transition.
type BreakerConfig struct {
	FailureThreshold  int
	ProbeInterval     time.Duration
	HalfOpenMaxProbes int
	OnStateChange     func(from, to CircuitState)
}

// Breaker is the reusable circuit breaker used by the generated cache
// facade. A single Breaker is shared across every cached table on the
// same *Cache — the breaker is *Cache-scoped, not table-scoped.
//
// Allow is the state-transition clock for the Open → HalfOpen edge; no
// background goroutine is started. Callers that receive Allow() == true
// in HalfOpen MUST eventually invoke RecordSuccess or RecordFailure to
// release the probe slot. Records received while in Open are ignored;
// counter is only meaningful in Closed.
type Breaker struct {
	cfg BreakerConfig

	mu             sync.Mutex
	state          CircuitState
	counter        int
	openedAt       time.Time
	probesInFlight int
	now            func() time.Time
}

// NewBreaker returns a Breaker initialized in the Closed state.
func NewBreaker(cfg BreakerConfig) *Breaker {
	return &Breaker{
		cfg:   cfg,
		state: StateClosed,
		now:   time.Now,
	}
}

// State returns the current breaker state.
func (b *Breaker) State() CircuitState {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.state
}

// Allow reports whether the next cache call may proceed. When Open,
// Allow returns false until ProbeInterval has elapsed; the first caller
// past that point observes the transition to HalfOpen and consumes the
// first probe slot (returning true when HalfOpenMaxProbes ≥ 1).
// Subsequent callers in HalfOpen receive false until RecordSuccess /
// RecordFailure releases a slot.
func (b *Breaker) Allow() bool {
	b.mu.Lock()
	from := b.state

	if b.state == StateOpen && b.now().Sub(b.openedAt) >= b.cfg.ProbeInterval {
		b.state = StateHalfOpen
		b.probesInFlight = 0
	}

	var allow bool
	switch b.state {
	case StateClosed:
		allow = true
	case StateOpen:
		allow = false
	case StateHalfOpen:
		if b.probesInFlight < b.cfg.HalfOpenMaxProbes {
			b.probesInFlight++
			allow = true
		}
	}

	to := b.state
	cb := b.cfg.OnStateChange
	b.mu.Unlock()

	if from != to && cb != nil {
		cb(from, to)
	}
	return allow
}

// RecordSuccess reports a successful cache call. In Closed the consecutive
// failure counter resets to zero. In HalfOpen a single success closes
// the breaker. In Open the record is ignored — Open transitions are
// driven by Allow, not RecordX.
func (b *Breaker) RecordSuccess() {
	b.mu.Lock()
	from := b.state
	switch b.state {
	case StateClosed:
		b.counter = 0
	case StateOpen:
		// ignored
	case StateHalfOpen:
		b.state = StateClosed
		b.counter = 0
		b.probesInFlight = 0
	}
	to := b.state
	cb := b.cfg.OnStateChange
	b.mu.Unlock()

	if from != to && cb != nil {
		cb(from, to)
	}
}

// RecordFailure reports a failed cache call. In Closed it increments the
// consecutive failure counter and trips to Open on the Nth consecutive
// failure. In HalfOpen a single failure re-opens the breaker. In Open
// the record is ignored.
func (b *Breaker) RecordFailure() {
	b.mu.Lock()
	from := b.state
	switch b.state {
	case StateClosed:
		b.counter++
		if b.counter >= b.cfg.FailureThreshold {
			b.state = StateOpen
			b.openedAt = b.now()
		}
	case StateOpen:
		// ignored
	case StateHalfOpen:
		b.state = StateOpen
		b.counter = b.cfg.FailureThreshold
		b.openedAt = b.now()
		b.probesInFlight = 0
	}
	to := b.state
	cb := b.cfg.OnStateChange
	b.mu.Unlock()

	if from != to && cb != nil {
		cb(from, to)
	}
}

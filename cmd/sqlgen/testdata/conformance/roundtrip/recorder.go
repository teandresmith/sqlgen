// Package roundtrip measures how many database round trips each generated
// client operation makes, for docs/round-trips.md. Each conformance dialect
// module opens a second connection pool whose driver reports every statement
// to a Recorder, transaction statements included, and runs its suite's
// round-trip cases on a client built over that pool. Run compares the
// measurements with the committed document, or writes them out for
// `make round-trips` to assemble.
package roundtrip

import (
	"cmp"
	"os"
	"slices"
	"strings"
	"sync"
	"time"
)

// DefaultDelay is the delay a Recorder adds to every statement it records
// when SQLGEN_ROUND_TRIP_DELAY does not set one.
const DefaultDelay = 25 * time.Millisecond

// DelayEnv names the environment variable that overrides DefaultDelay with
// a Go duration, such as 50ms.
const DelayEnv = "SQLGEN_ROUND_TRIP_DELAY"

// Recorder records the statements a driver sends while it is on. Each
// recorded statement is held for a fixed delay before its result returns, so
// statements sent one after another never overlap and statements sent
// concurrently always do, whatever the database's own latency.
type Recorder struct {
	delay time.Duration

	mu     sync.Mutex
	on     bool
	tx     bool
	spans  []span
	origin time.Time
}

// span is one round trip: the statements sent together and when the round
// trip started and ended.
type span struct {
	sqls       []string
	start, end time.Duration
}

// NewRecorder returns a Recorder, off, with the delay DelayEnv sets or
// DefaultDelay.
func NewRecorder() (*Recorder, error) {
	delay := DefaultDelay
	if v := os.Getenv(DelayEnv); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil || d <= 0 {
			return nil, &delayError{value: v}
		}
		delay = d
	}
	return &Recorder{delay: delay}, nil
}

type delayError struct{ value string }

func (e *delayError) Error() string {
	return DelayEnv + "=" + e.value + " is not a positive Go duration"
}

// Delay is the delay each recorded statement is held for.
func (r *Recorder) Delay() time.Duration { return r.delay }

// Start clears the recorded statements and turns recording on.
func (r *Recorder) Start() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.on, r.tx, r.spans, r.origin = true, false, nil, time.Now()
}

// Stop turns recording off and returns what was recorded since Start.
func (r *Recorder) Stop() Measurement {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.on = false
	m := Measurement{Tx: r.tx, Rounds: rounds(r.spans)}
	for _, s := range r.spans {
		m.Statements += len(s.sqls)
		m.SQL = append(m.SQL, s.sqls...)
	}
	return m
}

// Statement is called as sql is sent. The returned func is called once its
// result is back; it holds the caller for the delay, then records the round
// trip. Transaction control is not recorded (IsTxControl), and nothing is
// while the Recorder is off.
func (r *Recorder) Statement(sql string) func() { return r.Batch([]string{sql}) }

// Batch is Statement for statements sent together in one round trip, such
// as a pgx batch.
func (r *Recorder) Batch(sqls []string) func() {
	r.mu.Lock()
	if !r.on {
		r.mu.Unlock()
		return func() {}
	}
	var kept []string
	for _, s := range sqls {
		switch kind := txControl(s); kind {
		case txBegin:
			r.tx = true
		case txOther:
		default:
			kept = append(kept, s)
		}
	}
	if len(kept) == 0 {
		r.mu.Unlock()
		return func() {}
	}
	start := time.Since(r.origin)
	r.mu.Unlock()
	return func() {
		time.Sleep(r.delay)
		r.mu.Lock()
		defer r.mu.Unlock()
		if r.on {
			r.spans = append(r.spans, span{sqls: kept, start: start, end: time.Since(r.origin)})
		}
	}
}

// BeganTx records that a transaction began, for a driver whose BEGIN is not a
// statement it sends through the Recorder.
func (r *Recorder) BeganTx() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.on {
		r.tx = true
	}
}

// Measurement is what one operation sent.
type Measurement struct {
	// Statements counts every statement sent, transaction control excluded.
	Statements int
	// Rounds is the length of the longest chain of round trips each of which
	// started after the one before it ended: the sequential waves on the
	// critical path, which latency scales with.
	Rounds int
	// Tx is set when the operation began a transaction.
	Tx bool
	// SQL is each statement, in the order its round trip ended.
	SQL []string
}

// rounds returns the length of the longest chain of spans in which each span
// starts at or after the previous one ends. Spans that overlap ran
// concurrently and share a wave; a span that starts after another ended
// waited for it, or could have.
func rounds(spans []span) int {
	sorted := slices.Clone(spans)
	slices.SortFunc(sorted, func(a, b span) int { return cmp.Compare(a.start, b.start) })
	depth := make([]int, len(sorted))
	best := 0
	for i, s := range sorted {
		depth[i] = 1
		for j := range i {
			if sorted[j].end <= s.start && depth[j]+1 > depth[i] {
				depth[i] = depth[j] + 1
			}
		}
		best = max(best, depth[i])
	}
	return best
}

type txKind int

const (
	txNone  txKind = iota // not transaction control
	txBegin               // begins a transaction
	txOther               // commits, rolls back, or manages a savepoint
)

// IsTxControl reports whether sql only controls a transaction: BEGIN, START
// TRANSACTION, COMMIT, END, ROLLBACK, SAVEPOINT or RELEASE. The report does
// not count these as round trips.
func IsTxControl(sql string) bool { return txControl(sql) != txNone }

func txControl(sql string) txKind {
	word, _, _ := strings.Cut(strings.TrimSpace(sql), " ")
	switch strings.ToUpper(strings.TrimRight(word, ";")) {
	case "BEGIN", "START":
		return txBegin
	case "COMMIT", "END", "ROLLBACK", "SAVEPOINT", "RELEASE":
		return txOther
	}
	return txNone
}

package roundtrip

import (
	"sync"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
)

func TestRounds(t *testing.T) {
	ms := time.Millisecond
	tests := []struct {
		name  string
		spans []span
		want  int
	}{
		{name: "nothing sent", want: 0},
		{name: "one statement", spans: []span{{start: 0, end: 20 * ms}}, want: 1},
		{
			name:  "two in sequence",
			spans: []span{{start: 0, end: 20 * ms}, {start: 21 * ms, end: 41 * ms}},
			want:  2,
		},
		{
			name:  "a statement that starts as the one before ends waited for it",
			spans: []span{{start: 0, end: 20 * ms}, {start: 20 * ms, end: 40 * ms}},
			want:  2,
		},
		{
			name:  "a parent then two children in parallel",
			spans: []span{{start: 0, end: 20 * ms}, {start: 21 * ms, end: 41 * ms}, {start: 22 * ms, end: 42 * ms}},
			want:  2,
		},
		{
			name: "a parallel child with a child of its own is the critical path",
			spans: []span{
				{start: 0, end: 20 * ms},
				{start: 21 * ms, end: 41 * ms},
				{start: 21 * ms, end: 41 * ms},
				{start: 42 * ms, end: 62 * ms},
			},
			want: 3,
		},
		{
			name:  "listed out of start order",
			spans: []span{{start: 42 * ms, end: 62 * ms}, {start: 0, end: 20 * ms}, {start: 21 * ms, end: 41 * ms}},
			want:  3,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := rounds(tt.spans); got != tt.want {
				t.Errorf("rounds(%v) = %d, want %d", tt.spans, got, tt.want)
			}
		})
	}
}

func TestIsTxControl(t *testing.T) {
	tests := []struct {
		sql  string
		want bool
	}{
		{"BEGIN", true},
		{"begin isolation level serializable", true},
		{"START TRANSACTION", true},
		{"commit", true},
		{"END", true},
		{"ROLLBACK", true},
		{"ROLLBACK TO SAVEPOINT sp_1", true},
		{"SAVEPOINT sp_1", true},
		{"RELEASE SAVEPOINT sp_1", true},
		{"  COMMIT;", true},
		{"SELECT 1", false},
		{`INSERT INTO "begin" VALUES (1)`, false},
		{"UPDATE t SET a = 1", false},
		{"WITH x AS (SELECT 1) SELECT * FROM x", false},
	}
	for _, tt := range tests {
		t.Run(tt.sql, func(t *testing.T) {
			if got := IsTxControl(tt.sql); got != tt.want {
				t.Errorf("IsTxControl(%q) = %v, want %v", tt.sql, got, tt.want)
			}
		})
	}
}

func TestRecorder(t *testing.T) {
	r := &Recorder{delay: 5 * time.Millisecond}

	r.Statement("SELECT before start")()
	r.Start()
	r.Statement("BEGIN")()
	r.Statement("SELECT 1")()
	var wg sync.WaitGroup
	for _, sql := range []string{"SELECT 2", "SELECT 3"} {
		done := r.Statement(sql)
		wg.Go(done)
	}
	wg.Wait()
	r.Batch([]string{"INSERT 1", "INSERT 2", "COMMIT"})()
	got := r.Stop()
	r.Statement("SELECT after stop")()

	if got.Statements != 5 || got.Rounds != 3 || !got.Tx {
		t.Errorf("Stop() = %d statements in %d rounds, tx %v, want 5 in 3, tx true", got.Statements, got.Rounds, got.Tx)
	}
	want := []string{"SELECT 1", "SELECT 2", "SELECT 3", "INSERT 1", "INSERT 2"}
	if len(got.SQL) == len(want) && got.SQL[1] > got.SQL[2] {
		got.SQL[1], got.SQL[2] = got.SQL[2], got.SQL[1]
	}
	if diff := cmp.Diff(want, got.SQL); diff != "" {
		t.Errorf("Stop().SQL (-want +got):\n%s", diff)
	}

	r.Start()
	r.BeganTx()
	if m := r.Stop(); m.Statements != 0 || m.Rounds != 0 || !m.Tx {
		t.Errorf("after BeganTx alone, Stop() = %+v, want no statement and Tx", m)
	}
}

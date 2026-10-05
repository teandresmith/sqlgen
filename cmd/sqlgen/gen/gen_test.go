package gen_test

import (
	"errors"
	"testing"
	"text/template"

	"github.com/teandresmith/sqlgen/cmd/sqlgen/gen"
)

func TestDefaultSteps_order(t *testing.T) {
	steps := gen.DefaultSteps()

	wantNames := []string{
		"enums",
		"types",
		"errors",
		"tablenames",
		"tables",
		"sorters",
		"pagination",
		"connections",
		"views",
		"client",
	}

	if len(steps) != len(wantNames) {
		t.Fatalf("DefaultSteps() returned %d steps, want %d", len(steps), len(wantNames))
	}

	for i, want := range wantNames {
		if steps[i].Name != want {
			t.Errorf("DefaultSteps()[%d].Name = %q, want %q", i, steps[i].Name, want)
		}
	}
}

func TestRun_executesInOrder(t *testing.T) {
	var order []string

	steps := []gen.Step{
		{Name: "first", Fn: func(_ *template.Template, _ *gen.Options) error {
			order = append(order, "first")
			return nil
		}},
		{Name: "second", Fn: func(_ *template.Template, _ *gen.Options) error {
			order = append(order, "second")
			return nil
		}},
		{Name: "third", Fn: func(_ *template.Template, _ *gen.Options) error {
			order = append(order, "third")
			return nil
		}},
	}

	if err := gen.Run(steps, nil, &gen.Options{}); err != nil {
		t.Fatalf("Run() unexpected error: %v", err)
	}

	want := []string{"first", "second", "third"}
	if len(order) != len(want) {
		t.Fatalf("Run() executed %d steps, want %d", len(order), len(want))
	}
	for i, w := range want {
		if order[i] != w {
			t.Errorf("Run() step %d = %q, want %q", i, order[i], w)
		}
	}
}

func TestRun_stopsOnError(t *testing.T) {
	stepErr := errors.New("step failed")
	var executed []string

	steps := []gen.Step{
		{Name: "ok", Fn: func(_ *template.Template, _ *gen.Options) error {
			executed = append(executed, "ok")
			return nil
		}},
		{Name: "fail", Fn: func(_ *template.Template, _ *gen.Options) error {
			executed = append(executed, "fail")
			return stepErr
		}},
		{Name: "skipped", Fn: func(_ *template.Template, _ *gen.Options) error {
			executed = append(executed, "skipped")
			return nil
		}},
	}

	err := gen.Run(steps, nil, &gen.Options{})
	if err == nil {
		t.Fatal("Run() expected error, got nil")
	}
	if !errors.Is(err, stepErr) {
		t.Errorf("Run() error = %v, want wrapping %v", err, stepErr)
	}

	// "skipped" should not have run.
	if len(executed) != 2 {
		t.Errorf("Run() executed %d steps, want 2; got %v", len(executed), executed)
	}
}

func TestRun_errorIncludesStepName(t *testing.T) {
	steps := []gen.Step{
		{Name: "tables", Fn: func(_ *template.Template, _ *gen.Options) error {
			return errors.New("template syntax error")
		}},
	}

	err := gen.Run(steps, nil, &gen.Options{})
	if err == nil {
		t.Fatal("Run() expected error, got nil")
	}

	want := "generation step tables: template syntax error"
	if err.Error() != want {
		t.Errorf("Run() error = %q, want %q", err.Error(), want)
	}
}

func TestRun_emptySteps(t *testing.T) {
	if err := gen.Run(nil, nil, &gen.Options{}); err != nil {
		t.Errorf("Run(nil) unexpected error: %v", err)
	}
	if err := gen.Run([]gen.Step{}, nil, &gen.Options{}); err != nil {
		t.Errorf("Run([]) unexpected error: %v", err)
	}
}

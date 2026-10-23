package reqcheck_test

import (
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/teandresmith/sqlgen/cmd/sqlgen/internal/reqcheck"
)

const samplePRD = `# Title

## Table of Contents

- [1. Overview](#1-overview)

## 1. Overview

Intro.

### Core Pipeline

Unnumbered, belongs to §1.

## 9. Generated Client Operations

### 9.4c Nil input handling

GetMany(ctx, nil) is GetMany(ctx, &Input{}).

#### 9.4c.1 Detail

Detail text.

` + "```yaml" + `
## 99. Not a heading
` + "```" + `

### 9.5 Edge Case Behaviors

Edge cases.

## Appendix A: SQL Builder Functions

Builders.
`

func TestParsePRD_Sections(t *testing.T) {
	p, err := reqcheck.ParsePRD(samplePRD)
	if err != nil {
		t.Fatalf("ParsePRD() unexpected error: %v", err)
	}
	type sec struct {
		Key, Title string
		Level      int
	}
	got := make([]sec, 0, len(p.Sections))
	for _, s := range p.Sections {
		got = append(got, sec{s.Key, s.Title, s.Level})
	}
	want := []sec{
		{"", "Title", 1},
		{"Table of Contents", "Table of Contents", 2},
		{"1", "Overview", 2},
		{"", "Core Pipeline", 3},
		{"9", "Generated Client Operations", 2},
		{"9.4c", "Nil input handling", 3},
		{"9.4c.1", "Detail", 4},
		{"9.5", "Edge Case Behaviors", 3},
		{"Appendix A", "SQL Builder Functions", 2},
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("ParsePRD() sections mismatch (-want +got):\n%s", diff)
	}
}

func TestParsePRD_HashCoversSubsectionsOnly(t *testing.T) {
	base, err := reqcheck.ParsePRD(samplePRD)
	if err != nil {
		t.Fatalf("ParsePRD() unexpected error: %v", err)
	}
	tests := []struct {
		name      string
		edit      string // replaces "Detail text."
		changed   []string
		unchanged []string
	}{
		{
			name:      "subsection edit changes its ancestors",
			edit:      "Detail text, revised.",
			changed:   []string{"9.4c.1", "9.4c", "9"},
			unchanged: []string{"9.5", "1", "Appendix A"},
		},
		{
			name:      "identical text keeps every hash",
			edit:      "Detail text.",
			unchanged: []string{"9.4c.1", "9.4c", "9", "9.5"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, err := reqcheck.ParsePRD(strings.Replace(samplePRD, "Detail text.", tt.edit, 1))
			if err != nil {
				t.Fatalf("ParsePRD() unexpected error: %v", err)
			}
			for _, key := range tt.changed {
				before, _ := base.Section(key)
				after, _ := p.Section(key)
				if before.Hash == after.Hash {
					t.Errorf("Section(%q).Hash unchanged after %q, want changed", key, tt.edit)
				}
			}
			for _, key := range tt.unchanged {
				before, _ := base.Section(key)
				after, _ := p.Section(key)
				if before.Hash != after.Hash {
					t.Errorf("Section(%q).Hash changed after %q, want unchanged", key, tt.edit)
				}
			}
		})
	}
}

func TestParsePRD_DuplicateNumberIsAnError(t *testing.T) {
	_, err := reqcheck.ParsePRD("## 4. Config\n\n### 4.1 Root\n\n### 4.1 Again\n")
	if err == nil || !strings.Contains(err.Error(), `"4.1"`) {
		t.Errorf("ParsePRD(duplicate 4.1) error = %v, want one naming section 4.1", err)
	}
}

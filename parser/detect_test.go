package parser_test

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/teandresmith/sqlgen/parser"
)

// DetectSoftDelete matches by name only. The column's SQL type plays no part:
// the strategy is the configured type, and config validation owns the
// compatibility check.
func TestDetectSoftDelete(t *testing.T) {
	tests := []struct {
		name    string
		table   parser.Table
		columns []string
		want    string
	}{
		{
			name: "priority ordering: deleted_at wins over is_deleted",
			table: parser.Table{
				Name: "users",
				Columns: []parser.Column{
					{Name: "id", Type: "integer"},
					{Name: "deleted_at", Type: "timestamp"},
					{Name: "is_deleted", Type: "boolean"},
				},
			},
			columns: []string{"deleted_at", "is_deleted"},
			want:    "deleted_at",
		},
		{
			name: "priority: skips missing column, finds second match",
			table: parser.Table{
				Name: "users",
				Columns: []parser.Column{
					{Name: "id", Type: "integer"},
					{Name: "is_deleted", Type: "boolean"},
				},
			},
			columns: []string{"deleted_at", "is_deleted"},
			want:    "is_deleted",
		},
		{
			name: "type is not checked: varchar still matches",
			table: parser.Table{
				Name: "users",
				Columns: []parser.Column{
					{Name: "id", Type: "integer"},
					{Name: "deleted_at", Type: "varchar(255)"},
				},
			},
			columns: []string{"deleted_at"},
			want:    "deleted_at",
		},
		{
			name: "no match: column not found",
			table: parser.Table{
				Name: "products",
				Columns: []parser.Column{
					{Name: "id", Type: "integer"},
					{Name: "name", Type: "text"},
				},
			},
			columns: []string{"deleted_at", "is_deleted"},
			want:    "",
		},
		{
			name: "no match: empty columns list",
			table: parser.Table{
				Name: "products",
				Columns: []parser.Column{
					{Name: "id", Type: "integer"},
					{Name: "deleted_at", Type: "timestamp"},
				},
			},
			columns: nil,
			want:    "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := parser.DetectSoftDelete(&tt.table, tt.columns); got != tt.want {
				t.Errorf("DetectSoftDelete() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestDetectUpdateColumns(t *testing.T) {
	tests := []struct {
		name  string
		table parser.Table
		names []string
		want  []string
	}{
		{
			name: "returns all matching columns in config order",
			table: parser.Table{
				Name: "orders",
				Columns: []parser.Column{
					{Name: "id", Type: "integer"},
					{Name: "updated_at", Type: "timestamp"},
					{Name: "last_modified_at", Type: "timestamp"},
				},
			},
			names: []string{"updated_at", "updated_datetime", "last_modified_at", "modified_on"},
			want:  []string{"updated_at", "last_modified_at"},
		},
		{
			name: "no match returns nil",
			table: parser.Table{
				Name: "products",
				Columns: []parser.Column{
					{Name: "id", Type: "integer"},
					{Name: "name", Type: "text"},
				},
			},
			names: []string{"updated_at", "last_modified_at"},
			want:  nil,
		},
		{
			name: "empty names list returns nil",
			table: parser.Table{
				Name: "products",
				Columns: []parser.Column{
					{Name: "id", Type: "integer"},
					{Name: "updated_at", Type: "timestamp"},
				},
			},
			names: nil,
			want:  nil,
		},
		{
			name: "single match",
			table: parser.Table{
				Name: "users",
				Columns: []parser.Column{
					{Name: "id", Type: "integer"},
					{Name: "modified_on", Type: "datetime"},
				},
			},
			names: []string{"updated_at", "updated_datetime", "last_modified_at", "modified_on"},
			want:  []string{"modified_on"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parser.DetectUpdateColumns(&tt.table, tt.names)
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("DetectUpdateColumns() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

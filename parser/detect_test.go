package parser_test

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/teandresmith/sqlgen/parser"
)

func TestDetectSoftDelete(t *testing.T) {
	tests := []struct {
		name         string
		table        parser.Table
		columns      []string
		wantColumn   string
		wantStrategy string
		wantErr      bool
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
			columns:      []string{"deleted_at", "is_deleted"},
			wantColumn:   "deleted_at",
			wantStrategy: "timestamp",
		},
		{
			name: "timestamp column: timestamp",
			table: parser.Table{
				Name: "posts",
				Columns: []parser.Column{
					{Name: "id", Type: "integer"},
					{Name: "deleted_at", Type: "timestamp"},
				},
			},
			columns:      []string{"deleted_at"},
			wantColumn:   "deleted_at",
			wantStrategy: "timestamp",
		},
		{
			name: "timestamp column: timestamptz",
			table: parser.Table{
				Name: "posts",
				Columns: []parser.Column{
					{Name: "id", Type: "integer"},
					{Name: "deleted_at", Type: "timestamptz"},
				},
			},
			columns:      []string{"deleted_at"},
			wantColumn:   "deleted_at",
			wantStrategy: "timestamp",
		},
		{
			name: "timestamp column: datetime",
			table: parser.Table{
				Name: "posts",
				Columns: []parser.Column{
					{Name: "id", Type: "integer"},
					{Name: "deleted_datetime", Type: "datetime"},
				},
			},
			columns:      []string{"deleted_datetime"},
			wantColumn:   "deleted_datetime",
			wantStrategy: "timestamp",
		},
		{
			name: "timestamp column: timestamp without time zone",
			table: parser.Table{
				Name: "posts",
				Columns: []parser.Column{
					{Name: "id", Type: "integer"},
					{Name: "deleted_at", Type: "timestamp without time zone"},
				},
			},
			columns:      []string{"deleted_at"},
			wantColumn:   "deleted_at",
			wantStrategy: "timestamp",
		},
		{
			name: "bool column: boolean",
			table: parser.Table{
				Name: "users",
				Columns: []parser.Column{
					{Name: "id", Type: "integer"},
					{Name: "is_deleted", Type: "boolean"},
				},
			},
			columns:      []string{"is_deleted"},
			wantColumn:   "is_deleted",
			wantStrategy: "bool",
		},
		{
			name: "bool column: bool",
			table: parser.Table{
				Name: "users",
				Columns: []parser.Column{
					{Name: "id", Type: "integer"},
					{Name: "is_deleted", Type: "bool"},
				},
			},
			columns:      []string{"is_deleted"},
			wantColumn:   "is_deleted",
			wantStrategy: "bool",
		},
		{
			name: "integer column: integer",
			table: parser.Table{
				Name: "records",
				Columns: []parser.Column{
					{Name: "id", Type: "integer"},
					{Name: "deleted", Type: "integer"},
				},
			},
			columns:      []string{"deleted"},
			wantColumn:   "deleted",
			wantStrategy: "integer",
		},
		{
			name: "integer column: smallint",
			table: parser.Table{
				Name: "records",
				Columns: []parser.Column{
					{Name: "id", Type: "integer"},
					{Name: "deleted", Type: "smallint"},
				},
			},
			columns:      []string{"deleted"},
			wantColumn:   "deleted",
			wantStrategy: "integer",
		},
		{
			name: "integer column: tinyint",
			table: parser.Table{
				Name: "records",
				Columns: []parser.Column{
					{Name: "id", Type: "integer"},
					{Name: "deleted_flag", Type: "tinyint"},
				},
			},
			columns:      []string{"deleted_flag"},
			wantColumn:   "deleted_flag",
			wantStrategy: "integer",
		},
		{
			name: "type mismatch: varchar",
			table: parser.Table{
				Name: "users",
				Columns: []parser.Column{
					{Name: "id", Type: "integer"},
					{Name: "deleted_at", Type: "varchar(255)"},
				},
			},
			columns: []string{"deleted_at"},
			wantErr: true,
		},
		{
			name: "type mismatch: text",
			table: parser.Table{
				Name: "users",
				Columns: []parser.Column{
					{Name: "id", Type: "integer"},
					{Name: "is_deleted", Type: "text"},
				},
			},
			columns: []string{"is_deleted"},
			wantErr: true,
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
			columns:      []string{"deleted_at", "is_deleted"},
			wantColumn:   "",
			wantStrategy: "",
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
			columns:      nil,
			wantColumn:   "",
			wantStrategy: "",
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
			columns:      []string{"deleted_at", "is_deleted"},
			wantColumn:   "is_deleted",
			wantStrategy: "bool",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			col, strategy, err := parser.DetectSoftDelete(&tt.table, tt.columns)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("DetectSoftDelete() error = nil, want error")
				}
				return
			}
			if err != nil {
				t.Fatalf("DetectSoftDelete() unexpected error: %v", err)
			}
			if col != tt.wantColumn {
				t.Errorf("DetectSoftDelete() column = %q, want %q", col, tt.wantColumn)
			}
			if strategy != tt.wantStrategy {
				t.Errorf("DetectSoftDelete() strategy = %q, want %q", strategy, tt.wantStrategy)
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

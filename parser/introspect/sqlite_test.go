package introspect

import "testing"

func TestQuoteSQLiteIdent(t *testing.T) {
	tests := []struct {
		name  string
		ident string
		want  string
	}{
		{name: "plain", ident: "users", want: `"users"`},
		{name: "embedded quote", ident: `Quoted"Name`, want: `"Quoted""Name"`},
		{name: "backslash", ident: `back\slash`, want: `"back\slash"`},
		{name: "space and mixed case", ident: "My Table", want: `"My Table"`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := quoteSQLiteIdent(tt.ident); got != tt.want {
				t.Errorf("quoteSQLiteIdent(%q) = %s, want %s", tt.ident, got, tt.want)
			}
		})
	}
}

func TestExtractSQLiteIndexWhere(t *testing.T) {
	tests := []struct {
		name string
		ddl  string
		want string
	}{
		{
			name: "no where clause",
			ddl:  `CREATE UNIQUE INDEX idx_users_email ON users(email)`,
			want: "",
		},
		{
			name: "trailing partial where",
			ddl:  `CREATE UNIQUE INDEX idx_users_email_active ON users(email) WHERE deleted_at IS NULL`,
			want: "deleted_at IS NULL",
		},
		{
			name: "lowercase where keyword",
			ddl:  `create unique index idx_users_email on users(email) where deleted_at is null`,
			want: "deleted_at is null",
		},
		{
			name: "identifier containing where is not matched",
			ddl:  `CREATE INDEX idx_t_nowhereland ON t (nowhereland)`,
			want: "",
		},
		{
			name: "where after parenthesized column list",
			ddl:  `CREATE UNIQUE INDEX idx ON t (a, b)WHERE a > 0`,
			want: "a > 0",
		},
		{
			name: "line comment WHERE is ignored",
			ddl:  "CREATE UNIQUE INDEX idx ON t(email) -- WHERE x = 0\n WHERE deleted_at IS NULL",
			want: "deleted_at IS NULL",
		},
		{
			name: "block comment WHERE is ignored",
			ddl:  `CREATE UNIQUE INDEX idx ON t(email) /* WHERE x = 0 */ WHERE deleted_at IS NULL`,
			want: "deleted_at IS NULL",
		},
		{
			name: "quoted identifier containing WHERE is not matched",
			ddl:  `CREATE INDEX idx ON t ("a WHERE b")`,
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractSQLiteIndexWhere(tt.ddl)
			if got != tt.want {
				t.Errorf("extractSQLiteIndexWhere(%q) = %q, want %q", tt.ddl, got, tt.want)
			}
		})
	}
}

func TestSQLiteGeneratedStorage(t *testing.T) {
	tests := []struct {
		name        string
		hidden      int
		wantStorage string
		wantOK      bool
	}{
		{name: "ordinary column", hidden: 0, wantOK: true},
		{name: "virtual table hidden column", hidden: 1},
		{name: "virtual generated column", hidden: 2, wantStorage: "VIRTUAL", wantOK: true},
		{name: "stored generated column", hidden: 3, wantStorage: "STORED", wantOK: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			storage, ok := sqliteGeneratedStorage(tt.hidden)
			if storage != tt.wantStorage || ok != tt.wantOK {
				t.Errorf("sqliteGeneratedStorage(%d) = (%q, %t), want (%q, %t)", tt.hidden, storage, ok, tt.wantStorage, tt.wantOK)
			}
		})
	}
}

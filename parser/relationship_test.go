package parser_test

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/teandresmith/sqlgen/parser"
	"github.com/teandresmith/sqlgen/parser/mysql"
	"github.com/teandresmith/sqlgen/parser/postgres"
	"github.com/teandresmith/sqlgen/parser/sqlite"
)

// parseDDL parses ddl in dialect and resolves its references, as the CLI does
// before detection. pkOverrides maps a table to the columns its primary_key
// override declares.
func parseDDL(t *testing.T, dialect, ddl string, pkOverrides map[string][]string) *parser.Schema {
	t.Helper()
	var p parser.Parser
	switch dialect {
	case "postgres":
		p = postgres.New("public")
	case "mysql":
		var err error
		if p, err = mysql.New(); err != nil {
			t.Fatalf("mysql.New() error: %v", err)
		}
	case "sqlite":
		p = sqlite.New()
	}
	if err := p.Parse("schema.sql", []byte(ddl)); err != nil {
		t.Fatalf("Parse() error: %v", err)
	}
	schema := p.Schema()
	parser.ResolveReferenceNames(schema, dialect)
	for i := range schema.Tables {
		table := &schema.Tables[i]
		if cols, ok := pkOverrides[table.Name]; ok {
			for j := range table.Columns {
				table.Columns[j].PrimaryKey = slices.Contains(cols, table.Columns[j].Name)
			}
		}
	}
	return schema
}

// edgeStrings spells each relationship as
// "<kind> <source> -> <target> <name> [<local→referenced>…] <constraint>", with
// "(<base>)" after a renamed edge's name. An M2M edge lists its local and its
// reference key, then their constraints.
func edgeStrings(rels []parser.Relationship) []string {
	pairs := func(key []parser.KeyPair) string {
		s := make([]string, len(key))
		for i, p := range key {
			s[i] = p.Local + "→" + p.Referenced
		}
		return "[" + strings.Join(s, " ") + "]"
	}
	var out []string
	for _, r := range rels {
		name := r.Name
		if r.BaseName != r.Name {
			name += " (" + r.BaseName + ")"
		}
		switch r.Type {
		case parser.ManyToMany:
			out = append(out, fmt.Sprintf("M2M %s -> %s %s via %s %s %s %s / %s",
				r.SourceTable, r.TargetTable, name, r.JunctionTable,
				pairs(r.JunctionLocal), pairs(r.JunctionReference), r.JunctionLocalConstraint, r.JunctionReferenceConstraint))
		default:
			kind := "O2M"
			if r.Type == parser.OneToOne {
				kind = "O2O"
			}
			out = append(out, fmt.Sprintf("%s %s -> %s %s %s %s", kind, r.SourceTable, r.TargetTable, name, pairs(r.Key), r.Constraint))
		}
	}
	return out
}

const workspacesDDL = "CREATE TABLE workspaces (id bigint PRIMARY KEY);\n"

func TestDetectRelationships(t *testing.T) {
	tests := []struct {
		name    string
		dialect string // default postgres
		// allDialects runs the case on every dialect, each foreign key a named
		// table constraint. "@." in want is the PostgreSQL schema prefix,
		// dropped on MySQL and SQLite.
		allDialects  bool
		ddl          string
		pkOverrides  map[string][]string
		want         []string
		wantWarnings []string
	}{
		// --- belongs-to edges and to-one naming ---
		{
			name:        "non-unique foreign key gives a list edge and a belongs-to edge",
			allDialects: true,
			ddl: `
CREATE TABLE users (id bigint PRIMARY KEY);
CREATE TABLE posts (id bigint PRIMARY KEY, author_id bigint,
  CONSTRAINT posts_author FOREIGN KEY (author_id) REFERENCES users (id));`,
			want: []string{
				"O2M @.users -> @.posts posts [author_id→id] posts_author",
				"O2O @.posts -> @.users author (users) [author_id→id] posts_author",
			},
		},
		{
			name:        "composite foreign key gives a belongs-to edge over every pair",
			allDialects: true,
			ddl: `
CREATE TABLE cycles (id bigint PRIMARY KEY, workspace_id bigint NOT NULL, UNIQUE (workspace_id, id));
CREATE TABLE tasks (id bigint PRIMARY KEY, workspace_id bigint NOT NULL, cycle_id bigint,
  CONSTRAINT tasks_cycle FOREIGN KEY (workspace_id, cycle_id) REFERENCES cycles (workspace_id, id));`,
			want: []string{
				"O2M @.cycles -> @.tasks tasks [workspace_id→workspace_id cycle_id→id] tasks_cycle",
				"O2O @.tasks -> @.cycles cycle (cycles) [workspace_id→workspace_id cycle_id→id] tasks_cycle",
			},
		},
		{
			name:        "self-referential foreign key gives both edges",
			allDialects: true,
			ddl: `
CREATE TABLE tasks (id bigint PRIMARY KEY, parent_task_id bigint,
  CONSTRAINT tasks_parent FOREIGN KEY (parent_task_id) REFERENCES tasks (id));`,
			want: []string{
				"O2M @.tasks -> @.tasks tasks [parent_task_id→id] tasks_parent",
				"O2O @.tasks -> @.tasks parent_task (tasks) [parent_task_id→id] tasks_parent",
			},
		},
		{
			// Three foreign keys remain after rule 4's set-aside, so the table
			// is not a junction.
			name:        "three foreign keys on a non-junction give three belongs-to edges",
			allDialects: true,
			ddl: `
CREATE TABLE workspaces (id bigint PRIMARY KEY);
CREATE TABLE tasks (id bigint PRIMARY KEY);
CREATE TABLE labels (id bigint PRIMARY KEY);
CREATE TABLE task_labels (workspace_id bigint NOT NULL, task_id bigint NOT NULL, label_id bigint NOT NULL,
  PRIMARY KEY (workspace_id, task_id, label_id),
  CONSTRAINT task_labels_workspace FOREIGN KEY (workspace_id) REFERENCES workspaces (id),
  CONSTRAINT task_labels_task FOREIGN KEY (task_id) REFERENCES tasks (id),
  CONSTRAINT task_labels_label FOREIGN KEY (label_id) REFERENCES labels (id));`,
			want: []string{
				"O2M @.workspaces -> @.task_labels task_labels [workspace_id→id] task_labels_workspace",
				"O2O @.task_labels -> @.workspaces workspace (workspaces) [workspace_id→id] task_labels_workspace",
				"O2M @.tasks -> @.task_labels task_labels [task_id→id] task_labels_task",
				"O2O @.task_labels -> @.tasks task (tasks) [task_id→id] task_labels_task",
				"O2M @.labels -> @.task_labels task_labels [label_id→id] task_labels_label",
				"O2O @.task_labels -> @.labels label (labels) [label_id→id] task_labels_label",
			},
		},
		{
			// A unique key's edge is named the same way as a non-unique one's.
			name:        "to-one edge named after its key column",
			allDialects: true,
			ddl: `
CREATE TABLE users (id bigint PRIMARY KEY);
CREATE TABLE comments (id bigint PRIMARY KEY, writer_id bigint,
  CONSTRAINT comments_writer FOREIGN KEY (writer_id) REFERENCES users (id));
CREATE TABLE profiles (id bigint PRIMARY KEY, owner_id bigint UNIQUE,
  CONSTRAINT profiles_owner FOREIGN KEY (owner_id) REFERENCES users (id));`,
			want: []string{
				"O2M @.users -> @.comments comments [writer_id→id] comments_writer",
				"O2O @.comments -> @.users writer (users) [writer_id→id] comments_writer",
				"O2O @.profiles -> @.users owner (users) [owner_id→id] profiles_owner",
			},
		},
		{
			name:        "to-one edge whose stem is a column takes the table name",
			allDialects: true,
			ddl: `
CREATE TABLE codes (id bigint PRIMARY KEY);
CREATE TABLE items (id bigint PRIMARY KEY, code text, code_id bigint,
  CONSTRAINT items_code FOREIGN KEY (code_id) REFERENCES codes (id));`,
			want: []string{
				"O2M @.codes -> @.items items [code_id→id] items_code",
				"O2O @.items -> @.codes codes [code_id→id] items_code",
			},
		},
		{
			// Rule 6 groups to-one and list edges apart, so the two edges
			// between categories and itself are no collision.
			name:        "self-referential list edge keeps its name beside its belongs-to edge",
			allDialects: true,
			ddl: `
CREATE TABLE categories (id bigint PRIMARY KEY, parent_id bigint,
  CONSTRAINT categories_parent FOREIGN KEY (parent_id) REFERENCES categories (id));`,
			want: []string{
				"O2M @.categories -> @.categories categories [parent_id→id] categories_parent",
				"O2O @.categories -> @.categories parent (categories) [parent_id→id] categories_parent",
			},
		},
		// --- the tenant pattern ---
		{
			name: "composite tenant FK is O2M",
			ddl: workspacesDDL + `
CREATE TABLE cycles (id bigint PRIMARY KEY, workspace_id bigint NOT NULL REFERENCES workspaces(id), UNIQUE (workspace_id, id));
CREATE TABLE tasks (id bigint PRIMARY KEY, workspace_id bigint NOT NULL REFERENCES workspaces(id), cycle_id bigint,
  FOREIGN KEY (workspace_id, cycle_id) REFERENCES cycles (workspace_id, id));`,
			want: []string{
				"O2M public.workspaces -> public.cycles cycles [workspace_id→id] cycles_workspace_id_fkey",
				"O2O public.cycles -> public.workspaces workspace (workspaces) [workspace_id→id] cycles_workspace_id_fkey",
				"O2M public.workspaces -> public.tasks tasks [workspace_id→id] tasks_workspace_id_fkey",
				"O2O public.tasks -> public.workspaces workspace (workspaces) [workspace_id→id] tasks_workspace_id_fkey",
				"O2M public.cycles -> public.tasks tasks [workspace_id→workspace_id cycle_id→id] tasks_workspace_id_cycle_id_fkey",
				"O2O public.tasks -> public.cycles cycle (cycles) [workspace_id→workspace_id cycle_id→id] tasks_workspace_id_cycle_id_fkey",
			},
		},
		{
			name: "single-column FK beside the target's composite UNIQUE",
			ddl: workspacesDDL + `
CREATE TABLE cycles (id bigint PRIMARY KEY, workspace_id bigint NOT NULL REFERENCES workspaces(id), UNIQUE (workspace_id, id));
CREATE TABLE tasks (id bigint PRIMARY KEY, workspace_id bigint NOT NULL REFERENCES workspaces(id), cycle_id bigint REFERENCES cycles(id));`,
			want: []string{
				"O2M public.workspaces -> public.cycles cycles [workspace_id→id] cycles_workspace_id_fkey",
				"O2O public.cycles -> public.workspaces workspace (workspaces) [workspace_id→id] cycles_workspace_id_fkey",
				"O2M public.workspaces -> public.tasks tasks [workspace_id→id] tasks_workspace_id_fkey",
				"O2O public.tasks -> public.workspaces workspace (workspaces) [workspace_id→id] tasks_workspace_id_fkey",
				"O2M public.cycles -> public.tasks tasks [cycle_id→id] tasks_cycle_id_fkey",
				"O2O public.tasks -> public.cycles cycle (cycles) [cycle_id→id] tasks_cycle_id_fkey",
			},
		},
		{
			name: "reversed column order pairs in declared order",
			ddl: workspacesDDL + `
CREATE TABLE cycles (id bigint PRIMARY KEY, workspace_id bigint NOT NULL REFERENCES workspaces(id), UNIQUE (workspace_id, id));
CREATE TABLE tasks (id bigint PRIMARY KEY, workspace_id bigint NOT NULL REFERENCES workspaces(id), cycle_id bigint,
  FOREIGN KEY (cycle_id, workspace_id) REFERENCES cycles (id, workspace_id));`,
			want: []string{
				"O2M public.workspaces -> public.cycles cycles [workspace_id→id] cycles_workspace_id_fkey",
				"O2O public.cycles -> public.workspaces workspace (workspaces) [workspace_id→id] cycles_workspace_id_fkey",
				"O2M public.workspaces -> public.tasks tasks [workspace_id→id] tasks_workspace_id_fkey",
				"O2O public.tasks -> public.workspaces workspace (workspaces) [workspace_id→id] tasks_workspace_id_fkey",
				"O2M public.cycles -> public.tasks tasks [cycle_id→id workspace_id→workspace_id] tasks_cycle_id_workspace_id_fkey",
				"O2O public.tasks -> public.cycles cycle (cycles) [cycle_id→id workspace_id→workspace_id] tasks_cycle_id_workspace_id_fkey",
			},
		},
		{
			name: "FK to a composite primary key",
			ddl: workspacesDDL + `
CREATE TABLE cycles (workspace_id bigint NOT NULL REFERENCES workspaces(id), id bigint NOT NULL, PRIMARY KEY (workspace_id, id));
CREATE TABLE tasks (id bigint PRIMARY KEY, workspace_id bigint NOT NULL REFERENCES workspaces(id), cycle_id bigint,
  FOREIGN KEY (workspace_id, cycle_id) REFERENCES cycles (workspace_id, id));`,
			want: []string{
				"O2M public.workspaces -> public.cycles cycles [workspace_id→id] cycles_workspace_id_fkey",
				"O2O public.cycles -> public.workspaces workspace (workspaces) [workspace_id→id] cycles_workspace_id_fkey",
				"O2M public.workspaces -> public.tasks tasks [workspace_id→id] tasks_workspace_id_fkey",
				"O2O public.tasks -> public.workspaces workspace (workspaces) [workspace_id→id] tasks_workspace_id_fkey",
				"O2M public.cycles -> public.tasks tasks [workspace_id→workspace_id cycle_id→id] tasks_workspace_id_cycle_id_fkey",
				"O2O public.tasks -> public.cycles cycle (cycles) [workspace_id→workspace_id cycle_id→id] tasks_workspace_id_cycle_id_fkey",
			},
		},
		{
			name: "composite-PK entity with two composite FKs",
			ddl: workspacesDDL + `
CREATE TABLE projects (workspace_id bigint NOT NULL REFERENCES workspaces(id), id bigint NOT NULL, PRIMARY KEY (workspace_id, id));
CREATE TABLE cycles (workspace_id bigint NOT NULL REFERENCES workspaces(id), id bigint NOT NULL, PRIMARY KEY (workspace_id, id));
CREATE TABLE tasks (workspace_id bigint NOT NULL REFERENCES workspaces(id), id bigint NOT NULL, project_id bigint NOT NULL, cycle_id bigint,
  PRIMARY KEY (workspace_id, id),
  FOREIGN KEY (workspace_id, project_id) REFERENCES projects (workspace_id, id),
  FOREIGN KEY (workspace_id, cycle_id) REFERENCES cycles (workspace_id, id));`,
			want: []string{
				"O2M public.workspaces -> public.projects projects [workspace_id→id] projects_workspace_id_fkey",
				"O2O public.projects -> public.workspaces workspace (workspaces) [workspace_id→id] projects_workspace_id_fkey",
				"O2M public.workspaces -> public.cycles cycles [workspace_id→id] cycles_workspace_id_fkey",
				"O2O public.cycles -> public.workspaces workspace (workspaces) [workspace_id→id] cycles_workspace_id_fkey",
				"O2M public.workspaces -> public.tasks tasks [workspace_id→id] tasks_workspace_id_fkey",
				"O2O public.tasks -> public.workspaces workspace (workspaces) [workspace_id→id] tasks_workspace_id_fkey",
				"O2M public.projects -> public.tasks tasks [workspace_id→workspace_id project_id→id] tasks_workspace_id_project_id_fkey",
				"O2O public.tasks -> public.projects project (projects) [workspace_id→workspace_id project_id→id] tasks_workspace_id_project_id_fkey",
				"O2M public.cycles -> public.tasks tasks [workspace_id→workspace_id cycle_id→id] tasks_workspace_id_cycle_id_fkey",
				"O2O public.tasks -> public.cycles cycle (cycles) [workspace_id→workspace_id cycle_id→id] tasks_workspace_id_cycle_id_fkey",
			},
		},
		// --- O2O by a unique subset ---
		{
			name: "UNIQUE on the FK's own column is O2O",
			ddl: workspacesDDL + `
CREATE TABLE cycles (id bigint PRIMARY KEY, workspace_id bigint NOT NULL REFERENCES workspaces(id), UNIQUE (workspace_id, id));
CREATE TABLE cycle_reports (id bigint PRIMARY KEY, workspace_id bigint NOT NULL REFERENCES workspaces(id), cycle_id bigint UNIQUE,
  FOREIGN KEY (workspace_id, cycle_id) REFERENCES cycles (workspace_id, id));`,
			want: []string{
				"O2M public.workspaces -> public.cycles cycles [workspace_id→id] cycles_workspace_id_fkey",
				"O2O public.cycles -> public.workspaces workspace (workspaces) [workspace_id→id] cycles_workspace_id_fkey",
				"O2M public.workspaces -> public.cycle_reports cycle_reports [workspace_id→id] cycle_reports_workspace_id_fkey",
				"O2O public.cycle_reports -> public.workspaces workspace (workspaces) [workspace_id→id] cycle_reports_workspace_id_fkey",
				"O2O public.cycle_reports -> public.cycles cycle (cycles) [workspace_id→workspace_id cycle_id→id] cycle_reports_workspace_id_cycle_id_fkey",
			},
		},
		{
			name: "UNIQUE on every FK column is O2O",
			ddl: workspacesDDL + `
CREATE TABLE cycles (id bigint PRIMARY KEY, workspace_id bigint NOT NULL REFERENCES workspaces(id), UNIQUE (workspace_id, id));
CREATE TABLE cycle_reports (id bigint PRIMARY KEY, workspace_id bigint NOT NULL REFERENCES workspaces(id), cycle_id bigint, UNIQUE (workspace_id, cycle_id),
  FOREIGN KEY (workspace_id, cycle_id) REFERENCES cycles (workspace_id, id));`,
			want: []string{
				"O2M public.workspaces -> public.cycles cycles [workspace_id→id] cycles_workspace_id_fkey",
				"O2O public.cycles -> public.workspaces workspace (workspaces) [workspace_id→id] cycles_workspace_id_fkey",
				"O2M public.workspaces -> public.cycle_reports cycle_reports [workspace_id→id] cycle_reports_workspace_id_fkey",
				"O2O public.cycle_reports -> public.workspaces workspace (workspaces) [workspace_id→id] cycle_reports_workspace_id_fkey",
				"O2O public.cycle_reports -> public.cycles cycle (cycles) [workspace_id→workspace_id cycle_id→id] cycle_reports_workspace_id_cycle_id_fkey",
			},
		},
		{
			name: "single-column UNIQUE FK is O2O",
			ddl: `
CREATE TABLE users (id bigint PRIMARY KEY);
CREATE TABLE profiles (id bigint PRIMARY KEY, user_id bigint UNIQUE REFERENCES users(id));`,
			want: []string{
				"O2O public.profiles -> public.users user (users) [user_id→id] profiles_user_id_fkey",
			},
		},
		{
			name: "sole primary-key column FK is O2O",
			ddl: `
CREATE TABLE users (id bigint PRIMARY KEY);
CREATE TABLE user_credentials (user_id bigint PRIMARY KEY REFERENCES users(id), secret text);`,
			want: []string{
				"O2O public.user_credentials -> public.users user (users) [user_id→id] user_credentials_user_id_fkey",
			},
		},
		{
			name: "cycle_settings: keyed on its composite FK, it is O2O and not a junction",
			ddl: workspacesDDL + `
CREATE TABLE cycles (id bigint PRIMARY KEY, workspace_id bigint NOT NULL REFERENCES workspaces(id), UNIQUE (workspace_id, id));
CREATE TABLE cycle_settings (workspace_id bigint NOT NULL REFERENCES workspaces(id), cycle_id bigint NOT NULL, PRIMARY KEY (workspace_id, cycle_id),
  FOREIGN KEY (workspace_id, cycle_id) REFERENCES cycles (workspace_id, id));`,
			want: []string{
				"O2M public.workspaces -> public.cycles cycles [workspace_id→id] cycles_workspace_id_fkey",
				"O2O public.cycles -> public.workspaces workspace (workspaces) [workspace_id→id] cycles_workspace_id_fkey",
				"O2M public.workspaces -> public.cycle_settings cycle_settings [workspace_id→id] cycle_settings_workspace_id_fkey",
				"O2O public.cycle_settings -> public.workspaces workspace (workspaces) [workspace_id→id] cycle_settings_workspace_id_fkey",
				"O2O public.cycle_settings -> public.cycles cycle (cycles) [workspace_id→workspace_id cycle_id→id] cycle_settings_workspace_id_cycle_id_fkey",
			},
		},
		{
			name: "a member of a composite primary key is not unique",
			ddl: `
CREATE TABLE orders (id bigint PRIMARY KEY);
CREATE TABLE order_lines (order_id bigint NOT NULL REFERENCES orders(id), line_no int NOT NULL, PRIMARY KEY (order_id, line_no));`,
			want: []string{
				"O2M public.orders -> public.order_lines order_lines [order_id→id] order_lines_order_id_fkey",
				"O2O public.order_lines -> public.orders order (orders) [order_id→id] order_lines_order_id_fkey",
			},
		},
		{
			name: "partial UNIQUE does not make an FK O2O",
			ddl: `
CREATE TABLE users (id bigint PRIMARY KEY);
CREATE TABLE sessions (id bigint PRIMARY KEY, user_id bigint REFERENCES users(id), active boolean);
CREATE UNIQUE INDEX one_active_session ON sessions (user_id) WHERE active;`,
			want: []string{
				"O2M public.users -> public.sessions sessions [user_id→id] sessions_user_id_fkey",
				"O2O public.sessions -> public.users user (users) [user_id→id] sessions_user_id_fkey",
			},
		},
		// --- target keys ---
		{
			name: "FK to a non-PK UNIQUE column",
			ddl: `
CREATE TABLE users (id int PRIMARY KEY, email text NOT NULL UNIQUE);
CREATE TABLE invites (id int PRIMARY KEY, email text REFERENCES users(email));`,
			want: []string{
				"O2M public.users -> public.invites invites [email→email] invites_email_fkey",
				"O2O public.invites -> public.users users [email→email] invites_email_fkey",
			},
		},
		{
			name:        "a PK override moves the target key and the declared PK still counts",
			pkOverrides: map[string][]string{"cycles": {"code"}},
			ddl: `
CREATE TABLE cycles (id int PRIMARY KEY, code text NOT NULL UNIQUE, label text NOT NULL);
CREATE INDEX cycles_label ON cycles (label);
CREATE TABLE tasks (id int PRIMARY KEY, cycle_id int REFERENCES cycles(id), cycle_code text REFERENCES cycles(code));`,
			want: []string{
				"O2M public.cycles -> public.tasks cycle_tasks (tasks) [cycle_id→id] tasks_cycle_id_fkey",
				"O2O public.tasks -> public.cycles cycle (cycles) [cycle_id→id] tasks_cycle_id_fkey",
				"O2M public.cycles -> public.tasks cycle_code_tasks (tasks) [cycle_code→code] tasks_cycle_code_fkey",
				"O2O public.tasks -> public.cycles cycle_code (cycles) [cycle_code→code] tasks_cycle_code_fkey",
			},
		},
		{
			name:        "a PK override on the FK column makes it O2O",
			pkOverrides: map[string][]string{"user_settings": {"user_id"}},
			ddl: `
CREATE TABLE users (id bigint PRIMARY KEY);
CREATE TABLE user_settings (user_id bigint NOT NULL REFERENCES users(id), theme text);`,
			want: []string{
				"O2O public.user_settings -> public.users user (users) [user_id→id] user_settings_user_id_fkey",
			},
		},
		{
			name:    "MySQL composite FK to an indexed non-key warns",
			dialect: "mysql",
			ddl: `
CREATE TABLE parents (id int PRIMARY KEY, b int, c int, KEY (b, c));
CREATE TABLE children (id int PRIMARY KEY, a int, b int, FOREIGN KEY (a, b) REFERENCES parents (b, c));`,
			wantWarnings: []string{
				"tables.children: foreign key children_ibfk_1 references columns that are neither the primary key nor a non-partial UNIQUE constraint of parents, so it gives no relationship; declare one by hand if you need it (PRD §5.3, §13.4)",
			},
		},
		{
			name:    "SQLite composite FK to non-key columns warns",
			dialect: "sqlite",
			ddl: `
CREATE TABLE workspaces (id INTEGER PRIMARY KEY);
CREATE TABLE cycles (id INTEGER PRIMARY KEY, workspace_id INTEGER NOT NULL REFERENCES workspaces(id));
CREATE TABLE tasks (id INTEGER PRIMARY KEY, workspace_id INTEGER NOT NULL REFERENCES workspaces(id), cycle_id INTEGER,
  FOREIGN KEY (workspace_id, cycle_id) REFERENCES cycles (workspace_id, id));`,
			want: []string{
				"O2M workspaces -> cycles cycles [workspace_id→id] cycles(workspace_id) → workspaces(id)",
				"O2O cycles -> workspaces workspace (workspaces) [workspace_id→id] cycles(workspace_id) → workspaces(id)",
				"O2M workspaces -> tasks tasks [workspace_id→id] tasks(workspace_id) → workspaces(id)",
				"O2O tasks -> workspaces workspace (workspaces) [workspace_id→id] tasks(workspace_id) → workspaces(id)",
			},
			wantWarnings: []string{
				"tables.tasks: foreign key tasks(workspace_id, cycle_id) → cycles(workspace_id, id) references columns that are neither the primary key nor a non-partial UNIQUE constraint of cycles, so it gives no relationship; declare one by hand if you need it (PRD §5.3, §13.4)",
			},
		},
		{
			name:    "SQLite single-column FK to a non-key column warns",
			dialect: "sqlite",
			ddl: `
CREATE TABLE workspaces (id INTEGER PRIMARY KEY, slug TEXT);
CREATE TABLE cycles (id INTEGER PRIMARY KEY, workspace_slug TEXT REFERENCES workspaces(slug));`,
			wantWarnings: []string{
				"tables.cycles: foreign key cycles(workspace_slug) → workspaces(slug) references columns that are neither the primary key nor a non-partial UNIQUE constraint of workspaces, so it gives no relationship; declare one by hand if you need it (PRD §5.3, §13.4)",
			},
		},
		{
			name:    "SQLite reference whose parent key cannot be resolved warns",
			dialect: "sqlite",
			ddl: `
CREATE TABLE tags (name TEXT);
CREATE TABLE notes (id INTEGER PRIMARY KEY, tag TEXT REFERENCES tags);`,
			wantWarnings: []string{
				"tables.notes: foreign key notes(tag) → tags() references columns that are neither the primary key nor a non-partial UNIQUE constraint of tags, so it gives no relationship; declare one by hand if you need it (PRD §5.3, §13.4)",
			},
		},
		{
			name: "FK into a table the schema does not hold gives no edge",
			ddl: `
CREATE TABLE users (id bigint PRIMARY KEY);
CREATE TABLE posts (id bigint PRIMARY KEY, author_id bigint REFERENCES users(id), legacy_id bigint REFERENCES legacy.accounts(id));`,
			want: []string{
				"O2M public.users -> public.posts posts [author_id→id] posts_author_id_fkey",
				"O2O public.posts -> public.users author (users) [author_id→id] posts_author_id_fkey",
			},
		},
		// --- redundant FKs ---
		{
			name: "single-column FK inside a composite one to the same target gives one edge",
			ddl: workspacesDDL + `
CREATE TABLE cycles (id bigint PRIMARY KEY, workspace_id bigint NOT NULL REFERENCES workspaces(id), UNIQUE (workspace_id, id));
CREATE TABLE tasks (id bigint PRIMARY KEY, workspace_id bigint NOT NULL REFERENCES workspaces(id), cycle_id bigint REFERENCES cycles(id),
  FOREIGN KEY (workspace_id, cycle_id) REFERENCES cycles (workspace_id, id));`,
			want: []string{
				"O2M public.workspaces -> public.cycles cycles [workspace_id→id] cycles_workspace_id_fkey",
				"O2O public.cycles -> public.workspaces workspace (workspaces) [workspace_id→id] cycles_workspace_id_fkey",
				"O2M public.workspaces -> public.tasks tasks [workspace_id→id] tasks_workspace_id_fkey",
				"O2O public.tasks -> public.workspaces workspace (workspaces) [workspace_id→id] tasks_workspace_id_fkey",
				"O2M public.cycles -> public.tasks tasks [workspace_id→workspace_id cycle_id→id] tasks_workspace_id_cycle_id_fkey",
				"O2O public.tasks -> public.cycles cycle (cycles) [workspace_id→workspace_id cycle_id→id] tasks_workspace_id_cycle_id_fkey",
			},
		},
		// Of identical keys the one whose name sorts first gives the edge, as
		// both sources agree on it.
		{
			name: "duplicate FKs give one edge",
			ddl: `
CREATE TABLE users (id bigint PRIMARY KEY);
CREATE TABLE posts (id bigint PRIMARY KEY, author_id bigint,
  CONSTRAINT posts_author_b FOREIGN KEY (author_id) REFERENCES users(id),
  CONSTRAINT posts_author_a FOREIGN KEY (author_id) REFERENCES users(id));`,
			want: []string{
				"O2M public.users -> public.posts posts [author_id→id] posts_author_a",
				"O2O public.posts -> public.users author (users) [author_id→id] posts_author_a",
			},
		},
		{
			name: "identical FKs listing their columns in different orders keep the name that sorts first",
			ddl: `
CREATE TABLE t (x int NOT NULL, y int NOT NULL, PRIMARY KEY (x, y));
CREATE TABLE c (id int PRIMARY KEY, a int, b int,
  CONSTRAINT z_decl FOREIGN KEY (a, b) REFERENCES t (x, y),
  CONSTRAINT a_decl FOREIGN KEY (b, a) REFERENCES t (y, x));`,
			want: []string{"O2M public.t -> public.c c [b→y a→x] a_decl", "O2O public.c -> public.t t [b→y a→x] a_decl"},
		},
		{
			name:    "identical unnamed SQLite FKs keep the first declared",
			dialect: "sqlite",
			ddl: `
CREATE TABLE t (x INTEGER NOT NULL, y INTEGER NOT NULL, PRIMARY KEY (x, y));
CREATE TABLE c (id INTEGER PRIMARY KEY, a INTEGER, b INTEGER,
  FOREIGN KEY (b, a) REFERENCES t (y, x),
  FOREIGN KEY (a, b) REFERENCES t (x, y));`,
			want: []string{"O2M t -> c c [b→y a→x] c(b, a) → t(y, x)", "O2O c -> t t [b→y a→x] c(b, a) → t(y, x)"},
		},
		{
			name: "a duplicated FK keeps its naming member",
			ddl: `
CREATE TABLE users (id bigint PRIMARY KEY, email text NOT NULL UNIQUE);
CREATE TABLE posts (id bigint PRIMARY KEY, author_email text, reviewer_email text,
  CONSTRAINT posts_author_a FOREIGN KEY (author_email) REFERENCES users (email),
  CONSTRAINT posts_author_b FOREIGN KEY (author_email) REFERENCES users (email),
  CONSTRAINT posts_reviewer_a FOREIGN KEY (reviewer_email) REFERENCES users (email),
  CONSTRAINT posts_reviewer_b FOREIGN KEY (reviewer_email) REFERENCES users (email));`,
			want: []string{
				"O2M public.users -> public.posts author_email_posts (posts) [author_email→email] posts_author_a",
				"O2O public.posts -> public.users author_email (users) [author_email→email] posts_author_a",
				"O2M public.users -> public.posts reviewer_email_posts (posts) [reviewer_email→email] posts_reviewer_a",
				"O2O public.posts -> public.users reviewer_email (users) [reviewer_email→email] posts_reviewer_a",
			},
		},
		{
			name: "one column with FKs to two tables gives two edges",
			ddl: `
CREATE TABLE people (id bigint PRIMARY KEY);
CREATE TABLE employees (id bigint PRIMARY KEY);
CREATE TABLE badges (id bigint PRIMARY KEY, holder_id bigint REFERENCES people(id) REFERENCES employees(id));`,
			want: []string{
				"O2M public.people -> public.badges badges [holder_id→id] badges_holder_id_fkey",
				"O2O public.badges -> public.people holder (people) [holder_id→id] badges_holder_id_fkey",
				"O2M public.employees -> public.badges badges [holder_id→id] badges_holder_id_fkey1",
				"O2O public.badges -> public.employees holder (employees) [holder_id→id] badges_holder_id_fkey1",
			},
		},
		// --- junctions ---
		{
			name: "two FKs in a composite primary key are M2M",
			ddl: `
CREATE TABLE products (id bigint PRIMARY KEY);
CREATE TABLE tags (id bigint PRIMARY KEY);
CREATE TABLE product_tags (product_id bigint REFERENCES products(id), tag_id bigint REFERENCES tags(id), PRIMARY KEY (product_id, tag_id));`,
			want: []string{
				"M2M public.products -> public.tags tags via public.product_tags [product_id→id] [tag_id→id] product_tags_product_id_fkey / product_tags_tag_id_fkey",
				"M2M public.tags -> public.products products via public.product_tags [tag_id→id] [product_id→id] product_tags_tag_id_fkey / product_tags_product_id_fkey",
			},
		},
		{
			name: "two FKs in a composite UNIQUE beside a surrogate key are M2M",
			ddl: `
CREATE TABLE products (id bigint PRIMARY KEY);
CREATE TABLE tags (id bigint PRIMARY KEY);
CREATE TABLE product_tags (id bigint PRIMARY KEY, product_id bigint REFERENCES products(id), tag_id bigint REFERENCES tags(id), UNIQUE (product_id, tag_id));`,
			want: []string{
				"M2M public.products -> public.tags tags via public.product_tags [product_id→id] [tag_id→id] product_tags_product_id_fkey / product_tags_tag_id_fkey",
				"M2M public.tags -> public.products products via public.product_tags [tag_id→id] [product_id→id] product_tags_tag_id_fkey / product_tags_product_id_fkey",
			},
		},
		{
			name: "a key with a payload column beside the two FKs is M2M",
			ddl: `
CREATE TABLE users (id bigint PRIMARY KEY);
CREATE TABLE roles (id bigint PRIMARY KEY);
CREATE TABLE user_roles (user_id bigint REFERENCES users(id), role_id bigint REFERENCES roles(id), scope text, PRIMARY KEY (user_id, role_id, scope));`,
			want: []string{
				"M2M public.users -> public.roles roles via public.user_roles [user_id→id] [role_id→id] user_roles_user_id_fkey / user_roles_role_id_fkey",
				"M2M public.roles -> public.users users via public.user_roles [role_id→id] [user_id→id] user_roles_role_id_fkey / user_roles_user_id_fkey",
			},
		},
		{
			name: "partial UNIQUE does not make a junction",
			ddl: `
CREATE TABLE products (id bigint PRIMARY KEY);
CREATE TABLE tags (id bigint PRIMARY KEY);
CREATE TABLE product_tags (id bigint PRIMARY KEY, product_id bigint REFERENCES products(id), tag_id bigint REFERENCES tags(id), active boolean);
CREATE UNIQUE INDEX product_tags_active ON product_tags (product_id, tag_id) WHERE active;`,
			want: []string{
				"O2M public.products -> public.product_tags product_tags [product_id→id] product_tags_product_id_fkey",
				"O2O public.product_tags -> public.products product (products) [product_id→id] product_tags_product_id_fkey",
				"O2M public.tags -> public.product_tags product_tags [tag_id→id] product_tags_tag_id_fkey",
				"O2O public.product_tags -> public.tags tag (tags) [tag_id→id] product_tags_tag_id_fkey",
			},
		},
		{
			name: "three single-column FKs in the key give three O2M and three belongs-to edges",
			ddl: workspacesDDL + `
CREATE TABLE tasks (id bigint PRIMARY KEY, workspace_id bigint NOT NULL REFERENCES workspaces(id));
CREATE TABLE labels (id bigint PRIMARY KEY, workspace_id bigint NOT NULL REFERENCES workspaces(id));
CREATE TABLE task_labels (workspace_id bigint NOT NULL REFERENCES workspaces(id), task_id bigint NOT NULL REFERENCES tasks(id), label_id bigint NOT NULL REFERENCES labels(id),
  PRIMARY KEY (workspace_id, task_id, label_id));`,
			want: []string{
				"O2M public.workspaces -> public.tasks tasks [workspace_id→id] tasks_workspace_id_fkey",
				"O2O public.tasks -> public.workspaces workspace (workspaces) [workspace_id→id] tasks_workspace_id_fkey",
				"O2M public.workspaces -> public.labels labels [workspace_id→id] labels_workspace_id_fkey",
				"O2O public.labels -> public.workspaces workspace (workspaces) [workspace_id→id] labels_workspace_id_fkey",
				"O2M public.workspaces -> public.task_labels task_labels [workspace_id→id] task_labels_workspace_id_fkey",
				"O2O public.task_labels -> public.workspaces workspace (workspaces) [workspace_id→id] task_labels_workspace_id_fkey",
				"O2M public.tasks -> public.task_labels task_labels [task_id→id] task_labels_task_id_fkey",
				"O2O public.task_labels -> public.tasks task (tasks) [task_id→id] task_labels_task_id_fkey",
				"O2M public.labels -> public.task_labels task_labels [label_id→id] task_labels_label_id_fkey",
				"O2O public.task_labels -> public.labels label (labels) [label_id→id] task_labels_label_id_fkey",
			},
		},
		{
			name: "tenant junction with a workspace FK is M2M",
			ddl: workspacesDDL + `
CREATE TABLE tasks (id bigint PRIMARY KEY, workspace_id bigint NOT NULL REFERENCES workspaces(id), UNIQUE (workspace_id, id));
CREATE TABLE labels (id bigint PRIMARY KEY, workspace_id bigint NOT NULL REFERENCES workspaces(id), UNIQUE (workspace_id, id));
CREATE TABLE task_labels (workspace_id bigint NOT NULL REFERENCES workspaces(id), task_id bigint NOT NULL, label_id bigint NOT NULL,
  PRIMARY KEY (workspace_id, task_id, label_id),
  FOREIGN KEY (workspace_id, task_id) REFERENCES tasks (workspace_id, id),
  FOREIGN KEY (workspace_id, label_id) REFERENCES labels (workspace_id, id));`,
			want: []string{
				"O2M public.workspaces -> public.tasks tasks [workspace_id→id] tasks_workspace_id_fkey",
				"O2O public.tasks -> public.workspaces workspace (workspaces) [workspace_id→id] tasks_workspace_id_fkey",
				"O2M public.workspaces -> public.labels labels [workspace_id→id] labels_workspace_id_fkey",
				"O2O public.labels -> public.workspaces workspace (workspaces) [workspace_id→id] labels_workspace_id_fkey",
				"M2M public.tasks -> public.labels labels via public.task_labels [workspace_id→workspace_id task_id→id] [workspace_id→workspace_id label_id→id] task_labels_workspace_id_task_id_fkey / task_labels_workspace_id_label_id_fkey",
				"M2M public.labels -> public.tasks tasks via public.task_labels [workspace_id→workspace_id label_id→id] [workspace_id→workspace_id task_id→id] task_labels_workspace_id_label_id_fkey / task_labels_workspace_id_task_id_fkey",
			},
		},
		{
			name: "tenant junction without a workspace FK is M2M",
			ddl: workspacesDDL + `
CREATE TABLE tasks (id bigint PRIMARY KEY, workspace_id bigint NOT NULL REFERENCES workspaces(id), UNIQUE (workspace_id, id));
CREATE TABLE labels (id bigint PRIMARY KEY, workspace_id bigint NOT NULL REFERENCES workspaces(id), UNIQUE (workspace_id, id));
CREATE TABLE task_labels (workspace_id bigint NOT NULL, task_id bigint NOT NULL, label_id bigint NOT NULL,
  PRIMARY KEY (workspace_id, task_id, label_id),
  FOREIGN KEY (workspace_id, task_id) REFERENCES tasks (workspace_id, id),
  FOREIGN KEY (workspace_id, label_id) REFERENCES labels (workspace_id, id));`,
			want: []string{
				"O2M public.workspaces -> public.tasks tasks [workspace_id→id] tasks_workspace_id_fkey",
				"O2O public.tasks -> public.workspaces workspace (workspaces) [workspace_id→id] tasks_workspace_id_fkey",
				"O2M public.workspaces -> public.labels labels [workspace_id→id] labels_workspace_id_fkey",
				"O2O public.labels -> public.workspaces workspace (workspaces) [workspace_id→id] labels_workspace_id_fkey",
				"M2M public.tasks -> public.labels labels via public.task_labels [workspace_id→workspace_id task_id→id] [workspace_id→workspace_id label_id→id] task_labels_workspace_id_task_id_fkey / task_labels_workspace_id_label_id_fkey",
				"M2M public.labels -> public.tasks tasks via public.task_labels [workspace_id→workspace_id label_id→id] [workspace_id→workspace_id task_id→id] task_labels_workspace_id_label_id_fkey / task_labels_workspace_id_task_id_fkey",
			},
		},
		{
			name: "two composite FKs whose union no key contains are not a junction",
			ddl: workspacesDDL + `
CREATE TABLE tasks (id bigint PRIMARY KEY, workspace_id bigint NOT NULL REFERENCES workspaces(id), UNIQUE (workspace_id, id));
CREATE TABLE members (id bigint PRIMARY KEY, workspace_id bigint NOT NULL REFERENCES workspaces(id), UNIQUE (workspace_id, id));
CREATE TABLE assignments (id bigint PRIMARY KEY, workspace_id bigint NOT NULL REFERENCES workspaces(id), task_id bigint NOT NULL, member_id bigint NOT NULL,
  UNIQUE (workspace_id, task_id),
  FOREIGN KEY (workspace_id, task_id) REFERENCES tasks (workspace_id, id),
  FOREIGN KEY (workspace_id, member_id) REFERENCES members (workspace_id, id));`,
			want: []string{
				"O2M public.workspaces -> public.tasks tasks [workspace_id→id] tasks_workspace_id_fkey",
				"O2O public.tasks -> public.workspaces workspace (workspaces) [workspace_id→id] tasks_workspace_id_fkey",
				"O2M public.workspaces -> public.members members [workspace_id→id] members_workspace_id_fkey",
				"O2O public.members -> public.workspaces workspace (workspaces) [workspace_id→id] members_workspace_id_fkey",
				"O2M public.workspaces -> public.assignments assignments [workspace_id→id] assignments_workspace_id_fkey",
				"O2O public.assignments -> public.workspaces workspace (workspaces) [workspace_id→id] assignments_workspace_id_fkey",
				"O2O public.assignments -> public.tasks task (tasks) [workspace_id→workspace_id task_id→id] assignments_workspace_id_task_id_fkey",
				"O2M public.members -> public.assignments assignments [workspace_id→workspace_id member_id→id] assignments_workspace_id_member_id_fkey",
				"O2O public.assignments -> public.members member (members) [workspace_id→workspace_id member_id→id] assignments_workspace_id_member_id_fkey",
			},
		},
		{
			name: "a payload UNIQUE containing both composite FKs makes a junction",
			ddl: workspacesDDL + `
CREATE TABLE tasks (id bigint PRIMARY KEY, workspace_id bigint NOT NULL REFERENCES workspaces(id), UNIQUE (workspace_id, id));
CREATE TABLE members (id bigint PRIMARY KEY, workspace_id bigint NOT NULL REFERENCES workspaces(id), UNIQUE (workspace_id, id));
CREATE TABLE time_entries (id bigint PRIMARY KEY, workspace_id bigint NOT NULL REFERENCES workspaces(id), task_id bigint NOT NULL, member_id bigint NOT NULL, day date NOT NULL,
  UNIQUE (workspace_id, task_id, member_id, day),
  FOREIGN KEY (workspace_id, task_id) REFERENCES tasks (workspace_id, id),
  FOREIGN KEY (workspace_id, member_id) REFERENCES members (workspace_id, id));`,
			want: []string{
				"O2M public.workspaces -> public.tasks tasks [workspace_id→id] tasks_workspace_id_fkey",
				"O2O public.tasks -> public.workspaces workspace (workspaces) [workspace_id→id] tasks_workspace_id_fkey",
				"O2M public.workspaces -> public.members members [workspace_id→id] members_workspace_id_fkey",
				"O2O public.members -> public.workspaces workspace (workspaces) [workspace_id→id] members_workspace_id_fkey",
				"M2M public.tasks -> public.members members via public.time_entries [workspace_id→workspace_id task_id→id] [workspace_id→workspace_id member_id→id] time_entries_workspace_id_task_id_fkey / time_entries_workspace_id_member_id_fkey",
				"M2M public.members -> public.tasks tasks via public.time_entries [workspace_id→workspace_id member_id→id] [workspace_id→workspace_id task_id→id] time_entries_workspace_id_member_id_fkey / time_entries_workspace_id_task_id_fkey",
			},
		},
		{
			name: "shipments sets aside a real parent",
			ddl: `
CREATE TABLE orders (id bigint PRIMARY KEY);
CREATE TABLE carriers (id bigint PRIMARY KEY);
CREATE TABLE order_lines (order_id bigint NOT NULL REFERENCES orders(id), line_no int NOT NULL, PRIMARY KEY (order_id, line_no));
CREATE TABLE shipments (order_id bigint NOT NULL REFERENCES orders(id), line_no int NOT NULL, carrier_id bigint NOT NULL REFERENCES carriers(id),
  PRIMARY KEY (order_id, line_no, carrier_id),
  FOREIGN KEY (order_id, line_no) REFERENCES order_lines (order_id, line_no));`,
			want: []string{
				"O2M public.orders -> public.order_lines order_lines [order_id→id] order_lines_order_id_fkey",
				"O2O public.order_lines -> public.orders order (orders) [order_id→id] order_lines_order_id_fkey",
				"M2M public.order_lines -> public.carriers carriers via public.shipments [order_id→order_id line_no→line_no] [carrier_id→id] shipments_order_id_line_no_fkey / shipments_carrier_id_fkey",
				"M2M public.carriers -> public.order_lines order_lines via public.shipments [carrier_id→id] [order_id→order_id line_no→line_no] shipments_carrier_id_fkey / shipments_order_id_line_no_fkey",
			},
		},
		{
			name: "order_items sets aside a real parent",
			ddl: `
CREATE TABLE orders (id bigint PRIMARY KEY);
CREATE TABLE products (id bigint PRIMARY KEY);
CREATE TABLE warehouses (id bigint PRIMARY KEY);
CREATE TABLE stock (warehouse_id bigint NOT NULL REFERENCES warehouses(id), product_id bigint NOT NULL REFERENCES products(id), PRIMARY KEY (warehouse_id, product_id));
CREATE TABLE order_items (order_id bigint NOT NULL REFERENCES orders(id), product_id bigint NOT NULL REFERENCES products(id), warehouse_id bigint NOT NULL,
  PRIMARY KEY (order_id, product_id, warehouse_id),
  FOREIGN KEY (warehouse_id, product_id) REFERENCES stock (warehouse_id, product_id));`,
			want: []string{
				"M2M public.warehouses -> public.products products via public.stock [warehouse_id→id] [product_id→id] stock_warehouse_id_fkey / stock_product_id_fkey",
				"M2M public.products -> public.warehouses warehouses via public.stock [product_id→id] [warehouse_id→id] stock_product_id_fkey / stock_warehouse_id_fkey",
				"M2M public.orders -> public.stock stock via public.order_items [order_id→id] [warehouse_id→warehouse_id product_id→product_id] order_items_order_id_fkey / order_items_warehouse_id_product_id_fkey",
				"M2M public.stock -> public.orders orders via public.order_items [warehouse_id→warehouse_id product_id→product_id] [order_id→id] order_items_warehouse_id_product_id_fkey / order_items_order_id_fkey",
			},
		},
		{
			name: "a junction with an end outside the schema gives no edge",
			ddl: `
CREATE TABLE teams (id bigint PRIMARY KEY);
CREATE TABLE team_members (team_id bigint REFERENCES teams(id), user_id uuid REFERENCES auth.users(id), PRIMARY KEY (team_id, user_id));`,
		},
		{
			name:    "a junction with an end on non-key columns gives no edge",
			dialect: "sqlite",
			ddl: `
CREATE TABLE teams (id INTEGER PRIMARY KEY);
CREATE TABLE tags (id INTEGER PRIMARY KEY, label TEXT);
CREATE TABLE team_tags (team_id INTEGER REFERENCES teams(id), tag_label TEXT REFERENCES tags(label), PRIMARY KEY (team_id, tag_label));`,
			wantWarnings: []string{
				"tables.team_tags: foreign key team_tags(tag_label) → tags(label) references columns that are neither the primary key nor a non-partial UNIQUE constraint of tags, so it gives no relationship; declare one by hand if you need it (PRD §5.3, §13.4)",
			},
		},
		{
			name:        "a composite PK override over two FKs does not make a junction",
			pkOverrides: map[string][]string{"user_tags": {"user_id", "tag_id"}},
			ddl: `
CREATE TABLE users (id bigint PRIMARY KEY);
CREATE TABLE tags (id bigint PRIMARY KEY);
CREATE TABLE user_tags (user_id bigint REFERENCES users(id), tag_id bigint REFERENCES tags(id));`,
			want: []string{
				"O2M public.users -> public.user_tags user_tags [user_id→id] user_tags_user_id_fkey",
				"O2O public.user_tags -> public.users user (users) [user_id→id] user_tags_user_id_fkey",
				"O2M public.tags -> public.user_tags user_tags [tag_id→id] user_tags_tag_id_fkey",
				"O2O public.user_tags -> public.tags tag (tags) [tag_id→id] user_tags_tag_id_fkey",
			},
		},
		// --- naming and disambiguation ---
		{
			name: "two single-column FKs to one target are renamed from their columns",
			ddl: `
CREATE TABLE members (id bigint PRIMARY KEY);
CREATE TABLE tasks (id bigint PRIMARY KEY, assignee_id bigint REFERENCES members(id), reviewer_id bigint REFERENCES members(id));`,
			want: []string{
				"O2M public.members -> public.tasks assignee_tasks (tasks) [assignee_id→id] tasks_assignee_id_fkey",
				"O2O public.tasks -> public.members assignee (members) [assignee_id→id] tasks_assignee_id_fkey",
				"O2M public.members -> public.tasks reviewer_tasks (tasks) [reviewer_id→id] tasks_reviewer_id_fkey",
				"O2O public.tasks -> public.members reviewer (members) [reviewer_id→id] tasks_reviewer_id_fkey",
			},
		},
		{
			name: "two composite FKs to one target are renamed from their free members",
			ddl: workspacesDDL + `
CREATE TABLE members (id bigint PRIMARY KEY, workspace_id bigint NOT NULL REFERENCES workspaces(id), UNIQUE (workspace_id, id));
CREATE TABLE tasks (id bigint PRIMARY KEY, workspace_id bigint NOT NULL REFERENCES workspaces(id), assignee_id bigint, reviewer_id bigint,
  FOREIGN KEY (workspace_id, assignee_id) REFERENCES members (workspace_id, id),
  FOREIGN KEY (workspace_id, reviewer_id) REFERENCES members (workspace_id, id));`,
			want: []string{
				"O2M public.workspaces -> public.members members [workspace_id→id] members_workspace_id_fkey",
				"O2O public.members -> public.workspaces workspace (workspaces) [workspace_id→id] members_workspace_id_fkey",
				"O2M public.workspaces -> public.tasks tasks [workspace_id→id] tasks_workspace_id_fkey",
				"O2O public.tasks -> public.workspaces workspace (workspaces) [workspace_id→id] tasks_workspace_id_fkey",
				"O2M public.members -> public.tasks assignee_tasks (tasks) [workspace_id→workspace_id assignee_id→id] tasks_workspace_id_assignee_id_fkey",
				"O2O public.tasks -> public.members assignee (members) [workspace_id→workspace_id assignee_id→id] tasks_workspace_id_assignee_id_fkey",
				"O2M public.members -> public.tasks reviewer_tasks (tasks) [workspace_id→workspace_id reviewer_id→id] tasks_workspace_id_reviewer_id_fkey",
				"O2O public.tasks -> public.members reviewer (members) [workspace_id→workspace_id reviewer_id→id] tasks_workspace_id_reviewer_id_fkey",
			},
		},
		{
			name: "two unique FKs to one target are renamed O2O edges",
			ddl: `
CREATE TABLE profiles (id bigint PRIMARY KEY);
CREATE TABLE users (id bigint PRIMARY KEY, primary_profile_id bigint UNIQUE REFERENCES profiles(id), backup_profile_id bigint UNIQUE REFERENCES profiles(id));`,
			want: []string{
				"O2O public.users -> public.profiles primary_profile (profiles) [primary_profile_id→id] users_primary_profile_id_fkey",
				"O2O public.users -> public.profiles backup_profile (profiles) [backup_profile_id→id] users_backup_profile_id_fkey",
			},
		},
		{
			name: "single-column self-reference",
			ddl: `
CREATE TABLE employees (id bigint PRIMARY KEY, manager_id bigint REFERENCES employees(id));`,
			want: []string{
				"O2M public.employees -> public.employees employees [manager_id→id] employees_manager_id_fkey",
				"O2O public.employees -> public.employees manager (employees) [manager_id→id] employees_manager_id_fkey",
			},
		},
		{
			name: "composite self-reference",
			ddl: workspacesDDL + `
CREATE TABLE tasks (id bigint PRIMARY KEY, workspace_id bigint NOT NULL REFERENCES workspaces(id), parent_task_id bigint, UNIQUE (workspace_id, id),
  FOREIGN KEY (workspace_id, parent_task_id) REFERENCES tasks (workspace_id, id));`,
			want: []string{
				"O2M public.workspaces -> public.tasks tasks [workspace_id→id] tasks_workspace_id_fkey",
				"O2O public.tasks -> public.workspaces workspace (workspaces) [workspace_id→id] tasks_workspace_id_fkey",
				"O2M public.tasks -> public.tasks tasks [workspace_id→workspace_id parent_task_id→id] tasks_workspace_id_parent_task_id_fkey",
				"O2O public.tasks -> public.tasks parent_task (tasks) [workspace_id→workspace_id parent_task_id→id] tasks_workspace_id_parent_task_id_fkey",
			},
		},
		{
			name: "self-referential M2M names each direction from its reference key",
			ddl: `
CREATE TABLE tasks (id bigint PRIMARY KEY);
CREATE TABLE task_dependencies (task_id bigint REFERENCES tasks(id), depends_on_task_id bigint REFERENCES tasks(id), PRIMARY KEY (task_id, depends_on_task_id));`,
			want: []string{
				"M2M public.tasks -> public.tasks depends_on_task (tasks) via public.task_dependencies [task_id→id] [depends_on_task_id→id] task_dependencies_task_id_fkey / task_dependencies_depends_on_task_id_fkey",
				"M2M public.tasks -> public.tasks task (tasks) via public.task_dependencies [depends_on_task_id→id] [task_id→id] task_dependencies_depends_on_task_id_fkey / task_dependencies_task_id_fkey",
			},
		},
		{
			name: "cycle_notes names its directions cycle and prev_cycle from the second naming step",
			ddl: workspacesDDL + `
CREATE TABLE cycles (workspace_id bigint NOT NULL REFERENCES workspaces(id), id bigint NOT NULL, PRIMARY KEY (workspace_id, id));
CREATE TABLE cycle_notes (workspace_id bigint NOT NULL REFERENCES workspaces(id), cycle_id bigint NOT NULL, prev_cycle_id bigint NOT NULL, n int NOT NULL,
  PRIMARY KEY (workspace_id, cycle_id, prev_cycle_id, n),
  FOREIGN KEY (workspace_id, cycle_id) REFERENCES cycles (workspace_id, id),
  FOREIGN KEY (workspace_id, prev_cycle_id) REFERENCES cycles (workspace_id, id));`,
			want: []string{
				"O2M public.workspaces -> public.cycles cycles [workspace_id→id] cycles_workspace_id_fkey",
				"O2O public.cycles -> public.workspaces workspace (workspaces) [workspace_id→id] cycles_workspace_id_fkey",
				"M2M public.cycles -> public.cycles prev_cycle (cycles) via public.cycle_notes [workspace_id→workspace_id cycle_id→id] [workspace_id→workspace_id prev_cycle_id→id] cycle_notes_workspace_id_cycle_id_fkey / cycle_notes_workspace_id_prev_cycle_id_fkey",
				"M2M public.cycles -> public.cycles cycle (cycles) via public.cycle_notes [workspace_id→workspace_id prev_cycle_id→id] [workspace_id→workspace_id cycle_id→id] cycle_notes_workspace_id_prev_cycle_id_fkey / cycle_notes_workspace_id_cycle_id_fkey",
			},
		},
		{
			name: "a member shared with another FK names from the target's single-column PK",
			ddl: `
CREATE TABLE users (id bigint PRIMARY KEY);
CREATE TABLE accounts (user_id bigint NOT NULL REFERENCES users(id), n int NOT NULL, PRIMARY KEY (user_id, n));
CREATE TABLE audit (id bigint PRIMARY KEY, user_id bigint NOT NULL REFERENCES users(id), approver_id bigint REFERENCES users(id), n int,
  FOREIGN KEY (user_id, n) REFERENCES accounts (user_id, n));`,
			want: []string{
				"O2M public.users -> public.accounts accounts [user_id→id] accounts_user_id_fkey",
				"O2O public.accounts -> public.users user (users) [user_id→id] accounts_user_id_fkey",
				"O2M public.users -> public.audit user_audit (audit) [user_id→id] audit_user_id_fkey",
				"O2O public.audit -> public.users user (users) [user_id→id] audit_user_id_fkey",
				"O2M public.accounts -> public.audit audit [user_id→user_id n→n] audit_user_id_n_fkey",
				"O2O public.audit -> public.accounts accounts [user_id→user_id n→n] audit_user_id_n_fkey",
				"O2M public.users -> public.audit approver_audit (audit) [approver_id→id] audit_approver_id_fkey",
				"O2O public.audit -> public.users approver (users) [approver_id→id] audit_approver_id_fkey",
			},
		},
		{
			name: "two edges with no naming member keep one name for gen to reject",
			ddl: `
CREATE TABLE pairs (x int NOT NULL, y int NOT NULL, PRIMARY KEY (x, y));
CREATE TABLE links (id int PRIMARY KEY, a int, b int,
  CONSTRAINT links_ab FOREIGN KEY (a, b) REFERENCES pairs (x, y),
  CONSTRAINT links_ba FOREIGN KEY (b, a) REFERENCES pairs (x, y));`,
			want: []string{
				"O2M public.pairs -> public.links links [a→x b→y] links_ab",
				"O2O public.links -> public.pairs pairs [a→x b→y] links_ab",
				"O2M public.pairs -> public.links links [b→x a→y] links_ba",
				"O2O public.links -> public.pairs pairs [b→x a→y] links_ba",
			},
		},
		{
			name: "two junctions between one pair are named after the junctions",
			ddl: `
CREATE TABLE teams (id bigint PRIMARY KEY);
CREATE TABLE users (id bigint PRIMARY KEY);
CREATE TABLE team_members (team_id bigint REFERENCES teams(id), user_id bigint REFERENCES users(id), PRIMARY KEY (team_id, user_id));
CREATE TABLE team_managers (team_id bigint REFERENCES teams(id), user_id bigint REFERENCES users(id), PRIMARY KEY (team_id, user_id));`,
			want: []string{
				"M2M public.teams -> public.users team_members (users) via public.team_members [team_id→id] [user_id→id] team_members_team_id_fkey / team_members_user_id_fkey",
				"M2M public.users -> public.teams team_members (teams) via public.team_members [user_id→id] [team_id→id] team_members_user_id_fkey / team_members_team_id_fkey",
				"M2M public.teams -> public.users team_managers (users) via public.team_managers [team_id→id] [user_id→id] team_managers_team_id_fkey / team_managers_user_id_fkey",
				"M2M public.users -> public.teams team_managers (teams) via public.team_managers [user_id→id] [team_id→id] team_managers_user_id_fkey / team_managers_team_id_fkey",
			},
		},
		{
			name: "an M2M and an O2M between one pair are both renamed",
			ddl: `
CREATE TABLE organizations (id bigint PRIMARY KEY);
CREATE TABLE users (id bigint PRIMARY KEY, workspace_id bigint REFERENCES organizations(id));
CREATE TABLE organization_users (organization_id bigint REFERENCES organizations(id), user_id bigint REFERENCES users(id), PRIMARY KEY (organization_id, user_id));`,
			want: []string{
				"O2M public.organizations -> public.users workspace_users (users) [workspace_id→id] users_workspace_id_fkey",
				"O2O public.users -> public.organizations workspace (organizations) [workspace_id→id] users_workspace_id_fkey",
				"M2M public.organizations -> public.users organization_users (users) via public.organization_users [organization_id→id] [user_id→id] organization_users_organization_id_fkey / organization_users_user_id_fkey",
				"M2M public.users -> public.organizations organizations via public.organization_users [user_id→id] [organization_id→id] organization_users_user_id_fkey / organization_users_organization_id_fkey",
			},
		},
		{
			name: "circular references",
			ddl: `
CREATE TABLE a (id bigint PRIMARY KEY, b_id bigint);
CREATE TABLE b (id bigint PRIMARY KEY, a_id bigint REFERENCES a(id));
ALTER TABLE a ADD FOREIGN KEY (b_id) REFERENCES b(id);`,
			want: []string{
				"O2M public.b -> public.a a [b_id→id] a_b_id_fkey",
				"O2O public.a -> public.b b [b_id→id] a_b_id_fkey",
				"O2M public.a -> public.b b [a_id→id] b_a_id_fkey",
				"O2O public.b -> public.a a [a_id→id] b_a_id_fkey",
			},
		},
		{
			name: "no foreign keys",
			ddl:  `CREATE TABLE users (id bigint PRIMARY KEY, email text UNIQUE);`,
		},
	}
	check := func(t *testing.T, dialect, ddl string, pkOverrides map[string][]string, want, wantWarnings []string) {
		t.Helper()
		schema := parseDDL(t, dialect, ddl, pkOverrides)
		warnings := parser.DetectRelationships(schema)
		if diff := cmp.Diff(want, edgeStrings(schema.Relationships)); diff != "" {
			t.Errorf("DetectRelationships() edges mismatch (-want +got):\n%s", diff)
		}
		if diff := cmp.Diff(wantWarnings, warnings); diff != "" {
			t.Errorf("DetectRelationships() warnings mismatch (-want +got):\n%s", diff)
		}
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if !tt.allDialects {
				dialect := tt.dialect
				if dialect == "" {
					dialect = "postgres"
				}
				check(t, dialect, tt.ddl, tt.pkOverrides, tt.want, tt.wantWarnings)
				return
			}
			run := func(t *testing.T, dialect, prefix string) {
				t.Helper()
				want := make([]string, len(tt.want))
				for i, w := range tt.want {
					want[i] = strings.ReplaceAll(w, "@.", prefix)
				}
				check(t, dialect, tt.ddl, tt.pkOverrides, want, tt.wantWarnings)
			}
			t.Run("postgres", func(t *testing.T) { run(t, "postgres", "public.") })
			t.Run("mysql", func(t *testing.T) { run(t, "mysql", "") })
			t.Run("sqlite", func(t *testing.T) { run(t, "sqlite", "") })
		})
	}
}

// Detection replaces the relationships it found before, so a second run
// changes nothing.
func TestDetectRelationships_Idempotent(t *testing.T) {
	schema := parseDDL(t, "postgres", workspacesDDL+`
CREATE TABLE tasks (id bigint PRIMARY KEY, workspace_id bigint NOT NULL REFERENCES workspaces(id), UNIQUE (workspace_id, id));
CREATE TABLE labels (id bigint PRIMARY KEY, workspace_id bigint NOT NULL REFERENCES workspaces(id), UNIQUE (workspace_id, id));
CREATE TABLE task_labels (workspace_id bigint NOT NULL, task_id bigint NOT NULL, label_id bigint NOT NULL,
  PRIMARY KEY (workspace_id, task_id, label_id),
  FOREIGN KEY (workspace_id, task_id) REFERENCES tasks (workspace_id, id),
  FOREIGN KEY (workspace_id, label_id) REFERENCES labels (workspace_id, id));
CREATE TABLE members (id bigint PRIMARY KEY, assignee_of bigint REFERENCES tasks(id), reviewer_of bigint REFERENCES tasks(id));`, nil)
	firstWarnings := parser.DetectRelationships(schema)
	first := slices.Clone(schema.Relationships)
	secondWarnings := parser.DetectRelationships(schema)
	if diff := cmp.Diff(first, schema.Relationships); diff != "" {
		t.Errorf("second DetectRelationships() changed the relationships (-first +second):\n%s", diff)
	}
	if diff := cmp.Diff(firstWarnings, secondWarnings); diff != "" {
		t.Errorf("second DetectRelationships() changed the warnings (-first +second):\n%s", diff)
	}
}

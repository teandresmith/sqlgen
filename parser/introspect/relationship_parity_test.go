package introspect_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/teandresmith/sqlgen/parser"
	"github.com/teandresmith/sqlgen/parser/introspect"
	"github.com/teandresmith/sqlgen/parser/sqlite"
)

// The relationship shapes of PRD §5.3 on one schema. tasks holds a foreign key
// inside the composite one (redundant) and two identical composite keys whose
// names sort against their declared order: the one whose name sorts first
// gives the edge under both sources.
const pgRelationshipFixture = `
CREATE SCHEMA pgi_rel;
CREATE TABLE pgi_rel.workspaces (id bigint PRIMARY KEY);
CREATE TABLE pgi_rel.cycles (id bigint PRIMARY KEY, workspace_id bigint NOT NULL REFERENCES pgi_rel.workspaces (id), code text NOT NULL UNIQUE, UNIQUE (workspace_id, id));
CREATE TABLE pgi_rel.tasks (
	id bigint PRIMARY KEY,
	workspace_id bigint NOT NULL REFERENCES pgi_rel.workspaces (id),
	cycle_id bigint REFERENCES pgi_rel.cycles (id),
	cycle_code text REFERENCES pgi_rel.cycles (code),
	UNIQUE (workspace_id, id),
	CONSTRAINT tasks_z_cycle FOREIGN KEY (workspace_id, cycle_id) REFERENCES pgi_rel.cycles (workspace_id, id) MATCH FULL,
	CONSTRAINT tasks_a_cycle FOREIGN KEY (workspace_id, cycle_id) REFERENCES pgi_rel.cycles (workspace_id, id) MATCH FULL
);
CREATE TABLE pgi_rel.labels (id bigint PRIMARY KEY, workspace_id bigint NOT NULL REFERENCES pgi_rel.workspaces (id), UNIQUE (workspace_id, id));
CREATE TABLE pgi_rel.task_labels (
	workspace_id bigint NOT NULL REFERENCES pgi_rel.workspaces (id),
	task_id bigint NOT NULL,
	label_id bigint NOT NULL,
	PRIMARY KEY (workspace_id, task_id, label_id),
	FOREIGN KEY (workspace_id, task_id) REFERENCES pgi_rel.tasks (workspace_id, id),
	FOREIGN KEY (workspace_id, label_id) REFERENCES pgi_rel.labels (workspace_id, id)
);
CREATE TABLE pgi_rel.cycle_settings (
	workspace_id bigint NOT NULL,
	cycle_id bigint NOT NULL,
	PRIMARY KEY (workspace_id, cycle_id),
	FOREIGN KEY (workspace_id, cycle_id) REFERENCES pgi_rel.cycles (workspace_id, id)
);
CREATE TABLE pgi_rel.settings (workspace_id bigint NOT NULL, key text NOT NULL, PRIMARY KEY (workspace_id, key));
CREATE TABLE pgi_rel.setting_refs (
	id bigint PRIMARY KEY,
	workspace_id bigint NOT NULL,
	setting_key text,
	FOREIGN KEY (workspace_id, setting_key) REFERENCES pgi_rel.settings (workspace_id, key)
)`

// mysqlRelationshipFixture is pgRelationshipFixture on MySQL, plus a key into
// indexed columns that are not a key. MySQL ignores a foreign key written on a
// column, so every key here is a table constraint.
const mysqlRelationshipFixture = `
CREATE TABLE r_workspaces (id bigint PRIMARY KEY);
CREATE TABLE r_cycles (id bigint PRIMARY KEY, workspace_id bigint NOT NULL, code varchar(20) NOT NULL UNIQUE, UNIQUE (workspace_id, id),
	CONSTRAINT r_cycles_ws FOREIGN KEY (workspace_id) REFERENCES r_workspaces (id));
CREATE TABLE r_tasks (
	id bigint PRIMARY KEY,
	workspace_id bigint NOT NULL,
	cycle_id bigint,
	cycle_code varchar(20),
	UNIQUE (workspace_id, id),
	CONSTRAINT r_tasks_ws FOREIGN KEY (workspace_id) REFERENCES r_workspaces (id),
	CONSTRAINT r_tasks_cycle FOREIGN KEY (cycle_id) REFERENCES r_cycles (id),
	CONSTRAINT r_tasks_code FOREIGN KEY (cycle_code) REFERENCES r_cycles (code),
	CONSTRAINT r_tasks_z_cycle FOREIGN KEY (workspace_id, cycle_id) REFERENCES r_cycles (workspace_id, id) MATCH FULL,
	CONSTRAINT r_tasks_a_cycle FOREIGN KEY (workspace_id, cycle_id) REFERENCES r_cycles (workspace_id, id)
);
CREATE TABLE r_labels (id bigint PRIMARY KEY, workspace_id bigint NOT NULL, UNIQUE (workspace_id, id),
	CONSTRAINT r_labels_ws FOREIGN KEY (workspace_id) REFERENCES r_workspaces (id));
CREATE TABLE r_task_labels (
	workspace_id bigint NOT NULL,
	task_id bigint NOT NULL,
	label_id bigint NOT NULL,
	PRIMARY KEY (workspace_id, task_id, label_id),
	CONSTRAINT r_task_labels_ws FOREIGN KEY (workspace_id) REFERENCES r_workspaces (id),
	CONSTRAINT r_task_labels_task FOREIGN KEY (workspace_id, task_id) REFERENCES r_tasks (workspace_id, id),
	CONSTRAINT r_task_labels_label FOREIGN KEY (workspace_id, label_id) REFERENCES r_labels (workspace_id, id)
);
CREATE TABLE r_cycle_settings (
	workspace_id bigint NOT NULL,
	cycle_id bigint NOT NULL,
	PRIMARY KEY (workspace_id, cycle_id),
	CONSTRAINT r_cycle_settings_cycle FOREIGN KEY (workspace_id, cycle_id) REFERENCES r_cycles (workspace_id, id)
);
CREATE TABLE r_settings (workspace_id bigint NOT NULL, ` + "`key`" + ` varchar(20) NOT NULL, PRIMARY KEY (workspace_id, ` + "`key`" + `));
CREATE TABLE r_setting_refs (
	id bigint PRIMARY KEY,
	workspace_id bigint NOT NULL,
	setting_key varchar(20),
	CONSTRAINT r_setting_refs_setting FOREIGN KEY (workspace_id, setting_key) REFERENCES r_settings (workspace_id, ` + "`key`" + `)
);
CREATE TABLE r_parents (id int PRIMARY KEY, b int, c int, KEY (b, c));
CREATE TABLE r_children (id int PRIMARY KEY, a int, b int, CONSTRAINT r_children_bc FOREIGN KEY (a, b) REFERENCES r_parents (b, c))`

const mysqlRelationshipFixtureReset = `
DROP TABLE IF EXISTS r_children;
DROP TABLE IF EXISTS r_parents;
DROP TABLE IF EXISTS r_setting_refs;
DROP TABLE IF EXISTS r_settings;
DROP TABLE IF EXISTS r_cycle_settings;
DROP TABLE IF EXISTS r_task_labels;
DROP TABLE IF EXISTS r_labels;
DROP TABLE IF EXISTS r_tasks;
DROP TABLE IF EXISTS r_cycles;
DROP TABLE IF EXISTS r_workspaces`

// sqliteRelationshipFixture is pgRelationshipFixture on SQLite, whose foreign
// keys are unnamed, plus a key into a column that is not a key and one written
// with no column list.
const sqliteRelationshipFixture = `
CREATE TABLE workspaces (id INTEGER PRIMARY KEY, slug TEXT);
CREATE TABLE cycles (id INTEGER PRIMARY KEY, workspace_id INTEGER NOT NULL REFERENCES workspaces (id), code TEXT NOT NULL UNIQUE, UNIQUE (workspace_id, id));
CREATE TABLE tasks (
	id INTEGER PRIMARY KEY,
	workspace_id INTEGER NOT NULL REFERENCES workspaces (id),
	cycle_id INTEGER REFERENCES cycles,
	cycle_code TEXT REFERENCES cycles (code),
	UNIQUE (workspace_id, id),
	FOREIGN KEY (workspace_id, cycle_id) REFERENCES cycles (workspace_id, id) MATCH FULL,
	FOREIGN KEY (workspace_id, cycle_id) REFERENCES cycles (workspace_id, id)
);
CREATE TABLE labels (id INTEGER PRIMARY KEY, workspace_id INTEGER NOT NULL REFERENCES workspaces (id), UNIQUE (workspace_id, id));
CREATE TABLE task_labels (
	workspace_id INTEGER NOT NULL REFERENCES workspaces (id),
	task_id INTEGER NOT NULL,
	label_id INTEGER NOT NULL,
	PRIMARY KEY (workspace_id, task_id, label_id),
	FOREIGN KEY (workspace_id, task_id) REFERENCES tasks (workspace_id, id),
	FOREIGN KEY (workspace_id, label_id) REFERENCES labels (workspace_id, id)
);
CREATE TABLE cycle_settings (
	workspace_id INTEGER NOT NULL,
	cycle_id INTEGER NOT NULL,
	PRIMARY KEY (workspace_id, cycle_id),
	FOREIGN KEY (workspace_id, cycle_id) REFERENCES cycles (workspace_id, id)
);
CREATE TABLE settings (workspace_id INTEGER NOT NULL, key TEXT NOT NULL, PRIMARY KEY (workspace_id, key));
CREATE TABLE setting_refs (
	id INTEGER PRIMARY KEY,
	workspace_id INTEGER NOT NULL,
	setting_key TEXT,
	FOREIGN KEY (workspace_id, setting_key) REFERENCES settings (workspace_id, key)
);
CREATE TABLE slugged (id INTEGER PRIMARY KEY, workspace_slug TEXT REFERENCES workspaces (slug))`

// Relationship detection reads the schema alone, so it gives the same edges and
// warnings under `files` and `database` (PRD §5.3). Each fixture's MATCH types
// survive both sources too: FULL on PostgreSQL, SIMPLE on MySQL and SQLite.
func TestDetectRelationships_SourceParity(t *testing.T) {
	requireDatabases(t)

	t.Run("postgres", func(t *testing.T) {
		parsed, introspected := pgParityCase(t, "DROP SCHEMA IF EXISTS pgi_rel CASCADE", pgRelationshipFixture, "pgi_rel")
		assertMatch(t, introspected, "tasks", "tasks_a_cycle", parser.MatchFull)
		// Detection runs first: assertSchemaParity sorts the constraints in place.
		defer assertSchemaParity(t, "postgres", parsed, introspected)
		assertDetectionParity(t, "postgres", parsed, introspected, []string{
			"O2M pgi_rel.workspaces -> pgi_rel.cycles cycles [workspace_id→id] cycles_workspace_id_fkey",
			"O2O pgi_rel.cycles -> pgi_rel.workspaces workspace (workspaces) [workspace_id→id] cycles_workspace_id_fkey",
			"O2O pgi_rel.cycle_settings -> pgi_rel.cycles cycle (cycles) [workspace_id→workspace_id cycle_id→id] cycle_settings_workspace_id_cycle_id_fkey",
			"O2M pgi_rel.workspaces -> pgi_rel.labels labels [workspace_id→id] labels_workspace_id_fkey",
			"O2M pgi_rel.settings -> pgi_rel.setting_refs setting_refs [workspace_id→workspace_id setting_key→key] setting_refs_workspace_id_setting_key_fkey",
			"M2M pgi_rel.tasks -> pgi_rel.labels labels via pgi_rel.task_labels [workspace_id→workspace_id task_id→id] [workspace_id→workspace_id label_id→id]",
			"M2M pgi_rel.labels -> pgi_rel.tasks tasks via pgi_rel.task_labels [workspace_id→workspace_id label_id→id] [workspace_id→workspace_id task_id→id]",
			"O2M pgi_rel.workspaces -> pgi_rel.tasks tasks [workspace_id→id] tasks_workspace_id_fkey",
			"O2M pgi_rel.cycles -> pgi_rel.tasks cycle_tasks (tasks) [workspace_id→workspace_id cycle_id→id] tasks_a_cycle",
			"O2O pgi_rel.tasks -> pgi_rel.cycles cycle (cycles) [workspace_id→workspace_id cycle_id→id] tasks_a_cycle",
			"O2M pgi_rel.cycles -> pgi_rel.tasks cycle_code_tasks (tasks) [cycle_code→code] tasks_cycle_code_fkey",
		}, nil)
	})

	t.Run("mysql", func(t *testing.T) {
		applyParityFixture(t, "mysql", mysqlConnStr, mysqlRelationshipFixtureReset, mysqlRelationshipFixture)
		tables := []string{
			"r_workspaces", "r_cycles", "r_tasks", "r_labels", "r_task_labels",
			"r_cycle_settings", "r_settings", "r_setting_refs", "r_parents", "r_children",
		}
		parsed, introspected := parseMySQL(t, mysqlRelationshipFixture), introspectMySQL(t, tables...)
		assertConstraintParity(t, parsed, introspected, tables...)
		assertMatch(t, introspected, "r_tasks", "r_tasks_z_cycle", parser.MatchSimple)
		assertDetectionParity(t, "mysql", parsed, introspected, []string{
			"O2O r_cycles -> r_workspaces workspace (r_workspaces) [workspace_id→id] r_cycles_ws",
			"O2O r_cycle_settings -> r_cycles cycle (r_cycles) [workspace_id→workspace_id cycle_id→id] r_cycle_settings_cycle",
			"M2M r_tasks -> r_labels r_labels via r_task_labels [workspace_id→workspace_id task_id→id] [workspace_id→workspace_id label_id→id]",
			"O2M r_cycles -> r_tasks cycle_r_tasks (r_tasks) [workspace_id→workspace_id cycle_id→id] r_tasks_a_cycle",
			"O2O r_tasks -> r_cycles cycle (r_cycles) [workspace_id→workspace_id cycle_id→id] r_tasks_a_cycle",
			"O2M r_cycles -> r_tasks cycle_code_r_tasks (r_tasks) [cycle_code→code] r_tasks_code",
			"O2M r_settings -> r_setting_refs r_setting_refs [workspace_id→workspace_id setting_key→key] r_setting_refs_setting",
		}, []string{
			"tables.r_children: foreign key r_children_bc references columns that are neither the primary key nor a non-partial UNIQUE constraint of r_parents, so it gives no relationship; declare one by hand if you need it (PRD §5.3, §13.4)",
		})
	})

	t.Run("sqlite", func(t *testing.T) {
		dbPath := applySQLite(t, sqliteRelationshipFixture)
		parsed := parseStatements(t, sqlite.New(), sqliteRelationshipFixture)
		introspected := introspectSQLite(t, dbPath, introspect.IntrospectionOptions{})
		assertConstraintParity(t, parsed, introspected, tableNames(parsed)...)
		assertMatch(t, introspected, "tasks", "", parser.MatchSimple)
		assertDetectionParity(t, "sqlite", parsed, introspected, []string{
			"O2O cycles -> workspaces workspace (workspaces) [workspace_id→id] cycles(workspace_id) → workspaces(id)",
			"O2O cycle_settings -> cycles cycle (cycles) [workspace_id→workspace_id cycle_id→id] cycle_settings(workspace_id, cycle_id) → cycles(workspace_id, id)",
			"M2M tasks -> labels labels via task_labels [workspace_id→workspace_id task_id→id] [workspace_id→workspace_id label_id→id]",
			"O2M cycles -> tasks cycle_tasks (tasks) [workspace_id→workspace_id cycle_id→id] tasks(workspace_id, cycle_id) → cycles(workspace_id, id)",
			"O2O tasks -> cycles cycle (cycles) [workspace_id→workspace_id cycle_id→id] tasks(workspace_id, cycle_id) → cycles(workspace_id, id)",
			"O2M cycles -> tasks cycle_code_tasks (tasks) [cycle_code→code] tasks(cycle_code) → cycles(code)",
			"O2M settings -> setting_refs setting_refs [workspace_id→workspace_id setting_key→key] setting_refs(workspace_id, setting_key) → settings(workspace_id, key)",
		}, []string{
			"tables.slugged: foreign key slugged(workspace_slug) → workspaces(slug) references columns that are neither the primary key nor a non-partial UNIQUE constraint of workspaces, so it gives no relationship; declare one by hand if you need it (PRD §5.3, §13.4)",
		})
	})
}

// assertMatch checks the MATCH type of every FOREIGN KEY of table named name,
// or of every one when name is "".
func assertMatch(t *testing.T, s *parser.Schema, table, name string, want parser.MatchType) {
	t.Helper()
	found := false
	for _, c := range findTable(t, s, table).Constraints {
		if c.Type != parser.ForeignKey || name != "" && c.Name != name {
			continue
		}
		found = true
		if c.Match != want {
			t.Errorf("%s: FOREIGN KEY %s %v Match = %v, want %v", table, c.Name, c.Columns, c.Match, want)
		}
	}
	if !found {
		t.Errorf("%s: no FOREIGN KEY %q", table, name)
	}
}

// assertDetectionParity runs detection on both schemas as the CLI does and
// compares the results. want lists edges the parsed schema must give, in
// relationshipString's form; wantWarnings lists its warnings exactly.
func assertDetectionParity(t *testing.T, dialect string, parsed, introspected *parser.Schema, want, wantWarnings []string) {
	t.Helper()
	var warnings [2][]string
	for i, s := range []*parser.Schema{parsed, introspected} {
		parser.ResolveReferenceNames(s, dialect)
		s.Sort()
		warnings[i] = parser.DetectRelationships(s)
	}
	if diff := cmp.Diff(parsed.Relationships, introspected.Relationships); diff != "" {
		t.Errorf("%s: introspected relationships differ from the file parser's (-parsed +introspected):\n%s", dialect, diff)
	}
	if diff := cmp.Diff(warnings[0], warnings[1]); diff != "" {
		t.Errorf("%s: introspected detection warnings differ from the file parser's (-parsed +introspected):\n%s", dialect, diff)
	}
	if diff := cmp.Diff(wantWarnings, warnings[0]); diff != "" {
		t.Errorf("%s: detection warnings mismatch (-want +got):\n%s", dialect, diff)
	}
	got := make(map[string]bool, len(parsed.Relationships))
	for _, r := range parsed.Relationships {
		got[relationshipString(r)] = true
	}
	for _, w := range want {
		if !got[w] {
			t.Errorf("%s: DetectRelationships() did not give %q", dialect, w)
		}
	}
	if t.Failed() {
		for _, r := range parsed.Relationships {
			t.Logf("%s: got %s", dialect, relationshipString(r))
		}
	}
}

// relationshipString spells an edge as
// "<kind> <source> -> <target> <name> [<local→referenced>…] <constraint>", with
// "(<base>)" after a renamed edge's name. An M2M edge lists both junction keys.
func relationshipString(r parser.Relationship) string {
	pairs := func(key []parser.KeyPair) string {
		s := make([]string, len(key))
		for i, p := range key {
			s[i] = p.Local + "→" + p.Referenced
		}
		return "[" + strings.Join(s, " ") + "]"
	}
	name := r.Name
	if r.BaseName != r.Name {
		name += " (" + r.BaseName + ")"
	}
	switch r.Type {
	case parser.ManyToMany:
		return fmt.Sprintf("M2M %s -> %s %s via %s %s %s", r.SourceTable, r.TargetTable, name, r.JunctionTable, pairs(r.JunctionLocal), pairs(r.JunctionReference))
	case parser.OneToOne:
		return fmt.Sprintf("O2O %s -> %s %s %s %s", r.SourceTable, r.TargetTable, name, pairs(r.Key), r.Constraint)
	default:
		return fmt.Sprintf("O2M %s -> %s %s %s %s", r.SourceTable, r.TargetTable, name, pairs(r.Key), r.Constraint)
	}
}

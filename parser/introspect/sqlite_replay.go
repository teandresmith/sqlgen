package introspect

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"

	"github.com/teandresmith/sqlgen/parser"
)

// mergeSQLiteViews checks the SQL files against SQLite itself and reads
// schema's views again as SQLite leaves them (PRD §6.1). It replays
// schema.SQLiteSchema, the database's schema statements, into an in-memory
// database, then runs each file's text there in order. A statement SQLite
// refuses is an error naming the file, and so is a view that ran before a
// file and fails after it, as the database's view stands under source:
// both. A file that needs a collation, function or virtual-table module
// this SQLite lacks, which the tool that runs the migrations may have, is
// a warning instead, and it and the files after it are not checked, as is
// one that runs ATTACH DATABASE or VACUUM, which would write a file. Each
// view's columns are then read from the view as SQLite stores it, which
// follows a table or column rename the checked files make, against the
// tables as the files left them.
func mergeSQLiteViews(ctx context.Context, schema *parser.Schema, files []string) error {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		return fmt.Errorf("opening an in-memory SQLite database: %w", err)
	}
	defer db.Close() //nolint:errcheck // an in-memory database; nothing to flush
	// Every connection to :memory: opens a database of its own.
	db.SetMaxOpenConns(1)
	if err := forbidAttach(ctx, db); err != nil {
		return err
	}

	running, err := replaySQLiteSchema(ctx, db, schema)
	if err != nil {
		return err
	}
	for _, file := range files {
		checked, err := replayFile(ctx, db, schema, file, running)
		if err != nil {
			return err
		}
		if !checked {
			break
		}
	}
	return readReplayedViews(ctx, db, schema, running)
}

// forbidAttach sets db's connection to attach no database, so a file's
// ATTACH DATABASE, and VACUUM, which attaches its output, fail with
// "too many attached databases" instead of writing a file.
func forbidAttach(ctx context.Context, db *sql.DB) error {
	conn, err := db.Conn(ctx)
	if err != nil {
		return fmt.Errorf("opening an in-memory SQLite database: %w", err)
	}
	defer conn.Close() //nolint:errcheck // returns the connection to db
	if _, err := sqlite.Limit(conn, sqlite3.SQLITE_LIMIT_ATTACHED, 0); err != nil {
		return fmt.Errorf("limiting the in-memory SQLite database: %w", err)
	}
	return nil
}

// replayFile runs file in db and checks that every view of running that
// db still has runs. It returns false, with a warning in schema.Warnings,
// when SQLite cannot check the file because it needs a collation, function
// or virtual-table module this SQLite lacks, or runs ATTACH DATABASE or
// VACUUM.
func replayFile(ctx context.Context, db *sql.DB, schema *parser.Schema, file string, running []string) (bool, error) {
	text, err := os.ReadFile(filepath.Clean(file))
	if err != nil {
		return false, fmt.Errorf("reading %s: %w", file, err)
	}
	if _, err := db.ExecContext(ctx, string(text)); err != nil {
		if reason, ok := uncheckedReason(err); ok {
			schema.Warnings = append(schema.Warnings, uncheckedFileWarning(file, reason))
			return false, nil
		}
		return false, fmt.Errorf("%s: SQLite refuses it: %w", file, err)
	}
	for _, view := range running {
		ddl, err := storedView(ctx, db, view)
		if err != nil {
			return false, err
		}
		if ddl == "" {
			continue // dropped by the file; the database's view stands
		}
		if err := runView(ctx, db, view); err != nil {
			if reason, ok := uncheckedReason(err); ok {
				schema.Warnings = append(schema.Warnings, uncheckedFileWarning(file, reason))
				return false, nil
			}
			return false, fmt.Errorf("%s leaves view %s failing: %w; under source: both the database's view stands, so change it in the database first", file, view, err)
		}
	}
	return true, nil
}

// uncheckedFileWarning is the warning for a file SQLite cannot check, for
// reason.
func uncheckedFileWarning(file, reason string) string {
	return fmt.Sprintf("%s %s, so SQLite does not check it or the SQL files after it, and the views' columns do not follow them", file, reason)
}

// sqliteAttach matches SQLite's refusal of ATTACH DATABASE, or of VACUUM,
// under forbidAttach.
var sqliteAttach = regexp.MustCompile(`too many attached databases`)

// uncheckedReason returns why SQLite cannot check a file that failed with
// err: a collation, function or virtual-table module this SQLite lacks, or
// an ATTACH DATABASE or VACUUM, which the check does not run.
func uncheckedReason(err error) (string, bool) {
	if obj, ok := missingObject(err); ok {
		return fmt.Sprintf("needs %s %s, which this SQLite lacks", obj.kind, obj.name), true
	}
	if sqliteAttach.MatchString(err.Error()) {
		return "runs ATTACH DATABASE or VACUUM, which the check does not run", true
	}
	return "", false
}

// sqliteObject is a collation, function or virtual-table module SQLite
// reports missing.
type sqliteObject struct {
	kind, name string
}

// sqliteMissing matches SQLite's message for a collation, function or
// virtual-table module it does not have: "no such function" when a
// statement is prepared, "unknown function" when one runs.
var sqliteMissing = regexp.MustCompile(`(no such collation sequence|no such function|unknown function|no such module): (\S+)`)

// missingObject returns the collation, function or module err reports
// missing.
func missingObject(err error) (sqliteObject, bool) {
	m := sqliteMissing.FindStringSubmatch(err.Error())
	if m == nil {
		return sqliteObject{}, false
	}
	kind := "function"
	switch m[1] {
	case "no such collation sequence":
		kind = "collation"
	case "no such module":
		kind = "module"
	}
	return sqliteObject{kind: kind, name: strings.TrimSuffix(m[2], "()")}, true
}

// replaySQLiteSchema runs schema.SQLiteSchema in db and returns the views
// that run. A table or index SQLite cannot create because it names a
// collation, function or virtual-table module this SQLite lacks is created
// as a placeholder of that name, and its statement is then written over
// the placeholder's, so the copy has the database's schema. A view that
// fails to run is not checked. One that fails for a virtual table's
// missing module is replaced in db by a view of its name that reads
// nothing, with a warning in schema.Warnings, since SQLite checks every
// view when a table or column is renamed or dropped and would refuse every
// such statement; a file can still drop it.
func replaySQLiteSchema(ctx context.Context, db *sql.DB, schema *parser.Schema) ([]string, error) {
	var placed []placeholder
	var modules []string
	for _, stmt := range schema.SQLiteSchema {
		_, err := db.ExecContext(ctx, stmt.SQL)
		if err == nil {
			continue
		}
		obj, ok := missingObject(err)
		if !ok || !createPlaceholder(ctx, db, stmt, obj) {
			return nil, fmt.Errorf("replaying the database's %s %s: %w", stmt.Type, stmt.Name, err)
		}
		placed = append(placed, placeholder{stmt: stmt, virtual: obj.kind == "module"})
		if obj.kind == "module" {
			modules = append(modules, stmt.Name)
		}
	}
	if err := restoreStatements(ctx, db, placed); err != nil {
		return nil, err
	}
	running, unread, err := replayedViews(ctx, db, schema.SQLiteSchema)
	if err != nil {
		return nil, err
	}
	if len(modules) > 0 {
		warning := fmt.Sprintf("virtual tables %s: this SQLite has no module for them", strings.Join(modules, ", "))
		if len(unread) > 0 {
			warning += fmt.Sprintf(", so the SQL files are not checked against views %s, which read them", strings.Join(unread, ", "))
		}
		schema.Warnings = append(schema.Warnings, warning)
	}
	return running, nil
}

// replayedViews returns the views of stmts that run in db, and those that
// fail for a virtual table's missing module, which it replaces in db by a
// view of the name that reads nothing.
func replayedViews(ctx context.Context, db *sql.DB, stmts []parser.SchemaStatement) (running, unread []string, err error) {
	for _, stmt := range stmts {
		if stmt.Type != "view" {
			continue
		}
		err := runView(ctx, db, stmt.Name)
		if err == nil {
			running = append(running, stmt.Name)
			continue
		}
		if obj, ok := missingObject(err); ok && obj.kind == "module" {
			unread = append(unread, stmt.Name)
		}
	}
	for _, view := range unread {
		name := quoteSQLiteIdent(view)
		if _, err := db.ExecContext(ctx, "DROP VIEW "+name+"; CREATE VIEW "+name+" AS SELECT NULL"); err != nil {
			return nil, nil, fmt.Errorf("replacing view %s in the replay: %w", view, err)
		}
	}
	return running, unread, nil
}

// createPlaceholder creates, in place of stmt, which SQLite refused for
// missing obj, an object of its name and type that SQLite accepts: an
// ordinary table for a virtual table, and otherwise stmt with each missing
// collation made BINARY and each missing function call made a json_array
// call, which keeps the table's or index's structure. It reports whether
// SQLite accepted one.
func createPlaceholder(ctx context.Context, db *sql.DB, stmt parser.SchemaStatement, obj sqliteObject) bool {
	if obj.kind == "module" {
		if stmt.Type != "table" {
			return false
		}
		_, err := db.ExecContext(ctx, "CREATE TABLE "+quoteSQLiteIdent(stmt.Name)+" (x)")
		return err == nil
	}
	text := stmt.SQL
	// Each pass makes one missing name SQLite reports known; a statement
	// names finitely many.
	for {
		next := neutralize(text, obj)
		if next == text {
			return false
		}
		text = next
		_, err := db.ExecContext(ctx, text)
		if err == nil {
			return true
		}
		var ok bool
		if obj, ok = missingObject(err); !ok || obj.kind == "module" {
			return false
		}
	}
}

// neutralize replaces, in a statement's text, the collation obj names with
// BINARY, or each call of the function obj names with a json_array call.
func neutralize(text string, obj sqliteObject) string {
	name := regexp.QuoteMeta(obj.name)
	quoted := `(?:"` + name + `"|'` + name + `'|` + "`" + name + "`" + `|\[` + name + `\]|` + name + `\b)`
	if obj.kind == "collation" {
		return regexp.MustCompile(`(?i)\bCOLLATE\s+`+quoted).ReplaceAllLiteralString(text, "COLLATE BINARY")
	}
	return regexp.MustCompile(`(?i)(^|[^\w$])`+quoted+`\s*\(`).ReplaceAllString(text, "${1}json_array(")
}

// placeholder is a database statement createPlaceholder stood in for;
// virtual when it stands in for a virtual table's missing module.
type placeholder struct {
	stmt    parser.SchemaStatement
	virtual bool
}

// restoreStatements writes each statement of placed over the placeholder
// createPlaceholder made for it, in db's sqlite_master, which SQLite then
// reads again. A virtual table's row has no b-tree, as SQLite stores one.
func restoreStatements(ctx context.Context, db *sql.DB, placed []placeholder) (err error) {
	if len(placed) == 0 {
		return nil
	}
	if _, err := db.ExecContext(ctx, "PRAGMA writable_schema = ON"); err != nil {
		return fmt.Errorf("restoring the database's schema in the replay: %w", err)
	}
	defer func() {
		if _, resetErr := db.ExecContext(ctx, "PRAGMA writable_schema = RESET"); resetErr != nil && err == nil {
			err = fmt.Errorf("restoring the database's schema in the replay: %w", resetErr)
		}
	}()
	for _, p := range placed {
		if _, err := db.ExecContext(ctx, `
			UPDATE sqlite_master
			SET sql = ?, rootpage = CASE WHEN ? THEN 0 ELSE rootpage END
			WHERE type = ? AND name = ?`,
			p.stmt.SQL, p.virtual, p.stmt.Type, p.stmt.Name); err != nil {
			return fmt.Errorf("restoring the database's %s %s in the replay: %w", p.stmt.Type, p.stmt.Name, err)
		}
	}
	return nil
}

// readReplayedViews reads each of schema's views that runs in db, a view
// of running, again from db, as SQLite stores it after the files, against
// schema's tables. A view the files dropped keeps its columns, and so does
// one that no longer runs, which only a file SQLite stopped checking
// partway through can leave.
func readReplayedViews(ctx context.Context, db *sql.DB, schema *parser.Schema, running []string) error {
	si := &SQLiteIntrospector{db: db}
	// A view that cannot be read the way it could before keeps the columns
	// it has; the introspection already warned about one that never could.
	scratch := &parser.Schema{Tables: schema.Tables}
	for i := range schema.Views {
		view := &schema.Views[i]
		if !slices.Contains(running, view.Name) {
			continue
		}
		ddl, err := storedView(ctx, db, view.Name)
		if err != nil {
			return err
		}
		if ddl == "" || runView(ctx, db, view.Name) != nil {
			continue
		}
		read, err := si.readView(ctx, view.Name, ddl, scratch)
		if err != nil {
			return err
		}
		view.Columns, view.Definition = read.Columns, read.Definition
	}
	return nil
}

// storedView returns the CREATE VIEW statement db stores for view, or ""
// when it has none.
func storedView(ctx context.Context, db *sql.DB, view string) (string, error) {
	var ddl string
	err := db.QueryRowContext(ctx, "SELECT sql FROM sqlite_master WHERE type = 'view' AND name = ?", view).Scan(&ddl)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("reading view %s from the replay: %w", view, err)
	}
	return ddl, nil
}

// runView returns the error SQLite gives for reading view.
func runView(ctx context.Context, db *sql.DB, view string) error {
	rows, err := db.QueryContext(ctx, "SELECT * FROM "+quoteSQLiteIdent(view)+" LIMIT 0") //nolint:gosec // an identifier, quoted; SQLite binds no parameter there
	if err != nil {
		return err //nolint:wrapcheck // the caller names the view and the file
	}
	return rows.Close() //nolint:wrapcheck // as above
}

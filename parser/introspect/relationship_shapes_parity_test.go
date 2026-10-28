package introspect_test

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/teandresmith/sqlgen/parser"
	"github.com/teandresmith/sqlgen/parser/introspect"
	"github.com/teandresmith/sqlgen/parser/mysql"
	"github.com/teandresmith/sqlgen/parser/postgres"
	"github.com/teandresmith/sqlgen/parser/sqlite"
)

// rdwShape is one relationship shape on the three dialects. ddl serves
// PostgreSQL and SQLite: "@." is the PostgreSQL schema prefix and is dropped
// on SQLite. mysqlDDL writes every foreign key as a named table constraint,
// as MySQL ignores one written on a column; "@" is its table prefix. A setup
// statement creates what the database needs but the files and the introspected
// schema leave out: a foreign key's target outside the schema.
type rdwShape struct {
	name      string
	pgSchema  string // also the "@." prefix; a pgSkip reason replaces it
	pgSkip    string
	pgSetup   string
	pgReset   string // extra reset statements, before the schema drop
	ddl       string
	mysqlPfx  string
	mysqlSkip string
	mysqlDDL  string
	mysqlExt  []string // tables the setup creates, outside the introspected set
	mysqlSet  string
	tables    []string // the files' tables, bare; MySQL prefixes them
	overrides map[string][]string
	wantPG    []string
	wantMySQL []string
	wantLite  []string
}

// Each relationship shape gives exactly the same edges and warnings when read
// from the SQL files as when introspected from the database the files built,
// on every dialect. Every edge is checked, both directions of an M2M
// included.
func TestDetectRelationships_ShapeSourceParity(t *testing.T) {
	requireDatabases(t)
	shapes := []rdwShape{
		{
			name:     "single-column UNIQUE FK is O2O",
			pgSchema: "rdw_ou",
			ddl: `CREATE TABLE @.users (id bigint PRIMARY KEY);
CREATE TABLE @.profiles (id bigint PRIMARY KEY, user_id bigint UNIQUE REFERENCES @.users (id))`,
			mysqlPfx: "ou_",
			mysqlDDL: `CREATE TABLE @users (id bigint PRIMARY KEY);
CREATE TABLE @profiles (id bigint PRIMARY KEY, user_id bigint UNIQUE, CONSTRAINT @profiles_user FOREIGN KEY (user_id) REFERENCES @users (id))`,
			tables:    []string{"users", "profiles"},
			wantPG:    []string{"O2O rdw_ou.profiles -> rdw_ou.users users [user_id→id] profiles_user_id_fkey"},
			wantMySQL: []string{"O2O ou_profiles -> ou_users ou_users [user_id→id] ou_profiles_user"},
			wantLite:  []string{"O2O profiles -> users users [user_id→id] profiles(user_id) → users(id)"},
		},
		{
			name:     "FK on the sole primary-key column is O2O",
			pgSchema: "rdw_sp",
			ddl: `CREATE TABLE @.users (id bigint PRIMARY KEY);
CREATE TABLE @.user_credentials (user_id bigint PRIMARY KEY REFERENCES @.users (id), secret text)`,
			mysqlPfx: "sp_",
			mysqlDDL: `CREATE TABLE @users (id bigint PRIMARY KEY);
CREATE TABLE @user_credentials (user_id bigint PRIMARY KEY, secret text, CONSTRAINT @credentials_user FOREIGN KEY (user_id) REFERENCES @users (id))`,
			tables:    []string{"users", "user_credentials"},
			wantPG:    []string{"O2O rdw_sp.user_credentials -> rdw_sp.users users [user_id→id] user_credentials_user_id_fkey"},
			wantMySQL: []string{"O2O sp_user_credentials -> sp_users sp_users [user_id→id] sp_credentials_user"},
			wantLite:  []string{"O2O user_credentials -> users users [user_id→id] user_credentials(user_id) → users(id)"},
		},
		{
			// MySQL needs an index whose leading columns are the referenced
			// ones in the FK's order, so its target key is UNIQUE (id,
			// workspace_id) with a plain index in the FK's order.
			name:     "key pairs follow the constraint's column order",
			pgSchema: "rdw_rc",
			ddl: `CREATE TABLE @.cycles (id bigint PRIMARY KEY, workspace_id bigint NOT NULL, UNIQUE (workspace_id, id));
CREATE TABLE @.tasks (id bigint PRIMARY KEY, workspace_id bigint NOT NULL, cycle_id bigint,
  FOREIGN KEY (cycle_id, workspace_id) REFERENCES @.cycles (id, workspace_id))`,
			mysqlPfx: "rc_",
			mysqlDDL: `CREATE TABLE @cycles (id bigint PRIMARY KEY, workspace_id bigint NOT NULL, UNIQUE (id, workspace_id), KEY (workspace_id, id));
CREATE TABLE @tasks (id bigint PRIMARY KEY, workspace_id bigint NOT NULL, cycle_id bigint,
  CONSTRAINT @tasks_cycle FOREIGN KEY (workspace_id, cycle_id) REFERENCES @cycles (workspace_id, id))`,
			tables:    []string{"cycles", "tasks"},
			wantPG:    []string{"O2M rdw_rc.cycles -> rdw_rc.tasks tasks [cycle_id→id workspace_id→workspace_id] tasks_cycle_id_workspace_id_fkey"},
			wantMySQL: []string{"O2M rc_cycles -> rc_tasks rc_tasks [workspace_id→workspace_id cycle_id→id] rc_tasks_cycle"},
			wantLite:  []string{"O2M cycles -> tasks tasks [cycle_id→id workspace_id→workspace_id] tasks(cycle_id, workspace_id) → cycles(id, workspace_id)"},
		},
		{
			name:     "FK into the declared primary key an override moves",
			pgSchema: "rdw_dpo",
			ddl: `CREATE TABLE @.cycles (id bigint PRIMARY KEY, code text NOT NULL);
CREATE TABLE @.tasks (id bigint PRIMARY KEY, cycle_id bigint REFERENCES @.cycles (id))`,
			mysqlPfx: "dpo_",
			mysqlDDL: `CREATE TABLE @cycles (id bigint PRIMARY KEY, code varchar(20) NOT NULL);
CREATE TABLE @tasks (id bigint PRIMARY KEY, cycle_id bigint, CONSTRAINT @tasks_cycle FOREIGN KEY (cycle_id) REFERENCES @cycles (id))`,
			tables:    []string{"cycles", "tasks"},
			overrides: map[string][]string{"cycles": {"code"}},
			wantPG:    []string{"O2M rdw_dpo.cycles -> rdw_dpo.tasks tasks [cycle_id→id] tasks_cycle_id_fkey"},
			wantMySQL: []string{"O2M dpo_cycles -> dpo_tasks dpo_tasks [cycle_id→id] dpo_tasks_cycle"},
			wantLite:  []string{"O2M cycles -> tasks tasks [cycle_id→id] tasks(cycle_id) → cycles(id)"},
		},
		{
			// The override target holds no UNIQUE, so only the override makes
			// it a key. MySQL accepts a foreign key into a plain index.
			name:   "FK into an override-resolved primary key",
			pgSkip: "PostgreSQL rejects a foreign key into columns no PRIMARY KEY or UNIQUE constraint covers, and with one the edge comes from that constraint",
			ddl: `CREATE TABLE @.cycles (id bigint PRIMARY KEY, code text NOT NULL);
CREATE TABLE @.tasks (id bigint PRIMARY KEY, cycle_code text REFERENCES @.cycles (code))`,
			mysqlPfx: "rpo_",
			mysqlDDL: `CREATE TABLE @cycles (id bigint PRIMARY KEY, code varchar(20) NOT NULL, KEY (code));
CREATE TABLE @tasks (id bigint PRIMARY KEY, cycle_code varchar(20), CONSTRAINT @tasks_code FOREIGN KEY (cycle_code) REFERENCES @cycles (code))`,
			tables:    []string{"cycles", "tasks"},
			overrides: map[string][]string{"cycles": {"code"}},
			wantMySQL: []string{"O2M rpo_cycles -> rpo_tasks rpo_tasks [cycle_code→code] rpo_tasks_code"},
			wantLite:  []string{"O2M cycles -> tasks tasks [cycle_code→code] tasks(cycle_code) → cycles(code)"},
		},
		{
			name:     "REFERENCES with no column list joins the primary key",
			pgSchema: "rdw_ncl",
			ddl: `CREATE TABLE @.accounts (label text, num bigint PRIMARY KEY);
CREATE TABLE @.posts (id bigint PRIMARY KEY, account_num bigint REFERENCES @.accounts)`,
			mysqlSkip: "MySQL's REFERENCES requires a column list",
			tables:    []string{"accounts", "posts"},
			wantPG:    []string{"O2M rdw_ncl.accounts -> rdw_ncl.posts posts [account_num→num] posts_account_num_fkey"},
			wantLite:  []string{"O2M accounts -> posts posts [account_num→num] posts(account_num) → accounts(num)"},
		},
		{
			name:     "FK into a table outside the schema gives no edge",
			pgSchema: "rdw_tos",
			pgSetup:  "CREATE SCHEMA rdw_tos_ext; CREATE TABLE rdw_tos_ext.accounts (id bigint PRIMARY KEY)",
			pgReset:  "DROP SCHEMA IF EXISTS rdw_tos_ext CASCADE",
			ddl: `CREATE TABLE @.users (id bigint PRIMARY KEY);
CREATE TABLE @.posts (id bigint PRIMARY KEY, author_id bigint REFERENCES @.users (id), account_id bigint REFERENCES rdw_tos_ext.accounts (id))`,
			mysqlPfx: "tos_",
			mysqlExt: []string{"accounts"},
			mysqlSet: "CREATE TABLE @accounts (id bigint PRIMARY KEY)",
			mysqlDDL: `CREATE TABLE @users (id bigint PRIMARY KEY);
CREATE TABLE @posts (id bigint PRIMARY KEY, author_id bigint, account_id bigint,
  CONSTRAINT @posts_author FOREIGN KEY (author_id) REFERENCES @users (id),
  CONSTRAINT @posts_account FOREIGN KEY (account_id) REFERENCES @accounts (id))`,
			tables:    []string{"users", "posts"},
			wantPG:    []string{"O2M rdw_tos.users -> rdw_tos.posts posts [author_id→id] posts_author_id_fkey"},
			wantMySQL: []string{"O2M tos_users -> tos_posts tos_posts [author_id→id] tos_posts_author"},
			wantLite:  []string{"O2M users -> posts posts [author_id→id] posts(author_id) → users(id)"},
		},
		{
			name:     "three single-column FKs in the primary key give three O2M edges",
			pgSchema: "rdw_tfj",
			ddl: `CREATE TABLE @.workspaces (id bigint PRIMARY KEY);
CREATE TABLE @.tasks (id bigint PRIMARY KEY);
CREATE TABLE @.labels (id bigint PRIMARY KEY);
CREATE TABLE @.task_labels (workspace_id bigint NOT NULL REFERENCES @.workspaces (id), task_id bigint NOT NULL REFERENCES @.tasks (id),
  label_id bigint NOT NULL REFERENCES @.labels (id), PRIMARY KEY (workspace_id, task_id, label_id))`,
			mysqlPfx: "tfj_",
			mysqlDDL: `CREATE TABLE @workspaces (id bigint PRIMARY KEY);
CREATE TABLE @tasks (id bigint PRIMARY KEY);
CREATE TABLE @labels (id bigint PRIMARY KEY);
CREATE TABLE @task_labels (workspace_id bigint NOT NULL, task_id bigint NOT NULL, label_id bigint NOT NULL,
  PRIMARY KEY (workspace_id, task_id, label_id),
  CONSTRAINT @tl_ws FOREIGN KEY (workspace_id) REFERENCES @workspaces (id),
  CONSTRAINT @tl_task FOREIGN KEY (task_id) REFERENCES @tasks (id),
  CONSTRAINT @tl_label FOREIGN KEY (label_id) REFERENCES @labels (id))`,
			tables: []string{"workspaces", "tasks", "labels", "task_labels"},
			wantPG: []string{
				"O2M rdw_tfj.workspaces -> rdw_tfj.task_labels task_labels [workspace_id→id] task_labels_workspace_id_fkey",
				"O2M rdw_tfj.tasks -> rdw_tfj.task_labels task_labels [task_id→id] task_labels_task_id_fkey",
				"O2M rdw_tfj.labels -> rdw_tfj.task_labels task_labels [label_id→id] task_labels_label_id_fkey",
			},
			wantMySQL: []string{
				"O2M tfj_workspaces -> tfj_task_labels tfj_task_labels [workspace_id→id] tfj_tl_ws",
				"O2M tfj_tasks -> tfj_task_labels tfj_task_labels [task_id→id] tfj_tl_task",
				"O2M tfj_labels -> tfj_task_labels tfj_task_labels [label_id→id] tfj_tl_label",
			},
			wantLite: []string{
				"O2M workspaces -> task_labels task_labels [workspace_id→id] task_labels(workspace_id) → workspaces(id)",
				"O2M tasks -> task_labels task_labels [task_id→id] task_labels(task_id) → tasks(id)",
				"O2M labels -> task_labels task_labels [label_id→id] task_labels(label_id) → labels(id)",
			},
		},
		{
			name:     "a junction with an end outside the schema gives no edge",
			pgSchema: "rdw_jee",
			pgSetup:  "CREATE SCHEMA rdw_jee_ext; CREATE TABLE rdw_jee_ext.users (id bigint PRIMARY KEY)",
			pgReset:  "DROP SCHEMA IF EXISTS rdw_jee_ext CASCADE",
			ddl: `CREATE TABLE @.teams (id bigint PRIMARY KEY);
CREATE TABLE @.team_members (team_id bigint NOT NULL REFERENCES @.teams (id), user_id bigint NOT NULL REFERENCES rdw_jee_ext.users (id), PRIMARY KEY (team_id, user_id))`,
			mysqlPfx: "jee_",
			mysqlExt: []string{"users"},
			mysqlSet: "CREATE TABLE @users (id bigint PRIMARY KEY)",
			mysqlDDL: `CREATE TABLE @teams (id bigint PRIMARY KEY);
CREATE TABLE @team_members (team_id bigint NOT NULL, user_id bigint NOT NULL, PRIMARY KEY (team_id, user_id),
  CONSTRAINT @tm_team FOREIGN KEY (team_id) REFERENCES @teams (id),
  CONSTRAINT @tm_user FOREIGN KEY (user_id) REFERENCES @users (id))`,
			tables: []string{"teams", "team_members"},
		},
		{
			name:     "a composite primary-key override over two FKs is not a junction",
			pgSchema: "rdw_pkj",
			ddl: `CREATE TABLE @.users (id bigint PRIMARY KEY);
CREATE TABLE @.tags (id bigint PRIMARY KEY);
CREATE TABLE @.user_tags (user_id bigint NOT NULL REFERENCES @.users (id), tag_id bigint NOT NULL REFERENCES @.tags (id))`,
			mysqlPfx: "pkj_",
			mysqlDDL: `CREATE TABLE @users (id bigint PRIMARY KEY);
CREATE TABLE @tags (id bigint PRIMARY KEY);
CREATE TABLE @user_tags (user_id bigint NOT NULL, tag_id bigint NOT NULL,
  CONSTRAINT @ut_user FOREIGN KEY (user_id) REFERENCES @users (id),
  CONSTRAINT @ut_tag FOREIGN KEY (tag_id) REFERENCES @tags (id))`,
			tables:    []string{"users", "tags", "user_tags"},
			overrides: map[string][]string{"user_tags": {"user_id", "tag_id"}},
			wantPG: []string{
				"O2M rdw_pkj.users -> rdw_pkj.user_tags user_tags [user_id→id] user_tags_user_id_fkey",
				"O2M rdw_pkj.tags -> rdw_pkj.user_tags user_tags [tag_id→id] user_tags_tag_id_fkey",
			},
			wantMySQL: []string{
				"O2M pkj_users -> pkj_user_tags pkj_user_tags [user_id→id] pkj_ut_user",
				"O2M pkj_tags -> pkj_user_tags pkj_user_tags [tag_id→id] pkj_ut_tag",
			},
			wantLite: []string{
				"O2M users -> user_tags user_tags [user_id→id] user_tags(user_id) → users(id)",
				"O2M tags -> user_tags user_tags [tag_id→id] user_tags(tag_id) → tags(id)",
			},
		},
		{
			name:     "order_items sets aside a real parent",
			pgSchema: "rdw_sar",
			ddl: `CREATE TABLE @.orders (id bigint PRIMARY KEY);
CREATE TABLE @.products (id bigint PRIMARY KEY);
CREATE TABLE @.warehouses (id bigint PRIMARY KEY);
CREATE TABLE @.stock (warehouse_id bigint NOT NULL REFERENCES @.warehouses (id), product_id bigint NOT NULL REFERENCES @.products (id), PRIMARY KEY (warehouse_id, product_id));
CREATE TABLE @.order_items (order_id bigint NOT NULL REFERENCES @.orders (id), product_id bigint NOT NULL REFERENCES @.products (id), warehouse_id bigint NOT NULL,
  PRIMARY KEY (order_id, product_id, warehouse_id),
  FOREIGN KEY (warehouse_id, product_id) REFERENCES @.stock (warehouse_id, product_id))`,
			mysqlPfx: "sar_",
			mysqlDDL: `CREATE TABLE @orders (id bigint PRIMARY KEY);
CREATE TABLE @products (id bigint PRIMARY KEY);
CREATE TABLE @warehouses (id bigint PRIMARY KEY);
CREATE TABLE @stock (warehouse_id bigint NOT NULL, product_id bigint NOT NULL, PRIMARY KEY (warehouse_id, product_id),
  CONSTRAINT @stock_wh FOREIGN KEY (warehouse_id) REFERENCES @warehouses (id),
  CONSTRAINT @stock_product FOREIGN KEY (product_id) REFERENCES @products (id));
CREATE TABLE @order_items (order_id bigint NOT NULL, product_id bigint NOT NULL, warehouse_id bigint NOT NULL,
  PRIMARY KEY (order_id, product_id, warehouse_id),
  CONSTRAINT @oi_order FOREIGN KEY (order_id) REFERENCES @orders (id),
  CONSTRAINT @oi_product FOREIGN KEY (product_id) REFERENCES @products (id),
  CONSTRAINT @oi_stock FOREIGN KEY (warehouse_id, product_id) REFERENCES @stock (warehouse_id, product_id))`,
			tables: []string{"orders", "products", "warehouses", "stock", "order_items"},
			wantPG: []string{
				"M2M rdw_sar.warehouses -> rdw_sar.products products via rdw_sar.stock [warehouse_id→id] [product_id→id] stock_warehouse_id_fkey / stock_product_id_fkey",
				"M2M rdw_sar.products -> rdw_sar.warehouses warehouses via rdw_sar.stock [product_id→id] [warehouse_id→id] stock_product_id_fkey / stock_warehouse_id_fkey",
				"M2M rdw_sar.orders -> rdw_sar.stock stock via rdw_sar.order_items [order_id→id] [warehouse_id→warehouse_id product_id→product_id] order_items_order_id_fkey / order_items_warehouse_id_product_id_fkey",
				"M2M rdw_sar.stock -> rdw_sar.orders orders via rdw_sar.order_items [warehouse_id→warehouse_id product_id→product_id] [order_id→id] order_items_warehouse_id_product_id_fkey / order_items_order_id_fkey",
			},
			wantMySQL: []string{
				"M2M sar_warehouses -> sar_products sar_products via sar_stock [warehouse_id→id] [product_id→id] sar_stock_wh / sar_stock_product",
				"M2M sar_products -> sar_warehouses sar_warehouses via sar_stock [product_id→id] [warehouse_id→id] sar_stock_product / sar_stock_wh",
				"M2M sar_orders -> sar_stock sar_stock via sar_order_items [order_id→id] [warehouse_id→warehouse_id product_id→product_id] sar_oi_order / sar_oi_stock",
				"M2M sar_stock -> sar_orders sar_orders via sar_order_items [warehouse_id→warehouse_id product_id→product_id] [order_id→id] sar_oi_stock / sar_oi_order",
			},
			wantLite: []string{
				"M2M warehouses -> products products via stock [warehouse_id→id] [product_id→id] stock(warehouse_id) → warehouses(id) / stock(product_id) → products(id)",
				"M2M products -> warehouses warehouses via stock [product_id→id] [warehouse_id→id] stock(product_id) → products(id) / stock(warehouse_id) → warehouses(id)",
				"M2M orders -> stock stock via order_items [order_id→id] [warehouse_id→warehouse_id product_id→product_id] order_items(order_id) → orders(id) / order_items(warehouse_id, product_id) → stock(warehouse_id, product_id)",
				"M2M stock -> orders orders via order_items [warehouse_id→warehouse_id product_id→product_id] [order_id→id] order_items(warehouse_id, product_id) → stock(warehouse_id, product_id) / order_items(order_id) → orders(id)",
			},
		},
		{
			name:     "self-referential M2M",
			pgSchema: "rdw_srm",
			ddl: `CREATE TABLE @.tasks (id bigint PRIMARY KEY);
CREATE TABLE @.task_dependencies (task_id bigint NOT NULL REFERENCES @.tasks (id), depends_on_task_id bigint NOT NULL REFERENCES @.tasks (id), PRIMARY KEY (task_id, depends_on_task_id))`,
			mysqlPfx: "srm_",
			mysqlDDL: `CREATE TABLE @tasks (id bigint PRIMARY KEY);
CREATE TABLE @task_dependencies (task_id bigint NOT NULL, depends_on_task_id bigint NOT NULL, PRIMARY KEY (task_id, depends_on_task_id),
  CONSTRAINT @td_task FOREIGN KEY (task_id) REFERENCES @tasks (id),
  CONSTRAINT @td_dep FOREIGN KEY (depends_on_task_id) REFERENCES @tasks (id))`,
			tables: []string{"tasks", "task_dependencies"},
			wantPG: []string{
				"M2M rdw_srm.tasks -> rdw_srm.tasks depends_on_task (tasks) via rdw_srm.task_dependencies [task_id→id] [depends_on_task_id→id] task_dependencies_task_id_fkey / task_dependencies_depends_on_task_id_fkey",
				"M2M rdw_srm.tasks -> rdw_srm.tasks task (tasks) via rdw_srm.task_dependencies [depends_on_task_id→id] [task_id→id] task_dependencies_depends_on_task_id_fkey / task_dependencies_task_id_fkey",
			},
			wantMySQL: []string{
				"M2M srm_tasks -> srm_tasks depends_on_task (srm_tasks) via srm_task_dependencies [task_id→id] [depends_on_task_id→id] srm_td_task / srm_td_dep",
				"M2M srm_tasks -> srm_tasks task (srm_tasks) via srm_task_dependencies [depends_on_task_id→id] [task_id→id] srm_td_dep / srm_td_task",
			},
			wantLite: []string{
				"M2M tasks -> tasks depends_on_task (tasks) via task_dependencies [task_id→id] [depends_on_task_id→id] task_dependencies(task_id) → tasks(id) / task_dependencies(depends_on_task_id) → tasks(id)",
				"M2M tasks -> tasks task (tasks) via task_dependencies [depends_on_task_id→id] [task_id→id] task_dependencies(depends_on_task_id) → tasks(id) / task_dependencies(task_id) → tasks(id)",
			},
		},
	}
	for _, sh := range shapes {
		t.Run(sh.name, func(t *testing.T) {
			t.Run("postgres", func(t *testing.T) {
				if sh.pgSkip != "" {
					t.Skip(sh.pgSkip)
				}
				ddl := "CREATE SCHEMA " + sh.pgSchema + ";\n" + strings.ReplaceAll(sh.ddl, "@.", sh.pgSchema+".")
				reset := "DROP SCHEMA IF EXISTS " + sh.pgSchema + " CASCADE"
				if sh.pgReset != "" {
					reset += ";" + sh.pgReset
				}
				applied := ddl
				if sh.pgSetup != "" {
					applied = sh.pgSetup + ";" + ddl
				}
				applyParityFixture(t, "pgx", pgConnStr, reset, applied)
				parsed, introspected := parsePG(t, ddl), introspectPG(t, "", sh.pgSchema)
				rdwAssertShapeParity(t, "postgres", parsed, introspected, sh.overrides, "", sh.wantPG)
			})
			t.Run("mysql", func(t *testing.T) {
				if sh.mysqlSkip != "" {
					t.Skip(sh.mysqlSkip)
				}
				pfx := func(s string) string { return strings.ReplaceAll(s, "@", sh.mysqlPfx) }
				var tables, drops []string
				for _, name := range sh.tables {
					tables = append(tables, sh.mysqlPfx+name)
				}
				for _, name := range slices.Concat(sh.mysqlExt, sh.tables) {
					drops = append([]string{"DROP TABLE IF EXISTS " + sh.mysqlPfx + name}, drops...)
				}
				ddl := pfx(sh.mysqlDDL)
				applied := ddl
				if sh.mysqlSet != "" {
					applied = pfx(sh.mysqlSet) + ";" + ddl
				}
				applyParityFixture(t, "mysql", mysqlConnStr, strings.Join(drops, ";"), applied)
				parsed, introspected := parseMySQL(t, ddl), introspectMySQL(t, tables...)
				rdwAssertShapeParity(t, "mysql", parsed, introspected, sh.overrides, sh.mysqlPfx, sh.wantMySQL)
			})
			t.Run("sqlite", func(t *testing.T) {
				ddl := strings.ReplaceAll(sh.ddl, "@.", "")
				// The SQLite file holds no target outside the schema: SQLite
				// accepts a foreign key into a table that does not exist.
				ddl = strings.ReplaceAll(strings.ReplaceAll(ddl, "rdw_tos_ext.", ""), "rdw_jee_ext.", "")
				dbPath := applySQLite(t, ddl)
				parsed := parseStatements(t, sqlite.New(), ddl)
				introspected := introspectSQLite(t, dbPath, introspect.IntrospectionOptions{})
				rdwAssertShapeParity(t, "sqlite", parsed, introspected, sh.overrides, "", sh.wantLite)
			})
		})
	}
}

// rdwDetect runs what the CLI runs before generation: reference resolution,
// the sort, the primary_key overrides (keyed by bare table name, which
// prefix extends) and detection. It returns the detection warnings.
func rdwDetect(dialect string, s *parser.Schema, overrides map[string][]string, prefix string) []string {
	parser.ResolveReferenceNames(s, dialect)
	s.Sort()
	for i := range s.Tables {
		table := &s.Tables[i]
		cols, ok := overrides[strings.TrimPrefix(table.Name, prefix)]
		if !ok || !strings.HasPrefix(table.Name, prefix) {
			continue
		}
		for j := range table.Columns {
			table.Columns[j].PrimaryKey = slices.Contains(cols, table.Columns[j].Name)
		}
	}
	return parser.DetectRelationships(s)
}

// rdwAssertShapeParity detects relationships on both schemas and checks that
// they are identical and that the parsed edges are exactly want, with no
// warnings.
func rdwAssertShapeParity(t *testing.T, dialect string, parsed, introspected *parser.Schema, overrides map[string][]string, prefix string, want []string) {
	t.Helper()
	parsedWarnings := rdwDetect(dialect, parsed, overrides, prefix)
	introspectedWarnings := rdwDetect(dialect, introspected, overrides, prefix)
	if diff := cmp.Diff(parsed.Relationships, introspected.Relationships); diff != "" {
		t.Errorf("%s: introspected relationships differ from the file parser's (-parsed +introspected):\n%s", dialect, diff)
	}
	if len(parsedWarnings) != 0 || len(introspectedWarnings) != 0 {
		t.Errorf("%s: detection warnings = %q (files), %q (database), want none", dialect, parsedWarnings, introspectedWarnings)
	}
	got := rdwEdgeStrings(parsed.Relationships)
	slices.Sort(got)
	wantSorted := slices.Clone(want)
	slices.Sort(wantSorted)
	if diff := cmp.Diff(wantSorted, got); diff != "" {
		t.Errorf("%s: DetectRelationships() edges mismatch (-want +got):\n%s", dialect, diff)
	}
}

// rdwEdgeStrings spells each edge as relationshipString does, and an M2M
// edge's two constraints as well.
func rdwEdgeStrings(rels []parser.Relationship) []string {
	if len(rels) == 0 {
		return nil
	}
	out := make([]string, 0, len(rels))
	for _, r := range rels {
		s := relationshipString(r)
		if r.Type == parser.ManyToMany {
			s += fmt.Sprintf(" %s / %s", r.JunctionLocalConstraint, r.JunctionReferenceConstraint)
		}
		out = append(out, s)
	}
	return out
}

// Under source: both the database's tables and the files' migration together
// give the same edges as the files alone and as the database after the
// migration runs. The migration adds a tenant junction and an O2O key to a
// table the database already holds.
func TestDetectRelationships_BothSourceParity(t *testing.T) {
	requireDatabases(t)
	tests := []struct {
		name      string
		base      string
		migration string
		tables    []string // MySQL: the tables to introspect, in creation order
		newParser func(*testing.T) introspect.Seedable
		want      []string
	}{
		{
			name: "postgres",
			base: `CREATE SCHEMA rdw_both;
CREATE TABLE rdw_both.workspaces (id bigint PRIMARY KEY);
CREATE TABLE rdw_both.tasks (id bigint PRIMARY KEY, workspace_id bigint NOT NULL REFERENCES rdw_both.workspaces (id), UNIQUE (workspace_id, id));
CREATE TABLE rdw_both.labels (id bigint PRIMARY KEY, workspace_id bigint NOT NULL REFERENCES rdw_both.workspaces (id), UNIQUE (workspace_id, id))`,
			migration: `CREATE TABLE rdw_both.task_labels (workspace_id bigint NOT NULL REFERENCES rdw_both.workspaces (id), task_id bigint NOT NULL, label_id bigint NOT NULL,
  PRIMARY KEY (workspace_id, task_id, label_id),
  FOREIGN KEY (workspace_id, task_id) REFERENCES rdw_both.tasks (workspace_id, id),
  FOREIGN KEY (workspace_id, label_id) REFERENCES rdw_both.labels (workspace_id, id));
ALTER TABLE rdw_both.labels ADD COLUMN owner_task_id bigint REFERENCES rdw_both.tasks (id);
CREATE UNIQUE INDEX labels_owner ON rdw_both.labels (owner_task_id)`,
			newParser: func(*testing.T) introspect.Seedable { return postgres.New("public") },
			want: []string{
				"O2M rdw_both.workspaces -> rdw_both.labels labels [workspace_id→id] labels_workspace_id_fkey",
				"O2O rdw_both.labels -> rdw_both.tasks owner_task (tasks) [owner_task_id→id] labels_owner_task_id_fkey",
				"M2M rdw_both.tasks -> rdw_both.labels labels via rdw_both.task_labels [workspace_id→workspace_id task_id→id] [workspace_id→workspace_id label_id→id] task_labels_workspace_id_task_id_fkey / task_labels_workspace_id_label_id_fkey",
				"M2M rdw_both.labels -> rdw_both.tasks task_labels (tasks) via rdw_both.task_labels [workspace_id→workspace_id label_id→id] [workspace_id→workspace_id task_id→id] task_labels_workspace_id_label_id_fkey / task_labels_workspace_id_task_id_fkey",
				"O2M rdw_both.workspaces -> rdw_both.tasks tasks [workspace_id→id] tasks_workspace_id_fkey",
			},
		},
		{
			name: "mysql",
			base: `CREATE TABLE bo_workspaces (id bigint PRIMARY KEY);
CREATE TABLE bo_tasks (id bigint PRIMARY KEY, workspace_id bigint NOT NULL, UNIQUE (workspace_id, id),
  CONSTRAINT bo_tasks_ws FOREIGN KEY (workspace_id) REFERENCES bo_workspaces (id));
CREATE TABLE bo_labels (id bigint PRIMARY KEY, workspace_id bigint NOT NULL, UNIQUE (workspace_id, id),
  CONSTRAINT bo_labels_ws FOREIGN KEY (workspace_id) REFERENCES bo_workspaces (id))`,
			migration: `CREATE TABLE bo_task_labels (workspace_id bigint NOT NULL, task_id bigint NOT NULL, label_id bigint NOT NULL,
  PRIMARY KEY (workspace_id, task_id, label_id),
  CONSTRAINT bo_tl_ws FOREIGN KEY (workspace_id) REFERENCES bo_workspaces (id),
  CONSTRAINT bo_tl_task FOREIGN KEY (workspace_id, task_id) REFERENCES bo_tasks (workspace_id, id),
  CONSTRAINT bo_tl_label FOREIGN KEY (workspace_id, label_id) REFERENCES bo_labels (workspace_id, id));
ALTER TABLE bo_labels ADD COLUMN owner_task_id bigint;
CREATE UNIQUE INDEX bo_labels_owner ON bo_labels (owner_task_id);
ALTER TABLE bo_labels ADD CONSTRAINT bo_labels_owner_task FOREIGN KEY (owner_task_id) REFERENCES bo_tasks (id)`,
			tables: []string{"bo_workspaces", "bo_tasks", "bo_labels", "bo_task_labels"},
			newParser: func(t *testing.T) introspect.Seedable {
				p, err := mysql.New()
				if err != nil {
					t.Fatalf("mysql.New() error: %v", err)
				}
				return p
			},
			want: []string{
				"O2M bo_workspaces -> bo_labels bo_labels [workspace_id→id] bo_labels_ws",
				"O2O bo_labels -> bo_tasks owner_task (bo_tasks) [owner_task_id→id] bo_labels_owner_task",
				"M2M bo_tasks -> bo_labels bo_labels via bo_task_labels [workspace_id→workspace_id task_id→id] [workspace_id→workspace_id label_id→id] bo_tl_task / bo_tl_label",
				"M2M bo_labels -> bo_tasks bo_task_labels (bo_tasks) via bo_task_labels [workspace_id→workspace_id label_id→id] [workspace_id→workspace_id task_id→id] bo_tl_label / bo_tl_task",
				"O2M bo_workspaces -> bo_tasks bo_tasks [workspace_id→id] bo_tasks_ws",
			},
		},
		{
			name: "sqlite",
			base: `CREATE TABLE workspaces (id INTEGER PRIMARY KEY);
CREATE TABLE tasks (id INTEGER PRIMARY KEY, workspace_id INTEGER NOT NULL REFERENCES workspaces (id), UNIQUE (workspace_id, id));
CREATE TABLE labels (id INTEGER PRIMARY KEY, workspace_id INTEGER NOT NULL REFERENCES workspaces (id), UNIQUE (workspace_id, id))`,
			migration: `CREATE TABLE task_labels (workspace_id INTEGER NOT NULL REFERENCES workspaces (id), task_id INTEGER NOT NULL, label_id INTEGER NOT NULL,
  PRIMARY KEY (workspace_id, task_id, label_id),
  FOREIGN KEY (workspace_id, task_id) REFERENCES tasks (workspace_id, id),
  FOREIGN KEY (workspace_id, label_id) REFERENCES labels (workspace_id, id));
ALTER TABLE labels ADD COLUMN owner_task_id INTEGER REFERENCES tasks (id);
CREATE UNIQUE INDEX labels_owner ON labels (owner_task_id)`,
			newParser: func(*testing.T) introspect.Seedable { return sqlite.New() },
			want: []string{
				"O2M workspaces -> labels labels [workspace_id→id] labels(workspace_id) → workspaces(id)",
				"O2O labels -> tasks owner_task (tasks) [owner_task_id→id] labels(owner_task_id) → tasks(id)",
				"M2M tasks -> labels labels via task_labels [workspace_id→workspace_id task_id→id] [workspace_id→workspace_id label_id→id] task_labels(workspace_id, task_id) → tasks(workspace_id, id) / task_labels(workspace_id, label_id) → labels(workspace_id, id)",
				"M2M labels -> tasks task_labels (tasks) via task_labels [workspace_id→workspace_id label_id→id] [workspace_id→workspace_id task_id→id] task_labels(workspace_id, label_id) → labels(workspace_id, id) / task_labels(workspace_id, task_id) → tasks(workspace_id, id)",
				"O2M workspaces -> tasks tasks [workspace_id→id] tasks(workspace_id) → workspaces(id)",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Each source reads the schema the database holds after the
			// migration, or the files that build it.
			var base, after func() *parser.Schema
			var applyMigration func()
			switch tt.name {
			case "postgres":
				applyParityFixture(t, "pgx", pgConnStr, "DROP SCHEMA IF EXISTS rdw_both CASCADE", tt.base)
				base = func() *parser.Schema { return introspectPG(t, "", "rdw_both") }
				applyMigration = func() { applyParityFixture(t, "pgx", pgConnStr, "", tt.migration) }
				after = base
			case "mysql":
				var drops []string
				for _, name := range tt.tables {
					drops = append([]string{"DROP TABLE IF EXISTS " + name}, drops...)
				}
				applyParityFixture(t, "mysql", mysqlConnStr, strings.Join(drops, ";"), tt.base)
				base = func() *parser.Schema { return introspectMySQL(t, tt.tables[:3]...) }
				applyMigration = func() { applyParityFixture(t, "mysql", mysqlConnStr, "", tt.migration) }
				after = func() *parser.Schema { return introspectMySQL(t, tt.tables...) }
			case "sqlite":
				dbPath := applySQLite(t, tt.base)
				base = func() *parser.Schema { return introspectSQLite(t, dbPath, introspect.IntrospectionOptions{}) }
				applyMigration = func() { applyParityFixture(t, "sqlite", dbPath, "", tt.migration) }
				after = base
			}

			p := tt.newParser(t)
			if err := introspect.Merge(base(), p, []string{writeMigration(t, tt.migration)}); err != nil {
				t.Fatalf("Merge() error: %v", err)
			}
			both := p.Schema()
			files := parseStatements(t, tt.newParser(t), tt.base+";"+tt.migration)
			applyMigration()
			database := after()

			var warnings [3][]string
			for i, s := range []*parser.Schema{both, files, database} {
				warnings[i] = rdwDetect(tt.name, s, nil, "")
			}
			if diff := cmp.Diff(files.Relationships, both.Relationships); diff != "" {
				t.Errorf("both relationships differ from files (-files +both):\n%s", diff)
			}
			if diff := cmp.Diff(database.Relationships, both.Relationships); diff != "" {
				t.Errorf("both relationships differ from database (-database +both):\n%s", diff)
			}
			for i, w := range warnings {
				if len(w) != 0 {
					t.Errorf("source %d: detection warnings = %q, want none", i, w)
				}
			}
			got := rdwEdgeStrings(both.Relationships)
			slices.Sort(got)
			want := slices.Clone(tt.want)
			slices.Sort(want)
			if diff := cmp.Diff(want, got); diff != "" {
				t.Errorf("both: DetectRelationships() edges mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

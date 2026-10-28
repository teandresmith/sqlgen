package parser_test

import (
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/teandresmith/sqlgen/parser"
)

// Relationship detection on each dialect's own file parser. MySQL ignores a
// foreign key written on a column, so every MySQL key is a named table
// constraint; SQLite keys are unnamed and spelled table(columns) →
// target(columns).
func TestDetectRelationships_Dialects(t *testing.T) {
	tests := []struct {
		name         string
		dialect      string
		ddl          string
		pkOverrides  map[string][]string
		want         []string
		wantWarnings []string
	}{
		// --- O2O ---
		{
			name:    "mysql: single-column UNIQUE FK is O2O named after the referenced table",
			dialect: "mysql",
			ddl: `
CREATE TABLE users (id bigint PRIMARY KEY);
CREATE TABLE profiles (id bigint PRIMARY KEY, user_id bigint UNIQUE,
  CONSTRAINT profiles_user FOREIGN KEY (user_id) REFERENCES users (id));`,
			want: []string{"O2O profiles -> users users [user_id→id] profiles_user"},
		},
		{
			name:    "sqlite: single-column UNIQUE FK is O2O named after the referenced table",
			dialect: "sqlite",
			ddl: `
CREATE TABLE users (id INTEGER PRIMARY KEY);
CREATE TABLE profiles (id INTEGER PRIMARY KEY, user_id INTEGER UNIQUE REFERENCES users (id));`,
			want: []string{"O2O profiles -> users users [user_id→id] profiles(user_id) → users(id)"},
		},
		{
			name:    "mysql: FK on the sole primary-key column is O2O",
			dialect: "mysql",
			ddl: `
CREATE TABLE users (id bigint PRIMARY KEY);
CREATE TABLE user_credentials (user_id bigint PRIMARY KEY, secret text,
  CONSTRAINT credentials_user FOREIGN KEY (user_id) REFERENCES users (id));`,
			want: []string{"O2O user_credentials -> users users [user_id→id] credentials_user"},
		},
		{
			name:    "sqlite: FK on the sole primary-key column is O2O",
			dialect: "sqlite",
			ddl: `
CREATE TABLE users (id INTEGER PRIMARY KEY);
CREATE TABLE user_credentials (user_id INTEGER PRIMARY KEY REFERENCES users (id), secret TEXT);`,
			want: []string{"O2O user_credentials -> users users [user_id→id] user_credentials(user_id) → users(id)"},
		},
		{
			name:        "postgres: a declared primary key an override moves no longer makes the edge O2O",
			dialect:     "postgres",
			pkOverrides: map[string][]string{"user_credentials": {"code"}},
			ddl: `
CREATE TABLE users (id bigint PRIMARY KEY);
CREATE TABLE user_credentials (user_id bigint PRIMARY KEY REFERENCES users (id), code text NOT NULL);`,
			want: []string{"O2M public.users -> public.user_credentials user_credentials [user_id→id] user_credentials_user_id_fkey"},
		},
		{
			name:        "mysql: a declared primary key an override moves no longer makes the edge O2O",
			dialect:     "mysql",
			pkOverrides: map[string][]string{"user_credentials": {"code"}},
			ddl: `
CREATE TABLE users (id bigint PRIMARY KEY);
CREATE TABLE user_credentials (user_id bigint PRIMARY KEY, code varchar(20) NOT NULL,
  CONSTRAINT credentials_user FOREIGN KEY (user_id) REFERENCES users (id));`,
			want: []string{"O2M users -> user_credentials user_credentials [user_id→id] credentials_user"},
		},
		{
			name:        "sqlite: a declared primary key an override moves no longer makes the edge O2O",
			dialect:     "sqlite",
			pkOverrides: map[string][]string{"user_credentials": {"code"}},
			ddl: `
CREATE TABLE users (id INTEGER PRIMARY KEY);
CREATE TABLE user_credentials (user_id INTEGER PRIMARY KEY REFERENCES users (id), code TEXT NOT NULL);`,
			want: []string{"O2M users -> user_credentials user_credentials [user_id→id] user_credentials(user_id) → users(id)"},
		},
		{
			name:    "sqlite: partial UNIQUE on the FK column does not make it O2O",
			dialect: "sqlite",
			ddl: `
CREATE TABLE users (id INTEGER PRIMARY KEY);
CREATE TABLE sessions (id INTEGER PRIMARY KEY, user_id INTEGER REFERENCES users (id), active INTEGER NOT NULL);
CREATE UNIQUE INDEX one_active_session ON sessions (user_id) WHERE active = 1;`,
			want: []string{"O2M users -> sessions sessions [user_id→id] sessions(user_id) → users(id)"},
		},
		// --- column order ---
		{
			name:    "mysql: key pairs follow the constraint's column order, not the target key's",
			dialect: "mysql",
			ddl: `
CREATE TABLE cycles (id bigint PRIMARY KEY, workspace_id bigint NOT NULL, UNIQUE (id, workspace_id), KEY (workspace_id, id));
CREATE TABLE tasks (id bigint PRIMARY KEY, workspace_id bigint NOT NULL, cycle_id bigint,
  CONSTRAINT tasks_cycle FOREIGN KEY (workspace_id, cycle_id) REFERENCES cycles (workspace_id, id));`,
			want: []string{"O2M cycles -> tasks tasks [workspace_id→workspace_id cycle_id→id] tasks_cycle"},
		},
		// --- target keys ---
		{
			name:        "mysql: FK into the declared primary key an override moves keeps its edge",
			dialect:     "mysql",
			pkOverrides: map[string][]string{"cycles": {"code"}},
			ddl: `
CREATE TABLE cycles (id bigint PRIMARY KEY, code varchar(20) NOT NULL);
CREATE TABLE tasks (id bigint PRIMARY KEY, cycle_id bigint,
  CONSTRAINT tasks_cycle FOREIGN KEY (cycle_id) REFERENCES cycles (id));`,
			want: []string{"O2M cycles -> tasks tasks [cycle_id→id] tasks_cycle"},
		},
		{
			name:        "sqlite: FK into the declared primary key an override moves keeps its edge",
			dialect:     "sqlite",
			pkOverrides: map[string][]string{"cycles": {"code"}},
			ddl: `
CREATE TABLE cycles (id INTEGER PRIMARY KEY, code TEXT NOT NULL);
CREATE TABLE tasks (id INTEGER PRIMARY KEY, cycle_id INTEGER REFERENCES cycles (id));`,
			want: []string{"O2M cycles -> tasks tasks [cycle_id→id] tasks(cycle_id) → cycles(id)"},
		},
		{
			name:        "postgres: FK into an override-resolved primary key gives an edge",
			dialect:     "postgres",
			pkOverrides: map[string][]string{"cycles": {"code"}},
			ddl: `
CREATE TABLE cycles (id bigint PRIMARY KEY, code text NOT NULL);
CREATE TABLE tasks (id bigint PRIMARY KEY, cycle_code text REFERENCES cycles (code));`,
			want: []string{"O2M public.cycles -> public.tasks tasks [cycle_code→code] tasks_cycle_code_fkey"},
		},
		{
			name:        "mysql: FK into an override-resolved primary key gives an edge",
			dialect:     "mysql",
			pkOverrides: map[string][]string{"cycles": {"code"}},
			ddl: `
CREATE TABLE cycles (id bigint PRIMARY KEY, code varchar(20) NOT NULL, KEY (code));
CREATE TABLE tasks (id bigint PRIMARY KEY, cycle_code varchar(20),
  CONSTRAINT tasks_code FOREIGN KEY (cycle_code) REFERENCES cycles (code));`,
			want: []string{"O2M cycles -> tasks tasks [cycle_code→code] tasks_code"},
		},
		{
			name:        "sqlite: FK into an override-resolved primary key gives an edge",
			dialect:     "sqlite",
			pkOverrides: map[string][]string{"cycles": {"code"}},
			ddl: `
CREATE TABLE cycles (id INTEGER PRIMARY KEY, code TEXT NOT NULL);
CREATE TABLE tasks (id INTEGER PRIMARY KEY, cycle_code TEXT REFERENCES cycles (code));`,
			want: []string{"O2M cycles -> tasks tasks [cycle_code→code] tasks(cycle_code) → cycles(code)"},
		},
		{
			name:    "postgres: REFERENCES with no column list joins the target's primary key",
			dialect: "postgres",
			ddl: `
CREATE TABLE accounts (label text, num bigint PRIMARY KEY);
CREATE TABLE posts (id bigint PRIMARY KEY, account_num bigint REFERENCES accounts);`,
			want: []string{"O2M public.accounts -> public.posts posts [account_num→num] posts_account_num_fkey"},
		},
		{
			name:    "sqlite: REFERENCES with no column list joins the target's primary key",
			dialect: "sqlite",
			ddl: `
CREATE TABLE accounts (label TEXT, num INTEGER PRIMARY KEY);
CREATE TABLE posts (id INTEGER PRIMARY KEY, account_num INTEGER REFERENCES accounts);`,
			want: []string{"O2M accounts -> posts posts [account_num→num] posts(account_num) → accounts(num)"},
		},
		{
			name:    "postgres: FK into non-key columns gives no edge and a warning naming the constraint",
			dialect: "postgres",
			ddl: `
CREATE TABLE users (id bigint PRIMARY KEY, email text);
CREATE TABLE invites (id bigint PRIMARY KEY, email text REFERENCES users (email));`,
			wantWarnings: []string{
				"tables.public.invites: foreign key invites_email_fkey references columns that are neither the primary key nor a non-partial UNIQUE constraint of public.users, so it gives no relationship; declare one by hand if you need it (PRD §5.3, §13.4)",
			},
		},
		{
			name:    "mysql: FK into a table the schema does not hold gives no edge",
			dialect: "mysql",
			ddl: `
CREATE TABLE users (id bigint PRIMARY KEY);
CREATE TABLE posts (id bigint PRIMARY KEY, author_id bigint, account_id bigint,
  CONSTRAINT posts_author FOREIGN KEY (author_id) REFERENCES users (id),
  CONSTRAINT posts_account FOREIGN KEY (account_id) REFERENCES accounts (id));`,
			want: []string{"O2M users -> posts posts [author_id→id] posts_author"},
		},
		{
			name:    "sqlite: FK into a table the schema does not hold gives no edge",
			dialect: "sqlite",
			ddl: `
CREATE TABLE users (id INTEGER PRIMARY KEY);
CREATE TABLE posts (id INTEGER PRIMARY KEY, author_id INTEGER REFERENCES users (id), account_id INTEGER REFERENCES accounts (id));`,
			want: []string{"O2M users -> posts posts [author_id→id] posts(author_id) → users(id)"},
		},
		// --- redundant keys ---
		{
			name:    "mysql: FK whose pairs lie within another FK to the same target gives no edge",
			dialect: "mysql",
			ddl: `
CREATE TABLE cycles (id bigint PRIMARY KEY, workspace_id bigint NOT NULL, UNIQUE (workspace_id, id));
CREATE TABLE tasks (id bigint PRIMARY KEY, workspace_id bigint NOT NULL, cycle_id bigint,
  CONSTRAINT tasks_cycle FOREIGN KEY (cycle_id) REFERENCES cycles (id),
  CONSTRAINT tasks_ws_cycle FOREIGN KEY (workspace_id, cycle_id) REFERENCES cycles (workspace_id, id));`,
			want: []string{"O2M cycles -> tasks tasks [workspace_id→workspace_id cycle_id→id] tasks_ws_cycle"},
		},
		{
			name:    "sqlite: FK whose pairs lie within another FK to the same target gives no edge",
			dialect: "sqlite",
			ddl: `
CREATE TABLE cycles (id INTEGER PRIMARY KEY, workspace_id INTEGER NOT NULL, UNIQUE (workspace_id, id));
CREATE TABLE tasks (id INTEGER PRIMARY KEY, workspace_id INTEGER NOT NULL, cycle_id INTEGER REFERENCES cycles (id),
  FOREIGN KEY (workspace_id, cycle_id) REFERENCES cycles (workspace_id, id));`,
			want: []string{"O2M cycles -> tasks tasks [workspace_id→workspace_id cycle_id→id] tasks(workspace_id, cycle_id) → cycles(workspace_id, id)"},
		},
		{
			name:    "mysql: of identical FKs the name that sorts first gives the edge whatever its column order",
			dialect: "mysql",
			ddl: `
CREATE TABLE t (x int NOT NULL, y int NOT NULL, PRIMARY KEY (x, y));
CREATE TABLE c (id int PRIMARY KEY, a int, b int,
  CONSTRAINT z_decl FOREIGN KEY (a, b) REFERENCES t (x, y),
  CONSTRAINT a_decl FOREIGN KEY (b, a) REFERENCES t (y, x));`,
			want: []string{"O2M t -> c c [b→y a→x] a_decl"},
		},
		// --- junctions ---
		{
			name:    "mysql: two FKs in a composite primary key are one M2M edge",
			dialect: "mysql",
			ddl: `
CREATE TABLE products (id bigint PRIMARY KEY);
CREATE TABLE tags (id bigint PRIMARY KEY);
CREATE TABLE product_tags (product_id bigint NOT NULL, tag_id bigint NOT NULL, PRIMARY KEY (product_id, tag_id),
  CONSTRAINT pt_product FOREIGN KEY (product_id) REFERENCES products (id),
  CONSTRAINT pt_tag FOREIGN KEY (tag_id) REFERENCES tags (id));`,
			want: []string{
				"M2M products -> tags tags via product_tags [product_id→id] [tag_id→id] pt_product / pt_tag",
				"M2M tags -> products products via product_tags [tag_id→id] [product_id→id] pt_tag / pt_product",
			},
		},
		{
			name:    "mysql: two FKs in a UNIQUE beside a surrogate key are one M2M edge",
			dialect: "mysql",
			ddl: `
CREATE TABLE products (id bigint PRIMARY KEY);
CREATE TABLE tags (id bigint PRIMARY KEY);
CREATE TABLE product_tags (id bigint PRIMARY KEY, product_id bigint NOT NULL, tag_id bigint NOT NULL, UNIQUE (product_id, tag_id),
  CONSTRAINT pt_product FOREIGN KEY (product_id) REFERENCES products (id),
  CONSTRAINT pt_tag FOREIGN KEY (tag_id) REFERENCES tags (id));`,
			want: []string{
				"M2M products -> tags tags via product_tags [product_id→id] [tag_id→id] pt_product / pt_tag",
				"M2M tags -> products products via product_tags [tag_id→id] [product_id→id] pt_tag / pt_product",
			},
		},
		{
			name:    "sqlite: two FKs in a composite primary key are one M2M edge",
			dialect: "sqlite",
			ddl: `
CREATE TABLE products (id INTEGER PRIMARY KEY);
CREATE TABLE tags (id INTEGER PRIMARY KEY);
CREATE TABLE product_tags (product_id INTEGER NOT NULL REFERENCES products (id), tag_id INTEGER NOT NULL REFERENCES tags (id), PRIMARY KEY (product_id, tag_id));`,
			want: []string{
				"M2M products -> tags tags via product_tags [product_id→id] [tag_id→id] product_tags(product_id) → products(id) / product_tags(tag_id) → tags(id)",
				"M2M tags -> products products via product_tags [tag_id→id] [product_id→id] product_tags(tag_id) → tags(id) / product_tags(product_id) → products(id)",
			},
		},
		{
			name:    "sqlite: two FKs in a UNIQUE beside a surrogate key are one M2M edge",
			dialect: "sqlite",
			ddl: `
CREATE TABLE products (id INTEGER PRIMARY KEY);
CREATE TABLE tags (id INTEGER PRIMARY KEY);
CREATE TABLE product_tags (id INTEGER PRIMARY KEY, product_id INTEGER NOT NULL REFERENCES products (id), tag_id INTEGER NOT NULL REFERENCES tags (id), UNIQUE (product_id, tag_id));`,
			want: []string{
				"M2M products -> tags tags via product_tags [product_id→id] [tag_id→id] product_tags(product_id) → products(id) / product_tags(tag_id) → tags(id)",
				"M2M tags -> products products via product_tags [tag_id→id] [product_id→id] product_tags(tag_id) → tags(id) / product_tags(product_id) → products(id)",
			},
		},
		{
			name:    "sqlite: partial UNIQUE over two FKs does not make a junction",
			dialect: "sqlite",
			ddl: `
CREATE TABLE products (id INTEGER PRIMARY KEY);
CREATE TABLE tags (id INTEGER PRIMARY KEY);
CREATE TABLE product_tags (id INTEGER PRIMARY KEY, product_id INTEGER REFERENCES products (id), tag_id INTEGER REFERENCES tags (id), active INTEGER NOT NULL);
CREATE UNIQUE INDEX product_tags_active ON product_tags (product_id, tag_id) WHERE active = 1;`,
			want: []string{
				"O2M products -> product_tags product_tags [product_id→id] product_tags(product_id) → products(id)",
				"O2M tags -> product_tags product_tags [tag_id→id] product_tags(tag_id) → tags(id)",
			},
		},
		{
			name:    "mysql: tenant junction sets aside its workspace FK and is M2M",
			dialect: "mysql",
			ddl: `
CREATE TABLE workspaces (id bigint PRIMARY KEY);
CREATE TABLE tasks (id bigint PRIMARY KEY, workspace_id bigint NOT NULL, UNIQUE (workspace_id, id),
  CONSTRAINT tasks_ws FOREIGN KEY (workspace_id) REFERENCES workspaces (id));
CREATE TABLE labels (id bigint PRIMARY KEY, workspace_id bigint NOT NULL, UNIQUE (workspace_id, id),
  CONSTRAINT labels_ws FOREIGN KEY (workspace_id) REFERENCES workspaces (id));
CREATE TABLE task_labels (workspace_id bigint NOT NULL, task_id bigint NOT NULL, label_id bigint NOT NULL,
  PRIMARY KEY (workspace_id, task_id, label_id),
  CONSTRAINT tl_ws FOREIGN KEY (workspace_id) REFERENCES workspaces (id),
  CONSTRAINT tl_task FOREIGN KEY (workspace_id, task_id) REFERENCES tasks (workspace_id, id),
  CONSTRAINT tl_label FOREIGN KEY (workspace_id, label_id) REFERENCES labels (workspace_id, id));`,
			want: []string{
				"O2M workspaces -> tasks tasks [workspace_id→id] tasks_ws",
				"O2M workspaces -> labels labels [workspace_id→id] labels_ws",
				"M2M tasks -> labels labels via task_labels [workspace_id→workspace_id task_id→id] [workspace_id→workspace_id label_id→id] tl_task / tl_label",
				"M2M labels -> tasks tasks via task_labels [workspace_id→workspace_id label_id→id] [workspace_id→workspace_id task_id→id] tl_label / tl_task",
			},
		},
		{
			name:    "sqlite: tenant junction sets aside its workspace FK and is M2M",
			dialect: "sqlite",
			ddl: `
CREATE TABLE workspaces (id INTEGER PRIMARY KEY);
CREATE TABLE tasks (id INTEGER PRIMARY KEY, workspace_id INTEGER NOT NULL REFERENCES workspaces (id), UNIQUE (workspace_id, id));
CREATE TABLE labels (id INTEGER PRIMARY KEY, workspace_id INTEGER NOT NULL REFERENCES workspaces (id), UNIQUE (workspace_id, id));
CREATE TABLE task_labels (workspace_id INTEGER NOT NULL REFERENCES workspaces (id), task_id INTEGER NOT NULL, label_id INTEGER NOT NULL,
  PRIMARY KEY (workspace_id, task_id, label_id),
  FOREIGN KEY (workspace_id, task_id) REFERENCES tasks (workspace_id, id),
  FOREIGN KEY (workspace_id, label_id) REFERENCES labels (workspace_id, id));`,
			want: []string{
				"O2M workspaces -> tasks tasks [workspace_id→id] tasks(workspace_id) → workspaces(id)",
				"O2M workspaces -> labels labels [workspace_id→id] labels(workspace_id) → workspaces(id)",
				"M2M tasks -> labels labels via task_labels [workspace_id→workspace_id task_id→id] [workspace_id→workspace_id label_id→id] task_labels(workspace_id, task_id) → tasks(workspace_id, id) / task_labels(workspace_id, label_id) → labels(workspace_id, id)",
				"M2M labels -> tasks tasks via task_labels [workspace_id→workspace_id label_id→id] [workspace_id→workspace_id task_id→id] task_labels(workspace_id, label_id) → labels(workspace_id, id) / task_labels(workspace_id, task_id) → tasks(workspace_id, id)",
			},
		},
		{
			name:    "mysql: three single-column FKs in the primary key give three O2M edges",
			dialect: "mysql",
			ddl: `
CREATE TABLE workspaces (id bigint PRIMARY KEY);
CREATE TABLE tasks (id bigint PRIMARY KEY);
CREATE TABLE labels (id bigint PRIMARY KEY);
CREATE TABLE task_labels (workspace_id bigint NOT NULL, task_id bigint NOT NULL, label_id bigint NOT NULL,
  PRIMARY KEY (workspace_id, task_id, label_id),
  CONSTRAINT tl_ws FOREIGN KEY (workspace_id) REFERENCES workspaces (id),
  CONSTRAINT tl_task FOREIGN KEY (task_id) REFERENCES tasks (id),
  CONSTRAINT tl_label FOREIGN KEY (label_id) REFERENCES labels (id));`,
			want: []string{
				"O2M workspaces -> task_labels task_labels [workspace_id→id] tl_ws",
				"O2M tasks -> task_labels task_labels [task_id→id] tl_task",
				"O2M labels -> task_labels task_labels [label_id→id] tl_label",
			},
		},
		{
			name:    "sqlite: three single-column FKs in the primary key give three O2M edges",
			dialect: "sqlite",
			ddl: `
CREATE TABLE workspaces (id INTEGER PRIMARY KEY);
CREATE TABLE tasks (id INTEGER PRIMARY KEY);
CREATE TABLE labels (id INTEGER PRIMARY KEY);
CREATE TABLE task_labels (workspace_id INTEGER NOT NULL REFERENCES workspaces (id), task_id INTEGER NOT NULL REFERENCES tasks (id), label_id INTEGER NOT NULL REFERENCES labels (id),
  PRIMARY KEY (workspace_id, task_id, label_id));`,
			want: []string{
				"O2M workspaces -> task_labels task_labels [workspace_id→id] task_labels(workspace_id) → workspaces(id)",
				"O2M tasks -> task_labels task_labels [task_id→id] task_labels(task_id) → tasks(id)",
				"O2M labels -> task_labels task_labels [label_id→id] task_labels(label_id) → labels(id)",
			},
		},
		{
			name:    "mysql: a junction with an end outside the schema gives no edge",
			dialect: "mysql",
			ddl: `
CREATE TABLE teams (id bigint PRIMARY KEY);
CREATE TABLE team_members (team_id bigint NOT NULL, user_id bigint NOT NULL, PRIMARY KEY (team_id, user_id),
  CONSTRAINT tm_team FOREIGN KEY (team_id) REFERENCES teams (id),
  CONSTRAINT tm_user FOREIGN KEY (user_id) REFERENCES users (id));`,
		},
		{
			name:    "mysql: a junction with an end on non-key columns gives no edge",
			dialect: "mysql",
			ddl: `
CREATE TABLE teams (id bigint PRIMARY KEY);
CREATE TABLE tags (id bigint PRIMARY KEY, label varchar(20), KEY (label));
CREATE TABLE team_tags (team_id bigint NOT NULL, tag_label varchar(20) NOT NULL, PRIMARY KEY (team_id, tag_label),
  CONSTRAINT tt_team FOREIGN KEY (team_id) REFERENCES teams (id),
  CONSTRAINT tt_tag FOREIGN KEY (tag_label) REFERENCES tags (label));`,
			wantWarnings: []string{
				"tables.team_tags: foreign key tt_tag references columns that are neither the primary key nor a non-partial UNIQUE constraint of tags, so it gives no relationship; declare one by hand if you need it (PRD §5.3, §13.4)",
			},
		},
		{
			name:        "mysql: a composite primary-key override over two FKs does not make a junction",
			dialect:     "mysql",
			pkOverrides: map[string][]string{"user_tags": {"user_id", "tag_id"}},
			ddl: `
CREATE TABLE users (id bigint PRIMARY KEY);
CREATE TABLE tags (id bigint PRIMARY KEY);
CREATE TABLE user_tags (user_id bigint NOT NULL, tag_id bigint NOT NULL,
  CONSTRAINT ut_user FOREIGN KEY (user_id) REFERENCES users (id),
  CONSTRAINT ut_tag FOREIGN KEY (tag_id) REFERENCES tags (id));`,
			want: []string{
				"O2M users -> user_tags user_tags [user_id→id] ut_user",
				"O2M tags -> user_tags user_tags [tag_id→id] ut_tag",
			},
		},
		{
			name:        "sqlite: a composite primary-key override over two FKs does not make a junction",
			dialect:     "sqlite",
			pkOverrides: map[string][]string{"user_tags": {"user_id", "tag_id"}},
			ddl: `
CREATE TABLE users (id INTEGER PRIMARY KEY);
CREATE TABLE tags (id INTEGER PRIMARY KEY);
CREATE TABLE user_tags (user_id INTEGER NOT NULL REFERENCES users (id), tag_id INTEGER NOT NULL REFERENCES tags (id));`,
			want: []string{
				"O2M users -> user_tags user_tags [user_id→id] user_tags(user_id) → users(id)",
				"O2M tags -> user_tags user_tags [tag_id→id] user_tags(tag_id) → tags(id)",
			},
		},
		{
			name:    "mysql: order_items sets aside its product FK and joins orders and stock",
			dialect: "mysql",
			ddl: `
CREATE TABLE orders (id bigint PRIMARY KEY);
CREATE TABLE products (id bigint PRIMARY KEY);
CREATE TABLE warehouses (id bigint PRIMARY KEY);
CREATE TABLE stock (warehouse_id bigint NOT NULL, product_id bigint NOT NULL, PRIMARY KEY (warehouse_id, product_id),
  CONSTRAINT stock_wh FOREIGN KEY (warehouse_id) REFERENCES warehouses (id),
  CONSTRAINT stock_product FOREIGN KEY (product_id) REFERENCES products (id));
CREATE TABLE order_items (order_id bigint NOT NULL, product_id bigint NOT NULL, warehouse_id bigint NOT NULL,
  PRIMARY KEY (order_id, product_id, warehouse_id),
  CONSTRAINT oi_order FOREIGN KEY (order_id) REFERENCES orders (id),
  CONSTRAINT oi_product FOREIGN KEY (product_id) REFERENCES products (id),
  CONSTRAINT oi_stock FOREIGN KEY (warehouse_id, product_id) REFERENCES stock (warehouse_id, product_id));`,
			want: []string{
				"M2M warehouses -> products products via stock [warehouse_id→id] [product_id→id] stock_wh / stock_product",
				"M2M products -> warehouses warehouses via stock [product_id→id] [warehouse_id→id] stock_product / stock_wh",
				"M2M orders -> stock stock via order_items [order_id→id] [warehouse_id→warehouse_id product_id→product_id] oi_order / oi_stock",
				"M2M stock -> orders orders via order_items [warehouse_id→warehouse_id product_id→product_id] [order_id→id] oi_stock / oi_order",
			},
		},
		{
			name:    "sqlite: order_items sets aside its product FK and joins orders and stock",
			dialect: "sqlite",
			ddl: `
CREATE TABLE orders (id INTEGER PRIMARY KEY);
CREATE TABLE products (id INTEGER PRIMARY KEY);
CREATE TABLE warehouses (id INTEGER PRIMARY KEY);
CREATE TABLE stock (warehouse_id INTEGER NOT NULL REFERENCES warehouses (id), product_id INTEGER NOT NULL REFERENCES products (id), PRIMARY KEY (warehouse_id, product_id));
CREATE TABLE order_items (order_id INTEGER NOT NULL REFERENCES orders (id), product_id INTEGER NOT NULL REFERENCES products (id), warehouse_id INTEGER NOT NULL,
  PRIMARY KEY (order_id, product_id, warehouse_id),
  FOREIGN KEY (warehouse_id, product_id) REFERENCES stock (warehouse_id, product_id));`,
			want: []string{
				"M2M warehouses -> products products via stock [warehouse_id→id] [product_id→id] stock(warehouse_id) → warehouses(id) / stock(product_id) → products(id)",
				"M2M products -> warehouses warehouses via stock [product_id→id] [warehouse_id→id] stock(product_id) → products(id) / stock(warehouse_id) → warehouses(id)",
				"M2M orders -> stock stock via order_items [order_id→id] [warehouse_id→warehouse_id product_id→product_id] order_items(order_id) → orders(id) / order_items(warehouse_id, product_id) → stock(warehouse_id, product_id)",
				"M2M stock -> orders orders via order_items [warehouse_id→warehouse_id product_id→product_id] [order_id→id] order_items(warehouse_id, product_id) → stock(warehouse_id, product_id) / order_items(order_id) → orders(id)",
			},
		},
		{
			name:    "mysql: self-referential M2M names each direction from its reference key",
			dialect: "mysql",
			ddl: `
CREATE TABLE tasks (id bigint PRIMARY KEY);
CREATE TABLE task_dependencies (task_id bigint NOT NULL, depends_on_task_id bigint NOT NULL, PRIMARY KEY (task_id, depends_on_task_id),
  CONSTRAINT td_task FOREIGN KEY (task_id) REFERENCES tasks (id),
  CONSTRAINT td_dep FOREIGN KEY (depends_on_task_id) REFERENCES tasks (id));`,
			want: []string{
				"M2M tasks -> tasks depends_on_task (tasks) via task_dependencies [task_id→id] [depends_on_task_id→id] td_task / td_dep",
				"M2M tasks -> tasks task (tasks) via task_dependencies [depends_on_task_id→id] [task_id→id] td_dep / td_task",
			},
		},
		{
			name:    "sqlite: self-referential M2M names each direction from its reference key",
			dialect: "sqlite",
			ddl: `
CREATE TABLE tasks (id INTEGER PRIMARY KEY);
CREATE TABLE task_dependencies (task_id INTEGER NOT NULL REFERENCES tasks (id), depends_on_task_id INTEGER NOT NULL REFERENCES tasks (id), PRIMARY KEY (task_id, depends_on_task_id));`,
			want: []string{
				"M2M tasks -> tasks depends_on_task (tasks) via task_dependencies [task_id→id] [depends_on_task_id→id] task_dependencies(task_id) → tasks(id) / task_dependencies(depends_on_task_id) → tasks(id)",
				"M2M tasks -> tasks task (tasks) via task_dependencies [depends_on_task_id→id] [task_id→id] task_dependencies(depends_on_task_id) → tasks(id) / task_dependencies(task_id) → tasks(id)",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			schema := parseDDL(t, tt.dialect, tt.ddl, tt.pkOverrides)
			warnings := parser.DetectRelationships(schema)
			if diff := cmp.Diff(tt.want, edgeStrings(schema.Relationships)); diff != "" {
				t.Errorf("DetectRelationships() edges mismatch (-want +got):\n%s", diff)
			}
			if diff := cmp.Diff(tt.wantWarnings, warnings); diff != "" {
				t.Errorf("DetectRelationships() warnings mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// A column's own FK reference comes from a single-column foreign key only: a
// column that is a member of a composite foreign key alone has none.
func TestFKReference_SingleColumnOnly(t *testing.T) {
	tests := []struct {
		name    string
		dialect string
		ddl     string
		want    map[string]string // column of tasks → its FK reference, "" for none
	}{
		{
			name:    "postgres",
			dialect: "postgres",
			ddl: `
CREATE TABLE workspaces (id bigint PRIMARY KEY);
CREATE TABLE cycles (id bigint PRIMARY KEY, workspace_id bigint NOT NULL, UNIQUE (workspace_id, id));
CREATE TABLE tasks (id bigint PRIMARY KEY, workspace_id bigint NOT NULL REFERENCES workspaces (id), cycle_id bigint,
  FOREIGN KEY (workspace_id, cycle_id) REFERENCES cycles (workspace_id, id));`,
			want: map[string]string{"id": "", "workspace_id": "public.workspaces.id", "cycle_id": ""},
		},
		{
			name:    "mysql",
			dialect: "mysql",
			ddl: `
CREATE TABLE workspaces (id bigint PRIMARY KEY);
CREATE TABLE cycles (id bigint PRIMARY KEY, workspace_id bigint NOT NULL, UNIQUE (workspace_id, id));
CREATE TABLE tasks (id bigint PRIMARY KEY, workspace_id bigint NOT NULL, cycle_id bigint,
  CONSTRAINT tasks_ws FOREIGN KEY (workspace_id) REFERENCES workspaces (id),
  CONSTRAINT tasks_cycle FOREIGN KEY (workspace_id, cycle_id) REFERENCES cycles (workspace_id, id));`,
			want: map[string]string{"id": "", "workspace_id": ".workspaces.id", "cycle_id": ""},
		},
		{
			name:    "sqlite",
			dialect: "sqlite",
			ddl: `
CREATE TABLE workspaces (id INTEGER PRIMARY KEY);
CREATE TABLE cycles (id INTEGER PRIMARY KEY, workspace_id INTEGER NOT NULL, UNIQUE (workspace_id, id));
CREATE TABLE tasks (id INTEGER PRIMARY KEY, workspace_id INTEGER NOT NULL REFERENCES workspaces (id), cycle_id INTEGER,
  FOREIGN KEY (workspace_id, cycle_id) REFERENCES cycles (workspace_id, id));`,
			want: map[string]string{"id": "", "workspace_id": ".workspaces.id", "cycle_id": ""},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			schema := parseDDL(t, tt.dialect, tt.ddl, nil)
			got := map[string]string{}
			for _, table := range schema.Tables {
				if table.Name != "tasks" {
					continue
				}
				for _, col := range table.Columns {
					got[col.Name] = rdwFKReference(col.FKReference)
				}
			}
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("tasks column FK references mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// rdwFKReference spells a reference as "schema.table.column", or "" for none.
func rdwFKReference(ref *parser.FKReference) string {
	if ref == nil {
		return ""
	}
	return ref.Schema + "." + ref.Table + "." + ref.Column
}

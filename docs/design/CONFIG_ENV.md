# SQLGen Config — Environment Variable References

> **Status: DRAFT, all questions resolved (2026-10-06).** Adds `${VAR}`
> references to `sqlgen.yml` values. The behavior in §3–§5 was verified against
> `gopkg.in/yaml.v3 v3.0.1` with a 32-case prototype (§7). §9 records the five
> rulings. This doc is the input to sub-item 30.7a, which syncs the PRD and
> implements it.
>
> **Pattern:** parallels `docs/design/INTROSPECTION.md`, which fronted its open
> questions before the phase breakdown.
>
> **Phase:** 30, sub-item 30.7a, ahead of 30.8. 30.8 opens the first database connection, and
> that connection is where secrets come in. File references are to HEAD
> `04b2af92`.

---

## 1. Motivation

PRD §4.3 lets a connection come from `url` or from the individual fields, and it
lets `SQLGEN_DB_URL` and `SQLGEN_DB_PASSWORD` override them. The two variables
don't combine well:

```yaml
input:
  dialect: postgres
  source: database
  connection:
    url: postgres://app@db.internal:5432/appdb?sslmode=require
```

```sh
SQLGEN_DB_PASSWORD=s3cret sqlgen generate
```

`applyEnvOverrides` (`config/config.go:1994`) stores the password. Then
`connString` (`cli/connection.go:21`) returns `url` unchanged because it is set.
The password never reaches the driver, and the only symptom is a login failure.
The same config written with `host`/`user`/`database` works.

The two variables are also the only way to keep anything out of the committed
file, and they cover only the connection. A user who wants a different database
name, host or output setting per environment has to keep a separate
`sqlgen.yml` for each.

A `${VAR}` reference fixes both problems. The user puts the secret exactly where
it belongs (`url: postgres://app:${DB_PASSWORD}@db.internal/appdb`), and the
same syntax works in every field.

## 2. Non-goals

- **Defaults or fallbacks** (`${VAR:-default}`, `${VAR:?msg}`). A reference
  either resolves or fails. A default would hide a missing variable, which is
  exactly the configuration problem this feature should surface.
- **Escaping filters** (`${VAR|urlencode}`). §5.3 covers secrets that need
  escaping inside a URL.
- **Expanding keys.** Table, column and override keys name schema objects.
  Making them depend on the environment would make the set of configured tables
  vary from machine to machine.
- **Other files.** Only `sqlgen.yml` is expanded. The gqlgen config sqlgen
  reads and merges (`wrapper/merge.go`, `gen/gqlgen_model_layout.go`) is
  untouched.
- **Replacing `SQLGEN_DB_URL` / `SQLGEN_DB_PASSWORD`.** Both stay as they are
  (§5.4).

## 3. Syntax

| Text in a value | Result |
|---|---|
| `${NAME}` | The value of environment variable `NAME`. |
| `$${` | A literal `${`. This is the only escape. |
| Any other `$` | Kept as written: `$1`, `$HOME`, `a$` and `$$x` are unchanged. |

- `NAME` is `[A-Za-z_][A-Za-z0-9_]*`. Anything else between `${` and `}` is an
  invalid reference, which is an error. That includes `${}`, `${1X}`,
  `${A-B}` and `${X:-y}`.
- A `${` with no closing `}` is an error.
- A value can hold any number of references, mixed with literal text:
  `postgres://${DB_USER}:${DB_PASSWORD}@${DB_HOST}/app`.
- Only bare `$NAME` is left alone, unlike in a shell. The PRD's SQL examples
  use `$1`, and passwords contain `$`. Leaving every non-`${` dollar as written
  means no existing config changes meaning.

## 4. Semantics

### 4.1 Missing and empty variables

- **Unset:** a config error, exit 2 (§23.9). Every unset variable in the file
  is reported, not just the first, consistent with `validate` reporting all
  errors together (§23).
- **Set to the empty string:** substitutes the empty string. Go's
  `os.LookupEnv` tells unset from empty, and an empty value is something the
  user deliberately set. Required fields still catch it:
  `host: ${DB_HOST}` with `DB_HOST=` fails the existing
  `input.connection.host is required` check. An empty value into a number or
  boolean field is a type error, not zero (§4.3). *(Ruled: §9 Q1.)*

### 4.2 Where and when expansion happens

- Expansion applies to every scalar value in the file, in every section, at any
  depth: mapping values and sequence items. Keys are never expanded.
- It runs once, inside `decodeConfig`, after YAML parsing and before decoding
  into `RootConfig`. `applyDefaults` and `applyEnvOverrides` then run as they
  do today.
- It runs for every command that loads the config: `generate`, `validate`,
  `graphql` and the pipeline. So `sqlgen validate` fails when a referenced
  variable is unset. *(Ruled: §9 Q2.)*
- **Single pass.** Text that came from a variable is never expanded again.
  `HOST='${OTHER}'` yields the literal `${OTHER}`.
- **Structure is fixed before expansion.** References are replaced in the
  parsed tree, not in the raw text, so a value can never add keys, items or
  documents. A secret like `p@ss: #x\n- y` stays one string (verified, §7).
- **Anchors and aliases:** an anchored scalar is expanded where it is defined,
  and every alias of it sees the result.

### 4.3 Types

A reference in a **plain (unquoted) scalar** is typed by its expanded text, the
way YAML types any plain scalar. So `port: ${DB_PORT}` with `DB_PORT=6543`
decodes as the int 6543, and `enabled: ${FLAG}` with `FLAG=true` decodes as a
bool.

There is one exception: an expansion is never typed as null. YAML reads
`null`, `~` and the empty string as null, and decoding null into a string field
silently gives `""` (and gives `nil` for a pointer). A password that is
literally `null` would vanish. Expanded text that would resolve to null is kept
as a string instead (verified, §7).

A reference in a **quoted or block scalar** is always a string, as quoting
means in YAML. `port: "${DB_PORT}"` is therefore a type error. Leave references
in number and boolean fields unquoted.

**Inside a flow collection (`[...]`, `{...}`) a reference must be quoted.**
There `{` and `}` are YAML syntax, so `paths: [${DIR}, b]` is a YAML parse
error before sqlgen ever sees it (verified, §7). Write `paths: ["${DIR}", b]`
or use block style.

### 4.4 Errors

Each error starts with the YAML line, matching yaml.v3's own errors, then gives
the dotted path that §4.13 uses (sequence items as `[i]`):

```
line 3: input.connection.password: environment variable DB_PASSWORD is not set
line 4: input.connection.url: environment variable DB_HOST is not set
line 6: input.paths[1]: invalid reference "${1X}": a name is letters, digits and '_', not starting with a digit
line 7: input.connection.host: unterminated reference "${DB_HOST"
```

Missing and invalid references are reported before anything is decoded, so a
typo in a variable name is never hidden behind the type errors it would cause.

## 5. Security

### 5.1 Values never appear in errors

yaml.v3 type errors quote the offending value. It truncates values over 10
characters to 7, which still leaks a short secret or the start of a long one.
A type error on a line that holds a reference is rewritten to name the
reference instead:

```
line 3: input.connection.port: cannot unmarshal the expansion of "${DB_PORT}" into int
```

The prototype asserts that no error message contains a secret's text (§7).

### 5.2 Generated output

None of `input.connection` reaches generated code or the manifest: there are no
reads of `Connection` or `Password` in `gen/` or `manifest/`. An env-sourced
value in another field, such as a package name, does reach generated code. That
is the point of using one, but output then depends on the environment that ran
`generate`. The docs will say to keep env references to connection and
deployment settings when generated files are committed and drift-checked.

### 5.3 Secrets inside a URL

Substitution is literal. A password containing `/`, `?`, `#` or `%` breaks a
postgres URL (verified with `net/url`, which pgx uses: `a/b` gives
`invalid port ":a" after host`, `a%b` gives `invalid URL escape "%b"`). There
are two fixes, and the docs will give both:

- Prefer the individual fields: `password: ${DB_PASSWORD}`. `connString`
  escapes the password for you, as 30.7 tested.
- Or store the secret percent-encoded and reference it in `url`.

The MySQL DSN takes the password raw, so it doesn't have this problem (30.7
probe).

### 5.4 `SQLGEN_DB_URL` and `SQLGEN_DB_PASSWORD`

Both are unchanged and still applied after expansion. With references, the §1
config becomes `url: postgres://app:${SQLGEN_DB_PASSWORD}@db.internal/appdb`,
which works. The silent drop still happens for anyone who sets the variable but
doesn't reference it, so sqlgen warns in that case. *(Ruled: §9 Q4.)*

## 6. Implementation

### 6.1 Decoding

`decodeConfig` currently does one strict decode (`KnownFields(true)`) of the
raw bytes. The probe found three constraints:

1. **A strict decode of the raw text can't type references.**
   `port: ${PORT}` fails with ``cannot unmarshal !!str `${PORT}` into int``.
   Expansion has to come before the typed decode.
2. **`yaml.Node.Decode` is never strict.** It builds a fresh decoder that
   `KnownFields` can't reach (the gap `config.go:1634` already notes). Decoding
   the expanded tree accepted an unknown `typo:` key silently.
3. **Re-encoding the expanded tree loses line numbers.** `yaml.Marshal` drops
   blank lines and comment spacing. An unknown key on line 10 was reported on
   line 7.

The design that satisfies all three:

1. Parse to a `yaml.Node`. Walk it, expanding scalar values in place and
   recording each reference's line, path and original text. If any reference
   is unset or invalid, return those errors and stop.
2. **No references:** run today's single strict decode of the raw bytes. Every
   existing config takes exactly the path it takes today.
3. **References present:** run two decodes and merge their errors by line.
   - Run the strict decode of the **raw** bytes into a throwaway `RootConfig`,
     keeping only its `field X not found in type Y` errors. Keys are never
     expanded, so these are correct and at their real lines.
   - Run `Node.Decode` of the **expanded** tree into the real `RootConfig`,
     keeping all of its errors (type errors, duplicate keys). The tree keeps
     its parse-time line numbers, so these are correct too. Errors on reference
     lines are redacted (§5.1).
4. In both custom unmarshalers (`Operations`, `NullableVariant`), return a
   `*yaml.TypeError` from the inner `Decode` as it is instead of wrapping it.
   yaml.v3 merges a returned `*TypeError` but aborts the whole decode on any
   other error. With the wrap, a reference in the mapping form
   (`operations: {create: ${FLAG}}`) failed the raw pass, which then reported
   no unknown keys anywhere in the file (reproduced, §7). This also improves
   today's behavior without references: a type error inside those mappings is
   reported with the rest instead of stopping the decode.

The `paths` presence probe (`config.go:1649`) reads the raw bytes and is
unaffected, since presence doesn't depend on the value.

### 6.2 Where the code goes

All of it is in `cmd/sqlgen/config`: an `expand.go` for the reference parser
and the node walk, a revised `decodeConfig`, and the two unmarshaler edits. The
module gets no new dependency and the runtime is untouched.

## 7. Verified behavior

Prototype: `envref` package (scratch module, yaml.v3 v3.0.1), 32 table cases,
all passing. Each row below is a case.

| Area | Case | Result |
|---|---|---|
| Baseline | No references | Unchanged strict path |
| Strings | `host: ${HOST}`, `url: postgres://app:${PW}@${HOST}/d` | Expanded |
| Structure | Value `p@ss: #x\n- y` | One string, no injected keys |
| Types | `port: ${PORT}` (6543), `enabled: ${T}` (true) | int, bool |
| Types | `password: ${NULLPW}` (`null`) | The string `"null"`, not `""` |
| Types | `ssl_mode: ${EMPTY}` into `*string` | Explicit `""`, not nil |
| Types | `port: "${PORT}"` (quoted) | Type error |
| Types | `port: ${EMPTY}` | Type error, not 0 |
| Single pass | `NESTED='${HOST}'` | Literal `${HOST}` |
| Escapes | `a$${HOST}b$HOST$1$` | `a${HOST}b$HOST$1$` |
| Sequences | Block item; quoted flow item | Expanded |
| Sequences | Unquoted `[${HOST}, b]` | YAML parse error |
| Keys | `tables: {${HOST}: …}` | Key kept literal |
| Unmarshalers | `operations: ${OPS}`; mapping `create: ${T}` | Expanded |
| Anchors | `host: &h ${HOST}`, `url: *h` | Both expanded |
| Errors | One unset; three unset across two lines | All reported with line and path |
| Errors | `${HOST:-x}`, `${}`, `${1X}`, `${HOST` | Invalid or unterminated |
| Errors | `input.paths[1]` | Indexed path |
| Redaction | `port: ${BAD_PORT}` | Names `${BAD_PORT}`; no value in any message |
| Strictness | Unknown key after blank lines and comments | Real line (8) |
| Strictness | Unknown key plus a reference type error | Both, in line order |
| Strictness | Unknown key next to a typed reference in an unmarshaler mapping | Reported, but only with the §6.1 step 4 edit |
| Strictness | Duplicate key with a reference | Still reported |
| Strictness | Non-reference type error with references elsewhere | Original message |

URL escaping (§5.3) was checked separately with `net/url.Parse`.

## 8. PRD and doc changes

- **New §4.1.1 "Environment variable references"** covering §3, §4 and §5.1
  in spec form.
- **§4.3:** a note that `url` and `password` usually take a reference, plus the
  URL-escaping guidance from §5.3.
- **§4.13:** error rows (exit 2) for an unset variable, an invalid reference
  and an unterminated reference, and a warning row for an ignored
  `SQLGEN_DB_PASSWORD` (§9 Q4).
- **§4.14:** a note that the schema types `port` and the boolean fields
  strictly, so an editor flags a `${…}` reference in them even though sqlgen
  accepts it (§9 Q3).
- **`docs/configuration.md`:** a user-facing section with the §1 example.
- **Schema descriptions** for `url` and `password` in
  `config/schema/v1.json`.

## 9. Rulings (2026-10-06)

The user accepted the recommendation on all five.

**Q1. A variable set to the empty string is substituted.** Unset fails;
set-but-empty is a deliberate value. Required-field checks still catch an empty
`host`, `user` or `database`, and an empty value in a number or boolean field is
a type error. The rejected alternative, treating empty as missing, can't express
an intentionally empty value such as a blank `ssl_mode`.

**Q2. `sqlgen validate` fails with exit 2 on an unset variable, the same as
`generate`.** The config can't be loaded, and a `validate` that passes ahead of
a `generate` that fails would be worse. CI jobs that only lint the config need
the variables set; dummy values work.

**Q3. The JSON schema doesn't change.** The schema has 13 integer and 46
boolean fields, so an editor flags `port: ${DB_PORT}` as a type error. §4.14
documents that. References go mostly in string fields, and allowing a `${…}`
string on all 59 fields is churn until someone hits it.

**Q4. sqlgen warns when `SQLGEN_DB_PASSWORD` is ignored.** The warning fires
when all of these hold:
- `input.source` reads the database (`database` or `both`);
- `SQLGEN_DB_PASSWORD` is set and non-empty, the same test
  `applyEnvOverrides` uses;
- the connection string comes from a `url`, either `SQLGEN_DB_URL` or
  `input.connection.url`;
- that `url` doesn't reference `${SQLGEN_DB_PASSWORD}`. A `SQLGEN_DB_URL` never
  does, because environment values aren't expanded.

The message names the variable, says the `url` wins, and suggests writing
`${SQLGEN_DB_PASSWORD}` into the `url`. It is a warning, not an error: the `url`
may carry its own password, and §4.3 says `url` takes precedence. Telling
whether the config's `url` referenced the variable needs the expansion walk to
record which variables each path used, kept unexported on the config the way
`pathsSet` is.

**Q5. New sub-item 30.7a**, which syncs the PRD and implements this design,
runs before 30.8. 30.8 now depends on it, so the connection wiring has
references from the start.

## 10. Rejected alternatives

- **Expand the raw text before parsing** (`os.ExpandEnv` on the file bytes):
  the simplest option, but a value containing `: `, `#` or a newline would
  rewrite the YAML structure. That's an injection path, and §4.2 rules it out.
- **Re-encode the expanded tree, then strict-decode it:** this keeps
  strictness but reports unknown keys at the wrong lines (§6.1, constraint 3).
- **A reflection-based unknown-key walker in place of `KnownFields`:** this
  would also close the custom-unmarshaler gap, but it reimplements yaml.v3's
  field matching. The two-pass design reuses yaml.v3's own check.
- **Fill `SQLGEN_DB_PASSWORD` into a `url` that has no password:** this solves
  only §1, and only for postgres URLs, through an implicit rule. References are
  explicit and work in every field.
- **`os.Expand` semantics (`$VAR` as well as `${VAR}`):** this would change
  existing values that contain `$`.

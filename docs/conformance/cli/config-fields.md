# CLI Requirements: Config Fields

<!-- seeding: in-progress -->
<!-- matrix: config-fields rows=field cols=default,explicit,schema -->
<!-- matrix: config-resolution rows=field cols=table-wins,table-inherits,view-wins,view-inherits -->

The §4 field tables: each field's default, the value it keeps when set, and its JSON Schema entry
(`config-fields`), and how a field set at several levels resolves (`config-resolution`). The
§4.13 validation rules and the package's unclaimed tests stay in [`config.md`](config.md).

## §4 Configuration System
<!-- prd: 4 sha256: fcc4d32f8c7e728f -->

| ID | Requirement | Tests | Applies to | Matrix cell |
|---|---|---|---|---|
| config.load.file-names | The configuration is loaded from `sqlgen.yml` or `sqlgen.yaml` in the working directory | `unit: cmd/sqlgen/cli: TestConfigDiscovery_findsYml`; `unit: cmd/sqlgen/cli: TestConfigDiscovery_findsYaml` | all |  |
| config.minimal.loads | The minimal config (`input.dialect`, `input.paths`, `output.dir`) is a complete config: it loads with every other field defaulted | `unit: cmd/sqlgen/config: TestLoadConfig_MinimalWithDefaults` | all |  |

## §4.1 Root Config
<!-- prd: 4.1 sha256: e7d9a83d9bc4e4df -->

| ID | Requirement | Tests | Applies to | Matrix cell |
|---|---|---|---|---|
| config.version.default | `version` defaults to `"v1"` | `unit: cmd/sqlgen/config: TestLoadConfig_MinimalWithDefaults` | all | config-fields: version × default |
| config.version.schema | The JSON Schema declares root `version` as a string | `unit: cmd/sqlgen/config: TestSchemaV1_noDriftFromConfigStructs` | all | config-fields: version × schema |
| config.tables.default | `tables` defaults to an empty map | `unit: cmd/sqlgen/config: TestLoadConfig_MinimalWithDefaults` | all | config-fields: tables × default |
| config.tables.explicit | A `tables.<name>` entry is kept under its key | `unit: cmd/sqlgen/config: TestLoadConfig_FullConfig` | all | config-fields: tables × explicit |
| config.tables.schema | The JSON Schema declares `tables` as an object whose values are TableConfig objects | `unit: cmd/sqlgen/config: TestSchemaV1_noDriftFromConfigStructs`; `unit: cmd/sqlgen/config: TestSchemaV1_acceptsExampleConfigs` | all | config-fields: tables × schema |
| config.views.default | `views` defaults to an empty map | `unit: cmd/sqlgen/config: TestLoadConfig_MinimalWithDefaults` | all | config-fields: views × default |
| config.views.explicit | A `views.<name>` entry is kept under its key | `unit: cmd/sqlgen/config: TestLoadConfig_FullConfig` | all | config-fields: views × explicit |
| config.views.schema | The JSON Schema declares `views` as an object whose values are ViewConfig objects | `unit: cmd/sqlgen/config: TestSchemaV1_noDriftFromConfigStructs`; `unit: cmd/sqlgen/config: TestSchemaV1_acceptsExampleConfigs` | all | config-fields: views × schema |
| config.enums.default | `enums` defaults to an empty map | `unit: cmd/sqlgen/config: TestLoadConfig_EnumsBlockAbsent` | all | config-fields: enums × default |
| config.enums.explicit | An `enums.<name>` entry is kept under its key, bare or schema-qualified | `unit: cmd/sqlgen/config: TestLoadConfig_EnumsBlock` | all | config-fields: enums × explicit |
| config.enums.schema | The JSON Schema declares `enums` as an object whose values are EnumConfig objects | `unit: cmd/sqlgen/config: TestSchemaV1_noDriftFromConfigStructs` | all | config-fields: enums × schema |
| config.extras.default | `extras` defaults to an empty map | `unit: cmd/sqlgen/config: TestLoadConfig_MinimalWithDefaults` | all | config-fields: extras × default |
| config.extras.explicit | An `extras.<name>` entry is kept under its key | `unit: cmd/sqlgen/config: TestLoadConfig_FullConfig` | all | config-fields: extras × explicit |
| config.extras.schema | The JSON Schema declares `extras` as an object whose values are ExtraType objects | `unit: cmd/sqlgen/config: TestSchemaV1_noDriftFromConfigStructs`; `unit: cmd/sqlgen/config: TestSchemaV1_acceptsUnexercisedSurface/extras with tags` | all | config-fields: extras × schema |
| config.api.default-disabled | With no `api` block, API generation is disabled | `unit: cmd/sqlgen/config: TestAPIDefaults/api.enabled defaults to false` | all |  |
| config.cache.default-disabled | With no `cache` block (and no per-entity opt-in), caching is disabled | `unit: cmd/sqlgen/config: TestLoadConfig_CacheDefaultsForPerEntityOptIn/no block and no opt-in`; `unit: cmd/sqlgen/config: TestLoadConfig_CacheDefaultsForPerEntityOptIn/no block and a table opt-out` | all |  |
| config.events.default-disabled | With no `events` block, event publishing is disabled | `unit: cmd/sqlgen/config: TestLoadConfig_EventsDefaultNil` | all |  |
| config.env.scalar-values | Any scalar value can reference an environment variable: strings, integers, booleans and sequence items alike | `unit: cmd/sqlgen/config: TestLoadConfig_EnvReferences/whole value`; `unit: cmd/sqlgen/config: TestLoadConfig_EnvReferences/plain scalar typed as int`; `unit: cmd/sqlgen/config: TestLoadConfig_EnvReferences/plain scalar typed as bool`; `unit: cmd/sqlgen/config: TestLoadConfig_EnvReferences/block sequence item`; `unit: cmd/sqlgen/config: TestLoadConfig_EnvReferences/operations preset` | all |  |
| config.env.keys-not-expanded | Keys are never expanded: a `${NAME}` in a key stays literal | `unit: cmd/sqlgen/config: TestLoadConfig_EnvReferences/keys are not expanded` | all |  |
| config.env.reference-value | `${NAME}` is replaced by the value of environment variable `NAME` | `unit: cmd/sqlgen/config: TestLoadConfig_EnvReferences/whole value` | all |  |
| config.env.escape | `$${` is a literal `${` | `unit: cmd/sqlgen/config: TestLoadConfig_EnvReferences/escape` | all |  |
| config.env.other-dollar-kept | Any other `$` is kept as written: `$1`, `$HOME`, `a$` and `$$x` are unchanged | `unit: cmd/sqlgen/config: TestLoadConfig_EnvReferences/dollars outside a reference are kept`; `unit: cmd/sqlgen/config: TestLoadConfig_EnvReferences/escape` | all |  |
| config.env.mixed-text | A value can hold any number of references mixed with literal text | `unit: cmd/sqlgen/config: TestLoadConfig_EnvReferences/several references in one value` | all |  |
| config.env.empty-value | A variable set to the empty string substitutes the empty string | `unit: cmd/sqlgen/config: TestLoadConfig_EnvReferences/empty value into a pointer is an explicit empty string` | all |  |
| config.env.empty-value-required-rules | Required-field rules still apply to a value that expanded to the empty string |  | all |  |
| config.env.every-command | Expansion runs once, while the config loads, for every command that loads it, `validate` included | `unit: cmd/sqlgen/cli: TestValidate_UnsetEnvReference` | all |  |
| config.env.single-pass | Expansion is a single pass: text that came from a variable is never expanded again | `unit: cmd/sqlgen/config: TestLoadConfig_EnvReferences/single pass` | all |  |
| config.env.no-structure | References are replaced in the parsed YAML, so a value containing `: `, `#` or a newline stays one string and cannot add keys, items or documents | `unit: cmd/sqlgen/config: TestLoadConfig_EnvReferences/a value cannot add structure` | all |  |
| config.env.anchor-alias | An anchored scalar is expanded where it is defined, and every alias of it sees the result | `unit: cmd/sqlgen/config: TestLoadConfig_EnvReferences/anchor and alias` | all |  |
| config.env.plain-typed | A reference in a plain (unquoted) scalar is typed by its expanded text, as YAML types a plain scalar (`port: ${DB_PORT}` with `6543` is the integer 6543) | `unit: cmd/sqlgen/config: TestLoadConfig_EnvReferences/plain scalar typed as int`; `unit: cmd/sqlgen/config: TestLoadConfig_EnvReferences/plain scalar typed as bool` | all |  |
| config.env.never-null | An expansion is never typed as null: `null`, `~` or the empty string stays a string | `unit: cmd/sqlgen/config: TestLoadConfig_EnvReferences/null text stays a string`; `unit: cmd/sqlgen/config: TestLoadConfig_EnvReferences/empty value into a pointer is an explicit empty string` | all |  |
| config.env.quoted-is-string | A reference in a quoted or block scalar is always a string, so `port: "${DB_PORT}"` is a type error | `unit: cmd/sqlgen/config: TestLoadConfig_EnvReferenceErrors/quoted reference is a string` | all |  |
| config.env.flow-quoted | Inside a flow collection a reference must be quoted: a quoted one expands, an unquoted one is a YAML error | `unit: cmd/sqlgen/config: TestLoadConfig_EnvReferences/quoted flow sequence item`; `unit: cmd/sqlgen/config: TestLoadConfig_EnvReferenceErrors/unquoted reference in a flow sequence` | all |  |
| config.env.str-tag-allowed | A reference in a value tagged `!!str` expands as a string; only other tags are an error | `unit: cmd/sqlgen/config: TestLoadConfig_EnvReferences/string tag` | all |  |
| config.env.error-path | Reference errors start with the YAML line, then give the dotted path, sequence items as `[i]` | `unit: cmd/sqlgen/config: TestLoadConfig_EnvReferenceErrors/one unset variable`; `unit: cmd/sqlgen/config: TestLoadConfig_EnvReferenceErrors/sequence item path` | all |  |
| config.env.redaction | No error from loading the config contains an expanded value | `unit: cmd/sqlgen/config: TestLoadConfig_EnvReferenceErrors/short value in a type error is redacted`; `unit: cmd/sqlgen/config: TestLoadConfig_EnvReferenceErrors/long value in a type error is redacted`; `unit: cmd/sqlgen/config: TestLoadConfig_EnvReferenceErrors/empty value into an int` | all |  |
| config.env.type-error-names-reference | A type error on a line that holds a reference names the reference instead of the value: `cannot unmarshal the expansion of "${DB_PORT}" into int` | `unit: cmd/sqlgen/config: TestLoadConfig_EnvReferenceErrors/short value in a type error is redacted`; `unit: cmd/sqlgen/config: TestLoadConfig_EnvReferenceErrors/long value in a type error is redacted` | all |  |
| config.env.connection-not-in-output | `input.connection` never reaches generated code or the manifest |  | all |  |
| config.env.gqlgen-config-not-expanded | Only `sqlgen.yml` is expanded; the gqlgen config sqlgen reads and merges is not |  | all |  |

| Matrix cell | Reason | Accepted |
|---|---|---|
| config-fields: version × explicit | §4.1 and §4.14 (Versioning): `v1` is the only config version, so there is no non-default value to keep | |

## §4.2 InputConfig
<!-- prd: 4.2 sha256: 23bbef86df66be01 -->

| ID | Requirement | Tests | Applies to | Matrix cell |
|---|---|---|---|---|
| config.input.dialect.default | `input.dialect` defaults to `"postgres"` | `unit: cmd/sqlgen/config: TestValidatePreParse_IgnoredInputKeys/database with defaulted paths` | all | config-fields: input.dialect × default |
| config.input.dialect.explicit | An explicit `input.dialect` (`mysql`, `sqlite`) is kept | `unit: cmd/sqlgen/config: TestValidatePreParse_IgnoredIntrospectSchemas/mysql database`; `unit: cmd/sqlgen/config: TestValidatePreParse_IgnoredIntrospectSchemas/sqlite both`; `golden: mysql`; `golden: sqlite` | all | config-fields: input.dialect × explicit |
| config.input.dialect.schema | The JSON Schema declares `input.dialect` as a string with exactly the values `postgres`, `mysql`, `sqlite` | `unit: cmd/sqlgen/config: TestSchemaV1_rejectsKnownMistakes/dialect postgresql instead of postgres`; `unit: cmd/sqlgen/config: TestSchemaV1_acceptsExampleConfigs` | all | config-fields: input.dialect × schema |
| config.input.source.default | `input.source` defaults to `"files"` | `unit: cmd/sqlgen/config: TestLoadConfig_MinimalWithDefaults` | all | config-fields: input.source × default |
| config.input.source.explicit | An explicit `input.source` (`database`, `both`) is kept and selects the schema source | `unit: cmd/sqlgen/config: TestValidatePreParse_IgnoredInputKeys/database with explicit paths`; `integration: cmd/sqlgen/cli: TestGenerate_DatabaseSourceMatchesFiles`; `integration: cmd/sqlgen/cli: TestGenerate_BothSourceAppliesOverlay` | all | config-fields: input.source × explicit |
| config.input.source.schema | The JSON Schema declares `input.source` as a string | `unit: cmd/sqlgen/config: TestSchemaV1_noDriftFromConfigStructs`; `unit: cmd/sqlgen/config: TestSchemaV1_acceptsUnexercisedSurface/database source with full connection`; `unit: cmd/sqlgen/config: TestSchemaV1_acceptsUnexercisedSurface/connection by url` | all | config-fields: input.source × schema |
| config.input.schema.default | `input.schema` defaults to `"*"` | `unit: cmd/sqlgen/config: TestLoadConfig_MinimalWithDefaults` | all | config-fields: input.schema × default |
| config.input.schema.explicit | An explicit `input.schema` is kept | `unit: cmd/sqlgen/config: TestLoadConfig_FullConfig` | all | config-fields: input.schema × explicit |
| config.input.schema.schema | The JSON Schema declares `input.schema` as a string | `unit: cmd/sqlgen/config: TestSchemaV1_noDriftFromConfigStructs` | all | config-fields: input.schema × schema |
| config.input.schema.resolves-unqualified | On PostgreSQL, an unqualified table name, view name or `REFERENCES` target in the SQL files resolves to `input.schema` | `unit: cmd/sqlgen/cli: TestParseSchema_ViewDefaultSchema/unqualified view, input.schema set` | postgres |  |
| config.input.schema.star-public | `"*"` resolves unqualified names to `public` | `unit: cmd/sqlgen/cli: TestParseSchema_ViewDefaultSchema/unqualified view, default input.schema`; `unit: cmd/sqlgen/cli: TestParseSchema_ViewDefaultSchema/unqualified materialized view, default input.schema` | postgres |  |
| config.input.schema.not-a-filter | `input.schema` does not filter which schemas are parsed or generated: a name qualified with another schema keeps it | `unit: cmd/sqlgen/cli: TestParseSchema_ViewDefaultSchema/qualified view keeps its schema` | postgres |  |
| config.input.paths.default | `input.paths` defaults to `["."]` |  | all | config-fields: input.paths × default |
| config.input.paths.explicit | Explicit `input.paths` are kept and parsed | `unit: cmd/sqlgen/config: TestLoadConfig_MinimalWithDefaults`; `golden: postgres` | all | config-fields: input.paths × explicit |
| config.input.paths.schema | The JSON Schema declares `input.paths` as an array of strings | `unit: cmd/sqlgen/config: TestSchemaV1_noDriftFromConfigStructs` | all | config-fields: input.paths × schema |
| config.input.paths.unread-under-database | Under `source: database` no `input.paths` file is read, the `["."]` default included | `integration: cmd/sqlgen/cli: TestGenerate_DatabaseSourceIgnoresPaths` | all |  |
| config.input.views.default | `input.views` defaults to `[]` |  | all | config-fields: input.views × default |
| config.input.views.explicit | Explicit `input.views` paths are kept and their files parsed as views | `golden: postgres` | all | config-fields: input.views × explicit |
| config.input.views.schema | The JSON Schema declares `input.views` as an array of strings | `unit: cmd/sqlgen/config: TestSchemaV1_noDriftFromConfigStructs` | all | config-fields: input.views × schema |
| config.input.views.views-only | Files under `input.views` are parsed exclusively as views, not as DDL |  | all |  |
| config.input.parse-mode.default | `input.parse_mode` defaults to `"strict"` | `unit: cmd/sqlgen/config: TestLoadConfig_MinimalWithDefaults` | all | config-fields: input.parse-mode × default |
| config.input.parse-mode.explicit | An explicit `input.parse_mode: merge` is kept and changes how duplicate table definitions across files are handled |  | all | config-fields: input.parse-mode × explicit |
| config.input.parse-mode.schema | The JSON Schema declares `input.parse_mode` as a string | `unit: cmd/sqlgen/config: TestSchemaV1_noDriftFromConfigStructs` | all | config-fields: input.parse-mode × schema |
| config.input.introspect.files-no-effect | `input.introspect` has no effect under `source: files` |  | all |  |

## §4.4 IntrospectConfig
<!-- prd: 4.4 sha256: 22ff145b5440e026 -->

| ID | Requirement | Tests | Applies to | Matrix cell |
|---|---|---|---|---|
| config.input.introspect.schemas.default | `input.introspect.schemas` defaults to `[]` (all) |  | postgres | config-fields: input.introspect.schemas × default |
| config.input.introspect.schemas.explicit | An explicit `input.introspect.schemas` list is kept and limits introspection to those schemas | `unit: cmd/sqlgen/config: TestLoadConfig_FullConfig`; `integration: cmd/sqlgen/cli: TestGenerate_IntrospectFilters` | postgres | config-fields: input.introspect.schemas × explicit |
| config.input.introspect.schemas.schema | The JSON Schema declares `input.introspect.schemas` as an array of strings | `unit: cmd/sqlgen/config: TestSchemaV1_noDriftFromConfigStructs`; `unit: cmd/sqlgen/config: TestSchemaV1_acceptsUnexercisedSurface/database source with full connection` | all | config-fields: input.introspect.schemas × schema |
| config.input.introspect.schemas.include | Only the listed schemas are introspected | `integration: cmd/sqlgen/cli: TestGenerate_IntrospectFilters`; `integration: parser/introspect: TestPostgresSchemaFiltering` | postgres |  |
| config.input.introspect.schemas.empty-all | An empty `schemas` list means all non-system schemas |  | postgres |  |
| config.input.introspect.exclude-schemas.default | `input.introspect.exclude_schemas` defaults to `[]` |  | postgres | config-fields: input.introspect.exclude-schemas × default |
| config.input.introspect.exclude-schemas.explicit | An explicit `input.introspect.exclude_schemas` list is kept and its schemas are not introspected | `unit: cmd/sqlgen/config: TestValidatePreParse_IgnoredInputKeys/files with every introspect field`; `integration: cmd/sqlgen/cli: TestGenerate_IntrospectFilters` | postgres | config-fields: input.introspect.exclude-schemas × explicit |
| config.input.introspect.exclude-schemas.schema | The JSON Schema declares `input.introspect.exclude_schemas` as an array of strings | `unit: cmd/sqlgen/config: TestSchemaV1_noDriftFromConfigStructs`; `unit: cmd/sqlgen/config: TestSchemaV1_acceptsUnexercisedSurface/database source with full connection` | all | config-fields: input.introspect.exclude-schemas × schema |
| config.input.introspect.exclude-schemas.after-inclusion | `exclude_schemas` is applied after inclusion: a schema both included and excluded is not introspected | `integration: parser/introspect: TestPostgresSchemaFiltering` | postgres |  |
| config.input.introspect.tables.default | `input.introspect.tables` defaults to `[]` (all) |  | all | config-fields: input.introspect.tables × default |
| config.input.introspect.tables.explicit | An explicit `input.introspect.tables` list is kept and limits introspection to those tables | `unit: cmd/sqlgen/config: TestValidatePreParse_IgnoredInputKeys/files with introspect tables`; `integration: cmd/sqlgen/cli: TestGenerate_IntrospectFilters` | all | config-fields: input.introspect.tables × explicit |
| config.input.introspect.tables.schema | The JSON Schema declares `input.introspect.tables` as an array of strings | `unit: cmd/sqlgen/config: TestSchemaV1_noDriftFromConfigStructs`; `unit: cmd/sqlgen/config: TestSchemaV1_acceptsUnexercisedSurface/database source with full connection` | all | config-fields: input.introspect.tables × schema |
| config.input.introspect.tables.empty-all | An empty `tables` list means every table in the selected schemas | `integration: cmd/sqlgen/cli: TestGenerate_DatabaseSourceMatchesFiles` | all |  |
| config.exclude-tables.loaded | The root `exclude_tables` list is read from the top of `sqlgen.yml`, in order | `unit: cmd/sqlgen/config: TestLoadConfig_TopLevelExcludeTables` | all |  |
| config.exclude-tables.drops | A table matched by any `exclude_tables` pattern is dropped from generation entirely | `unit: cmd/sqlgen/gen: TestBuildTableContexts_ExcludeTables` | all |  |
| config.exclude-tables.every-source | `exclude_tables` applies to file and introspected inputs alike |  | all |  |
| config.exclude-tables.no-tablename-constant | An excluded table gets no `hook.TableName` constant | `unit: cmd/sqlgen/gen: TestTableConstantName_MatchesTableNameFile` | all |  |
| config.exclude-tables.exact | A pattern with no wildcard matches the exact table name | `unit: cmd/sqlgen/config: TestMatchExcludeTablePattern/exact match`; `unit: cmd/sqlgen/config: TestMatchExcludeTablePattern/no match` | all |  |
| config.exclude-tables.star | `*` matches any sequence | `unit: cmd/sqlgen/config: TestMatchExcludeTablePattern/star prefix`; `unit: cmd/sqlgen/gen: TestBuildTableContexts_ExcludeTables` | all |  |
| config.exclude-tables.qmark | `?` matches exactly one character | `unit: cmd/sqlgen/config: TestMatchExcludeTablePattern/qmark single char`; `unit: cmd/sqlgen/config: TestMatchExcludeTablePattern/qmark too short` | all |  |
| config.exclude-tables.case-sensitive | Patterns are case-sensitive | `unit: cmd/sqlgen/config: TestMatchExcludeTablePattern/case sensitive` | all |  |
| config.exclude-tables.any-pattern | A table matching any one of several patterns is excluded; an empty list excludes nothing | `unit: cmd/sqlgen/config: TestMatchExcludeTablePattern/second pattern matches`; `unit: cmd/sqlgen/config: TestMatchExcludeTablePattern/empty patterns` | all |  |
| config.exclude-tables.schema | The JSON Schema declares root `exclude_tables` as an array of strings | `unit: cmd/sqlgen/config: TestSchemaV1_noDriftFromConfigStructs` | all |  |

## §4.5 OutputConfig
<!-- prd: 4.5 sha256: 68916ce91b21c581 -->

| ID | Requirement | Tests | Applies to | Matrix cell |
|---|---|---|---|---|
| config.output.driver.default | `output.driver` defaults to `"pgx"` | `unit: cmd/sqlgen/config: TestLoadConfig_MinimalWithDefaults` | all | config-fields: output.driver × default |
| config.output.driver.explicit | An explicit `output.driver: stdlib` is kept and is the driver the generated code imports | `golden: postgres_stdlib`; `unit: cmd/sqlgen/config: TestValidatePreParse_IgnoredIntrospectSchemas/sqlite both` | all | config-fields: output.driver × explicit |
| config.output.driver.schema | The JSON Schema declares `output.driver` as a string with exactly the values `pgx`, `stdlib` | `unit: cmd/sqlgen/config: TestSchemaV1_rejectsKnownMistakes/invalid driver`; `unit: cmd/sqlgen/config: TestSchemaV1_acceptsExampleConfigs` | all | config-fields: output.driver × schema |
| config.output.dir.default | `output.dir` defaults to `"."` | `unit: cmd/sqlgen/config: TestLoadConfig_EmptyDocument` | all | config-fields: output.dir × default |
| config.output.dir.explicit | An explicit `output.dir` is kept and is where generated artifacts are written | `unit: cmd/sqlgen/config: TestLoadConfig_MinimalWithDefaults`; `golden: postgres` | all | config-fields: output.dir × explicit |
| config.output.dir.schema | The JSON Schema declares `output.dir` as a string | `unit: cmd/sqlgen/config: TestSchemaV1_noDriftFromConfigStructs` | all | config-fields: output.dir × schema |
| config.output.package.default | With `output.package` omitted, the package name is derived from the `output.dir` directory name | `unit: cmd/sqlgen/config: TestLoadConfig_MinimalWithDefaults`; `unit: cmd/sqlgen/config: TestLoadConfig_OutputPackageDerivedFromDir/simple directory`; `unit: cmd/sqlgen/config: TestLoadConfig_OutputPackageDerivedFromDir/nested directory` | all | config-fields: output.package × default |
| config.output.package.explicit | An explicit `output.package` is kept, not re-derived from `output.dir` |  | all | config-fields: output.package × explicit |
| config.output.package.schema | The JSON Schema declares `output.package` as a string | `unit: cmd/sqlgen/config: TestSchemaV1_noDriftFromConfigStructs` | all | config-fields: output.package × schema |
| config.output.layout.default | `output.layout` defaults to `"single_file"` | `unit: cmd/sqlgen/config: TestLoadConfig_MinimalWithDefaults` | all | config-fields: output.layout × default |
| config.output.layout.explicit | An explicit `output.layout: file_per_table` is kept and selects the per-table file layout | `unit: cmd/sqlgen/config: TestLoadConfig_FullConfig`; `golden: sqlite` | all | config-fields: output.layout × explicit |
| config.output.layout.schema | The JSON Schema declares `output.layout` as a string with exactly the values `single_file`, `file_per_table` | `unit: cmd/sqlgen/config: TestSchemaV1_noDriftFromConfigStructs`; `unit: cmd/sqlgen/config: TestSchemaV1_acceptsExampleConfigs` | all | config-fields: output.layout × schema |
| config.output.enums.file.default | `output.enums.file` defaults to `"enums_gen.go"` | `unit: cmd/sqlgen/config: TestLoadConfig_MinimalWithDefaults`; `golden: postgres` | all | config-fields: output.enums.file × default |
| config.output.enums.file.explicit | An explicit `output.enums.file` is kept and names the enums file | `unit: cmd/sqlgen/gen: TestReservedPackageNamesMatrix/output.enums.file/table` | all | config-fields: output.enums.file × explicit |
| config.output.enums.file.schema | The JSON Schema declares `output.enums.file` as a string | `unit: cmd/sqlgen/config: TestSchemaV1_noDriftFromConfigStructs` | all | config-fields: output.enums.file × schema |
| config.output.enums.file.relative | A relative `output.enums.file` is resolved under `output.dir` | `golden: postgres` | all |  |
| config.output.enums.file.absolute | An absolute `output.enums.file` is used as written, not resolved under `output.dir` |  | all |  |
| config.output.enums.package.default | `output.enums.package` defaults to `output.package` | `unit: cmd/sqlgen/config: TestLoadConfig_MinimalWithDefaults`; `golden: postgres` | all | config-fields: output.enums.package × default |
| config.output.enums.package.explicit | An explicit `output.enums.package` is kept and is the enums file's package name |  | all | config-fields: output.enums.package × explicit |
| config.output.enums.package.schema | The JSON Schema declares `output.enums.package` as a string | `unit: cmd/sqlgen/config: TestSchemaV1_noDriftFromConfigStructs` | all | config-fields: output.enums.package × schema |
| config.output.types.file.default | `output.types.file` defaults to `"types_gen.go"` | `unit: cmd/sqlgen/config: TestLoadConfig_MinimalWithDefaults`; `golden: postgres` | all | config-fields: output.types.file × default |
| config.output.types.file.explicit | An explicit `output.types.file` is kept and names the composite/domain types file | `unit: cmd/sqlgen/gen: TestReservedPackageNamesMatrix/output.types.file/table` | all | config-fields: output.types.file × explicit |
| config.output.types.file.schema | The JSON Schema declares `output.types.file` as a string | `unit: cmd/sqlgen/config: TestSchemaV1_noDriftFromConfigStructs` | all | config-fields: output.types.file × schema |
| config.output.types.file.relative | A relative `output.types.file` is resolved under `output.dir` | `golden: postgres` | all |  |
| config.output.types.file.absolute | An absolute `output.types.file` is used as written, not resolved under `output.dir` |  | all |  |
| config.output.types.package.default | `output.types.package` defaults to `output.package` | `unit: cmd/sqlgen/config: TestLoadConfig_MinimalWithDefaults`; `golden: postgres` | all | config-fields: output.types.package × default |
| config.output.types.package.explicit | An explicit `output.types.package` is kept and is the types file's package name |  | all | config-fields: output.types.package × explicit |
| config.output.types.package.schema | The JSON Schema declares `output.types.package` as a string | `unit: cmd/sqlgen/config: TestSchemaV1_noDriftFromConfigStructs` | all | config-fields: output.types.package × schema |
| config.output.client.name.default | `output.client.name` defaults to `"Client"` | `unit: cmd/sqlgen/config: TestLoadConfig_MinimalWithDefaults`; `golden: postgres` | all | config-fields: output.client.name × default |
| config.output.client.name.explicit | An explicit `output.client.name` is kept and names the unified client struct | `unit: cmd/sqlgen/gen: TestValidateResolvedNames_ReservedNames/a renamed client reserves its new name instead` | all | config-fields: output.client.name × explicit |
| config.output.client.name.schema | The JSON Schema declares `output.client.name` as a string | `unit: cmd/sqlgen/config: TestSchemaV1_noDriftFromConfigStructs` | all | config-fields: output.client.name × schema |
| config.output.client.file.default | `output.client.file` defaults to `"client_gen.go"` | `unit: cmd/sqlgen/config: TestLoadConfig_MinimalWithDefaults`; `golden: postgres`; `unit: cmd/sqlgen/cli: TestGenerate_ManifestPointerInClientPackageDoc/file_per_table` | all | config-fields: output.client.file × default |
| config.output.client.file.explicit | An explicit `output.client.file` is kept: the client is written there and not to `client_gen.go` | `unit: cmd/sqlgen/cli: TestGenerate_ManifestPointerInClientPackageDoc/file_per_table with a custom client file` | all | config-fields: output.client.file × explicit |
| config.output.client.file.schema | The JSON Schema declares `output.client.file` as a string | `unit: cmd/sqlgen/config: TestSchemaV1_noDriftFromConfigStructs` | all | config-fields: output.client.file × schema |
| config.output.client.file.relative | `output.client.file` is relative to `output.dir` | `unit: cmd/sqlgen/cli: TestGenerate_ManifestPointerInClientPackageDoc/file_per_table with a custom client file` | all |  |
| config.output.client.always-generated | The unified client is always generated, even for a schema with no tables | `unit: cmd/sqlgen/cli: TestGenerate_ManifestPointerInClientPackageDoc/single_file with zero tables`; `golden: postgres` | all |  |

## §4.6 GenerationConfig
<!-- prd: 4.6 sha256: 4eaf217685646d90 -->

| ID | Requirement | Tests | Applies to | Matrix cell |
|---|---|---|---|---|
| config.generation.query-limit.default | With `generation.query_limit` unset, the resolved value is `1000` | `unit: cmd/sqlgen/config: TestLoadConfig_MinimalWithDefaults` | all | config-fields: generation.query-limit × default |
| config.generation.query-limit.explicit | An explicit `generation.query_limit` is kept | `unit: cmd/sqlgen/config: TestLoadConfig_FullConfig` | all | config-fields: generation.query-limit × explicit |
| config.generation.query-limit.schema | The JSON Schema declares `generation.query_limit` as an integer | `unit: cmd/sqlgen/config: TestSchemaV1_rejectsKnownMistakes/query_limit as a string` | all | config-fields: generation.query-limit × schema |
| config.generation.batch-size.default | With `generation.batch_size` unset, the resolved value is `200` | `unit: cmd/sqlgen/config: TestLoadConfig_MinimalWithDefaults` | all | config-fields: generation.batch-size × default |
| config.generation.batch-size.explicit | An explicit `generation.batch_size` is kept | `unit: cmd/sqlgen/config: TestLoadConfig_FullConfig` | all | config-fields: generation.batch-size × explicit |
| config.generation.batch-size.schema | The JSON Schema declares `generation.batch_size` as an integer | `unit: cmd/sqlgen/config: TestSchemaV1_noDriftFromConfigStructs` | all | config-fields: generation.batch-size × schema |
| config.generation.page-size.default | With `generation.page_size` unset, the resolved value is `100` | `unit: cmd/sqlgen/config: TestLoadConfig_MinimalWithDefaults` | all | config-fields: generation.page-size × default |
| config.generation.page-size.explicit | An explicit `generation.page_size` is kept | `unit: cmd/sqlgen/config: TestLoadConfig_FullConfig` | all | config-fields: generation.page-size × explicit |
| config.generation.page-size.schema | The JSON Schema declares `generation.page_size` as an integer | `unit: cmd/sqlgen/config: TestSchemaV1_noDriftFromConfigStructs` | all | config-fields: generation.page-size × schema |
| config.generation.cursor-keys.default | With `generation.cursor_keys` unset, the resolved value is `["id"]` | `unit: cmd/sqlgen/config: TestLoadConfig_MinimalWithDefaults` | all | config-fields: generation.cursor-keys × default |
| config.generation.cursor-keys.explicit | An explicit `generation.cursor_keys` list is kept | `unit: cmd/sqlgen/config: TestPerTableOverrideResolution` | all | config-fields: generation.cursor-keys × explicit |
| config.generation.cursor-keys.schema | The JSON Schema declares `generation.cursor_keys` as an array of strings | `unit: cmd/sqlgen/config: TestSchemaV1_noDriftFromConfigStructs` | all | config-fields: generation.cursor-keys × schema |
| config.generation.uuid-version.default | With `generation.uuid_version` unset, the resolved value is `"v4"` | `unit: cmd/sqlgen/config: TestLoadConfig_MinimalWithDefaults`; `unit: cmd/sqlgen/gen: TestBuildTableContexts_UUIDVersionResolution/v4 when neither the table nor the global config sets one` | all | config-fields: generation.uuid-version × default |
| config.generation.uuid-version.explicit | An explicit `generation.uuid_version: v7` is kept | `unit: cmd/sqlgen/config: TestLoadConfig_FullConfig` | all | config-fields: generation.uuid-version × explicit |
| config.generation.uuid-version.schema | The JSON Schema declares `generation.uuid_version` as a string with exactly the closed set `v4`, `v7` (§4.14) | `unit: cmd/sqlgen/config: TestSchemaV1_noDriftFromConfigStructs` | all | config-fields: generation.uuid-version × schema |
| config.generation.strict-updates.default | With `generation.strict_updates` unset, the resolved value is `true` | `unit: cmd/sqlgen/config: TestLoadConfig_MinimalWithDefaults` | all | config-fields: generation.strict-updates × default |
| config.generation.strict-updates.explicit | An explicit `generation.strict_updates: false` is kept | `unit: cmd/sqlgen/config: TestLoadConfig_FullConfig` | all | config-fields: generation.strict-updates × explicit |
| config.generation.strict-updates.schema | The JSON Schema declares `generation.strict_updates` as a boolean | `unit: cmd/sqlgen/config: TestSchemaV1_noDriftFromConfigStructs` | all | config-fields: generation.strict-updates × schema |
| config.generation.soft-delete-columns.default | With `generation.soft_delete_columns` unset, the resolved list is, in priority order, `deleted_at` (`timestamp`), `deleted_datetime` (`timestamp`), `is_deleted` (`bool`), `deleted_flag` (`bool`), `deleted` (`bool`) | `unit: cmd/sqlgen/config: TestLoadConfig_DefaultSoftDeleteColumns` | all | config-fields: generation.soft-delete-columns × default |
| config.generation.soft-delete-columns.explicit | An explicit `generation.soft_delete_columns` list is used in place of the default | `unit: cmd/sqlgen/cli: TestGenerate_notNullTimestampSoftDeleteRejected`; `unit: cmd/sqlgen/cli: TestValidate_softDeleteTypeMismatchReportedOnce` | all | config-fields: generation.soft-delete-columns × explicit |
| config.generation.soft-delete-columns.schema | The JSON Schema declares `generation.soft_delete_columns` as an array of `{name, type}` objects whose `type` is exactly the closed set `timestamp`, `bool`, `integer` (§4.14 soft-delete `type`) | `unit: cmd/sqlgen/config: TestSchemaV1_rejectsKnownMistakes/invalid soft delete column type` | all | config-fields: generation.soft-delete-columns × schema |
| config.generation.update-columns.default | With `generation.update_columns` unset, the resolved list is `[updated_at, updated_datetime, last_modified_at, modified_on]` | `unit: cmd/sqlgen/config: TestLoadConfig_DefaultUpdateColumns` | all | config-fields: generation.update-columns × default |
| config.generation.update-columns.explicit | An explicit `generation.update_columns` list is used in place of the default |  | all | config-fields: generation.update-columns × explicit |
| config.generation.update-columns.schema | The JSON Schema declares `generation.update_columns` as an array of strings | `unit: cmd/sqlgen/config: TestSchemaV1_noDriftFromConfigStructs` | all | config-fields: generation.update-columns × schema |
| config.generation.exclude-columns.default | With `generation.exclude_columns` unset, no column is excluded (`[]`) |  | all | config-fields: generation.exclude-columns × default |
| config.generation.exclude-columns.explicit | An explicit `generation.exclude_columns` list is kept | `unit: cmd/sqlgen/config: TestPerTableExcludeColumnsMerge` | all | config-fields: generation.exclude-columns × explicit |
| config.generation.exclude-columns.schema | The JSON Schema declares `generation.exclude_columns` as an array of strings | `unit: cmd/sqlgen/config: TestSchemaV1_noDriftFromConfigStructs` | all | config-fields: generation.exclude-columns × schema |
| config.generation.nested-mutations.enabled.default | With `generation.nested_mutations` (or its `enabled`) unset, nested mutations are off: no nested surface is emitted on any table | `unit: cmd/sqlgen/gen: TestNestedEligibility_DisabledEmitsNothing` | all | config-fields: generation.nested-mutations.enabled × default |
| config.generation.nested-mutations.enabled.explicit | An explicit `nested_mutations.enabled: true` is kept | `unit: cmd/sqlgen/config: TestLoadConfig_NestedMutationKeysParse` | all | config-fields: generation.nested-mutations.enabled × explicit |
| config.generation.nested-mutations.enabled.schema | The JSON Schema declares `generation.nested_mutations.enabled` as a boolean | `unit: cmd/sqlgen/config: TestSchemaV1_acceptsUnexercisedSurface/nested mutations block with every key`; `unit: cmd/sqlgen/config: TestSchemaV1_noDriftFromConfigStructs` | all | config-fields: generation.nested-mutations.enabled × schema |
| config.generation.nested-mutations.operations.default | With `nested_mutations.operations` unset, every family is emitted: `create`, `update` and `upsert` | `unit: cmd/sqlgen/gen: TestNestedEligibility_UpsertNeedsAConflictTarget` | all | config-fields: generation.nested-mutations.operations × default |
| config.generation.nested-mutations.operations.explicit | An explicit `nested_mutations.operations` list is kept: only the listed families are emitted | `unit: cmd/sqlgen/gen: TestNestedOperations_FollowNestedMutationsAlone` | all | config-fields: generation.nested-mutations.operations × explicit |
| config.generation.nested-mutations.operations.schema | The JSON Schema declares `nested_mutations.operations` as an array over the closed set `create`, `update`, `upsert` | `unit: cmd/sqlgen/config: TestSchemaV1_rejectsKnownMistakes/unknown nested mutation operation`; `unit: cmd/sqlgen/config: TestSchemaV1_acceptsUnexercisedSurface/nested mutations block with every key` | all | config-fields: generation.nested-mutations.operations × schema |
| config.generation.nested-mutations.verbs.default | With `nested_mutations.verbs` unset, all four verbs `create`, `connect`, `disconnect`, `clear` may appear | `unit: cmd/sqlgen/gen: TestNestedEligibility_VerbSetPerShape/Events`; `unit: cmd/sqlgen/gen: TestNestedEligibility_UnlinkVerbSetPerShape/Events` | all | config-fields: generation.nested-mutations.verbs × default |
| config.generation.nested-mutations.verbs.explicit | An explicit `nested_mutations.verbs` list is kept: a verb it omits is absent from every generated block | `unit: cmd/sqlgen/gen: TestNestedEligibility_VerbsMaskNarrowsTheUnlinkHalf` | all | config-fields: generation.nested-mutations.verbs × explicit |
| config.generation.nested-mutations.verbs.schema | The JSON Schema declares `nested_mutations.verbs` as an array over the closed set `create`, `connect`, `disconnect`, `clear` | `unit: cmd/sqlgen/config: TestSchemaV1_rejectsKnownMistakes/unknown nested mutation verb`; `unit: cmd/sqlgen/config: TestSchemaV1_acceptsUnexercisedSurface/nested mutations block with every key` | all | config-fields: generation.nested-mutations.verbs × schema |
| config.generation.nested-mutations.max-depth.default | With `nested_mutations.max_depth` unset, the depth is `1` | `unit: cmd/sqlgen/config: TestValidateNestedMutations_MaxDepth/omitted` | all | config-fields: generation.nested-mutations.max-depth × default |
| config.generation.nested-mutations.max-depth.schema | The JSON Schema declares `nested_mutations.max_depth` as an integer accepting `1` only | `unit: cmd/sqlgen/config: TestSchemaV1_rejectsKnownMistakes/nested mutations max depth other than one`; `unit: cmd/sqlgen/config: TestSchemaV1_acceptsUnexercisedSurface/nested mutations block with every key` | all | config-fields: generation.nested-mutations.max-depth × schema |
| config.generation.manifest.default | With `generation.manifest` unset, the manifest is disabled | `unit: cmd/sqlgen/config: TestLoadConfig_ManifestOmitted` | all | config-fields: generation.manifest × default |
| config.generation.manifest.explicit | An explicit `generation.manifest.enabled: true` is kept | `unit: cmd/sqlgen/config: TestLoadConfig_ManifestDefaultsApplied` | all | config-fields: generation.manifest × explicit |
| config.generation.manifest.schema | The JSON Schema declares `generation.manifest` as a `ManifestConfig` object | `unit: cmd/sqlgen/config: TestSchemaV1_acceptsUnexercisedSurface/manifest with mcp and breadcrumbs`; `unit: cmd/sqlgen/config: TestSchemaV1_noDriftFromConfigStructs` | all | config-fields: generation.manifest × schema |

| ID | Requirement | Tests | Applies to | Matrix cell |
|---|---|---|---|---|
| config.generation.query-limit.table-wins | A `tables.<t>.query_limit` takes precedence over `generation.query_limit` for that table | `unit: cmd/sqlgen/config: TestPerTableOverrideResolution` | all | config-resolution: generation.query-limit × table-wins |
| config.generation.query-limit.table-inherits | With `tables.<t>.query_limit` unset, the table gets `generation.query_limit` | `unit: cmd/sqlgen/config: TestPerTableOverrideResolution` | all | config-resolution: generation.query-limit × table-inherits |
| config.generation.batch-size.table-wins | A `tables.<t>.batch_size` takes precedence over `generation.batch_size` for that table | `unit: cmd/sqlgen/config: TestPerTableOverrideResolution`; `conformance: HardDeleteMany/splits the keys into sub-batches of the table's batch size` | all | config-resolution: generation.batch-size × table-wins |
| config.generation.batch-size.table-inherits | With `tables.<t>.batch_size` unset, the table gets `generation.batch_size` | `unit: cmd/sqlgen/config: TestPerTableOverrideResolution` | all | config-resolution: generation.batch-size × table-inherits |
| config.generation.page-size.table-wins | A `tables.<t>.page_size` takes precedence over `generation.page_size` for that table |  | all | config-resolution: generation.page-size × table-wins |
| config.generation.page-size.table-inherits | With `tables.<t>.page_size` unset, the table gets `generation.page_size` | `unit: cmd/sqlgen/config: TestPerTableOverrideResolution` | all | config-resolution: generation.page-size × table-inherits |
| config.generation.cursor-keys.table-wins | A `tables.<t>.cursor_keys` takes precedence over `generation.cursor_keys` for that table | `unit: cmd/sqlgen/config: TestPerTableOverrideResolution` | all | config-resolution: generation.cursor-keys × table-wins |
| config.generation.cursor-keys.table-inherits | With `tables.<t>.cursor_keys` unset, the table gets `generation.cursor_keys` | `unit: cmd/sqlgen/config: TestPerTableOverrideResolution` | all | config-resolution: generation.cursor-keys × table-inherits |
| config.generation.cursor-keys.view-wins | A `views.<v>.cursor_keys` takes precedence over `generation.cursor_keys` for that view | `unit: cmd/sqlgen/gen: TestRowIdentity_ViewWithoutPKFallsBackToCursorKeys` | all | config-resolution: generation.cursor-keys × view-wins |
| config.generation.cursor-keys.view-inherits | With `views.<v>.cursor_keys` unset, the view gets `generation.cursor_keys` ("applied as the default to every table and view unless overridden") |  | all | config-resolution: generation.cursor-keys × view-inherits |
| config.generation.strict-updates.table-wins | A `tables.<t>.strict_updates` takes precedence over `generation.strict_updates` for that table | `unit: cmd/sqlgen/config: TestPerTableOverrideResolution`; `example: postgres: TestIncrement_MissingPK_NonStrict_ReturnsNil`; `example: mysql: TestIncrement_MissingPK_NonStrict_ReturnsNil_MySQL`; `example: sqlite: TestIncrement_MissingPK_NonStrict_ReturnsNil_SQLite` | all | config-resolution: generation.strict-updates × table-wins |
| config.generation.strict-updates.table-inherits | With `tables.<t>.strict_updates` unset, the table gets `generation.strict_updates` | `unit: cmd/sqlgen/config: TestPerTableOverrideResolution` | all | config-resolution: generation.strict-updates × table-inherits |
| config.generation.exclude-columns.table-merges | A table's effective `exclude_columns` is the union of `generation.exclude_columns` and `tables.<t>.exclude_columns`; the table list does not replace the global one | `unit: cmd/sqlgen/config: TestPerTableExcludeColumnsMerge` | all | config-resolution: generation.exclude-columns × table-wins |
| config.generation.exclude-columns.table-inherits | With `tables.<t>.exclude_columns` unset, the table excludes the `generation.exclude_columns` columns | `unit: cmd/sqlgen/config: TestValidatePostParse_ExcludedPrimaryKey/global exclusion of the only key column` | all | config-resolution: generation.exclude-columns × table-inherits |
| config.generation.uuid-version.table-wins | A `tables.<t>.primary_key.uuid_version` takes precedence over `generation.uuid_version` for that table | `unit: cmd/sqlgen/gen: TestBuildTableContexts_UUIDVersionResolution/table version wins over the global version`; `unit: cmd/sqlgen/gen: TestBuildTableContexts_UUIDVersionResolution/table v4 wins over a global v7` | all | config-resolution: generation.uuid-version × table-wins |
| config.generation.uuid-version.table-inherits | With `tables.<t>.primary_key.uuid_version` unset, the table gets `generation.uuid_version` | `unit: cmd/sqlgen/gen: TestBuildTableContexts_UUIDVersionResolution/global version applies when the table sets none`; `unit: cmd/sqlgen/gen: TestBuildTableContexts_UUIDVersionResolution/v4 when neither the table nor the global config sets one` | all | config-resolution: generation.uuid-version × table-inherits |

| ID | Requirement | Tests | Applies to | Matrix cell |
|---|---|---|---|---|
| config.generation.limit.nil-uses-query-limit | A `GetMany` whose `Limit` is `nil` applies the table's `query_limit` | `unit: cmd/sqlgen/gen: TestGetTemplate_getManyQueryBuilding` | all |  |
| config.generation.limit.zero-no-limit | `Limit: new(0)` means no limit: the generated code passes `nil` to `SelectOptions.Limit`, so the `LIMIT` clause is omitted | `example: postgres: TestRelationshipLoad_PastBindLimit`; `example: postgres_stdlib: TestRelationshipLoad_PastBindLimit`; `example: mysql: TestRelationshipLoad_PastBindLimit`; `example: sqlite: TestRelationshipLoad_PastBindLimit` | all |  |
| config.generation.limit.n-uses-n | `Limit: new(N)` limits `GetMany` to N rows |  | all |  |
| config.generation.offset.zero-omitted | `Offset: new(0)` passes `nil` to `SelectOptions.Offset`, so the `OFFSET` clause is omitted |  | all |  |
| config.generation.batch-size.create-many | `CreateMany` splits its input into sub-batches of the table's `batch_size` |  | all |  |
| config.generation.batch-size.update-many | `UpdateMany` splits its input into sub-batches of the table's `batch_size` |  | all |  |
| config.generation.page-size.pagination-default | Pagination with no page size given uses the table's `page_size` | `unit: cmd/sqlgen/gen: TestPaginationTableTemplate_connectionForward`; `unit: cmd/sqlgen/gen: TestPaginationTableTemplate_connectionFirstPage` | all |  |
| config.generation.strict-updates.update-not-found | With `strict_updates: true`, `Update` on a PK that does not exist returns `ErrNotFound` | `unit: cmd/sqlgen/gen: TestUpdateTemplate_strictUpdates` | all |  |
| config.generation.strict-updates.increment-not-found | With `strict_updates: true`, `Increment` on a PK that does not exist returns `ErrNotFound` | `unit: cmd/sqlgen/gen: TestIncrementTemplate_strictUpdatesMissingPK`; `example: postgres: TestIncrement_MissingPK_StrictUpdates_ReturnsErrNotFound`; `example: mysql: TestIncrement_MissingPK_StrictUpdates_ReturnsErrNotFound_MySQL`; `example: sqlite: TestIncrement_MissingPK_StrictUpdates_ReturnsErrNotFound_SQLite` | all |  |
| config.generation.strict-updates.update-nil | With `strict_updates: false`, `Update` on a PK that does not exist returns `nil` | `unit: cmd/sqlgen/gen: TestUpdateTemplate_noStrictUpdates` | all |  |
| config.generation.strict-updates.increment-nil | With `strict_updates: false`, `Increment` on a PK that does not exist returns `nil` | `example: postgres: TestIncrement_MissingPK_NonStrict_ReturnsNil`; `example: mysql: TestIncrement_MissingPK_NonStrict_ReturnsNil_MySQL`; `example: sqlite: TestIncrement_MissingPK_NonStrict_ReturnsNil_SQLite` | all |  |
| config.generation.soft-delete-columns.priority-order | `soft_delete_columns` is checked in list (priority) order: a table holding two listed columns uses the earlier one, and a missing column is skipped for the next | `unit: parser: TestDetectSoftDelete/priority ordering: deleted_at wins over is_deleted`; `unit: parser: TestDetectSoftDelete/priority: skips missing column, finds second match` | all |  |
| config.generation.soft-delete-columns.compatible-types | A soft delete column of a compatible SQL type is accepted for its configured `type`: `timestamp` with `timestamp` / `timestamptz` / `datetime`; `bool` with `bool` / `boolean` / `tinyint` / MySQL `tinyint(1)`; `integer` with `int` / `integer` / `smallint` / `tinyint` | `unit: cmd/sqlgen/config: TestValidatePostParse_SoftDeleteTypeMatch` | all |  |
| config.generation.soft-delete-columns.configured-type-strategy | A column type compatible with more than one row (MySQL `tinyint`, `tinyint(1)`) takes the strategy of the configured `type` | `unit: cmd/sqlgen/gen: TestBuildTableContexts_SoftDeleteStrategyFromConfiguredType` | mysql |  |
| config.generation.update-columns.detected | A table column whose name is in `update_columns` is detected as a last-modification-time column | `unit: parser: TestDetectUpdateColumns/returns all matching columns in config order`; `unit: cmd/sqlgen/gen: TestUpdateTemplate_autoSetUpdateColumns` | all |  |
| config.generation.exclude-columns.removed | A column named in `exclude_columns` is left out of the generated code for every table |  | all |  |
| config.generation.operations.upsert-gate | A table that emits no conflict-target constant gets no `Upsert` and no `UpsertMany` | `unit: cmd/sqlgen/gen: TestUpsert_OmittedWithoutAConflictTarget` | all |  |
| config.generation.operations.every-other-method | `Get`, `GetMany`, `Count`, `Exists`, `ExistsWhere`, `Stream`, `Paginate`, `Connection`, `Create`, `CreateMany`, `Update`, `UpdateMany`, `UpdateWhere`, `HardDelete`, `HardDeleteMany` and `HardDeleteWhere` are generated for every table with a primary key | `unit: cmd/sqlgen/gen: TestClientTemplate_fullOperations` | all |  |

| Matrix cell | Reason | Accepted |
|---|---|---|
| config-fields: generation.nested-mutations.max-depth × explicit | §4.6: `1` is the only accepted value in v1, so no non-default value can be set and kept | |
| config-resolution: generation.query-limit × view-wins | §4.9: ViewConfig has no `query_limit` field | |
| config-resolution: generation.query-limit × view-inherits | §4.9: ViewConfig has no `query_limit` field | |
| config-resolution: generation.batch-size × view-wins | §4.9: ViewConfig has no `batch_size` field (and §4.6 Operations: a view has no write method to batch) | |
| config-resolution: generation.batch-size × view-inherits | §4.9: ViewConfig has no `batch_size` field | |
| config-resolution: generation.page-size × view-wins | §4.9: ViewConfig has no `page_size` field | |
| config-resolution: generation.page-size × view-inherits | §4.9: ViewConfig has no `page_size` field | |
| config-resolution: generation.strict-updates × view-wins | §4.9: ViewConfig has no `strict_updates` field (a view has no `Update` / `Increment`, §4.6 Operations) | |
| config-resolution: generation.strict-updates × view-inherits | §4.9: ViewConfig has no `strict_updates` field | |
| config-resolution: generation.exclude-columns × view-wins | §4.9: ViewConfig has no `exclude_columns` field | |
| config-resolution: generation.exclude-columns × view-inherits | §4.9: ViewConfig has no `exclude_columns` field | |
| config-resolution: generation.uuid-version × view-wins | §4.9: ViewConfig has no `primary_key` block (and a view has no `Create`, §4.6 Operations) | |
| config-resolution: generation.uuid-version × view-inherits | §4.9: ViewConfig has no `primary_key` block | |

## §4.7 OverrideConfig
<!-- prd: 4.7 sha256: 5bcb83f15685165a -->

| ID | Requirement | Tests | Applies to | Matrix cell |
|---|---|---|---|---|
| config.overrides.use-pointers.default | With `overrides.use_pointers` unset, the resolved value is `true` | `unit: cmd/sqlgen/config: TestLoadConfig_MinimalWithDefaults` | all | config-fields: overrides.use-pointers × default |
| config.overrides.use-pointers.explicit | An explicit `overrides.use_pointers: false` is kept | `unit: cmd/sqlgen/config: TestLoadConfig_FullConfig` | all | config-fields: overrides.use-pointers × explicit |
| config.overrides.use-pointers.schema | The JSON Schema declares `overrides.use_pointers` as a boolean | `unit: cmd/sqlgen/config: TestSchemaV1_noDriftFromConfigStructs` | all | config-fields: overrides.use-pointers × schema |
| config.overrides.types.default | With `overrides.types` unset, the map is empty (`{}`): no SQL type is overridden |  | all | config-fields: overrides.types × default |
| config.overrides.types.explicit | Explicit `overrides.types` entries are kept, `type`, `import` and the `nullable` variant (shorthand and full form) included | `unit: cmd/sqlgen/config: TestLoadConfig_FullConfig` | all | config-fields: overrides.types × explicit |
| config.overrides.types.schema | The JSON Schema declares `overrides.types` as a map of SQL type name to a `TypeOverride` object (`type`, `import`, `zero_value`, `nullable`), with `nullable` either a type-name string or a `NullableOverride` object | `unit: cmd/sqlgen/config: TestSchemaV1_acceptsBothOperationsForms/nullable shorthand`; `unit: cmd/sqlgen/config: TestSchemaV1_acceptsBothOperationsForms/nullable full form`; `unit: cmd/sqlgen/config: TestSchemaV1_rejectsKnownMistakes/removed convert block on a type override` | all | config-fields: overrides.types × schema |
| config.overrides.types.nullable.valid-field.default | With `nullable.valid_field` (and `valid_method`) unset, validity is read from the `Valid` field | `unit: cmd/sqlgen/gotype: TestCustomWrapperValidFieldDefault` | all | config-fields: overrides.types.nullable.valid-field × default |
| config.overrides.types.nullable.valid-field.explicit | An explicit `nullable.valid_field` is kept: the guard reads that field | `unit: cmd/sqlgen/gotype: TestDeriveScalarExtraction_CustomValidField` | all | config-fields: overrides.types.nullable.valid-field × explicit |
| config.overrides.types.nullable.valid-field.schema | The JSON Schema declares `nullable.valid_field` as a string | `unit: cmd/sqlgen/config: TestSchemaV1_noDriftFromConfigStructs` | all | config-fields: overrides.types.nullable.valid-field × schema |

| ID | Requirement | Tests | Applies to | Matrix cell |
|---|---|---|---|---|
| config.overrides.types.table-wins | A `tables.<t>.overrides.types.<type>` entry takes precedence over the global `overrides.types.<type>` for that table | `unit: cmd/sqlgen/gotype: TestResolutionChainPrecedence/table overrides override global and integrations`; `unit: cmd/sqlgen/gotype: TestForSchema_QualifiedOverrideKey/table scope still precedes global` | all | config-resolution: overrides.types × table-wins |
| config.overrides.types.table-inherits | With no `tables.<t>.overrides.types.<type>` entry, the table's columns of that type take the global `overrides.types.<type>` | `unit: cmd/sqlgen/gotype: TestResolutionChainPrecedence/global overrides override integrations and dialect` | all | config-resolution: overrides.types × table-inherits |
| config.overrides.use-pointers.table-wins | A `tables.<t>.overrides.use_pointers` takes precedence over the global `overrides.use_pointers` for that table's nullable columns (§4.8: the per-table `overrides` block has the global structure) |  | all | config-resolution: overrides.use-pointers × table-wins |
| config.overrides.use-pointers.table-inherits | With `tables.<t>.overrides.use_pointers` unset, the table's nullable columns follow the global `overrides.use_pointers` | `unit: cmd/sqlgen/gotype: TestNullableSQLNullTypes`; `unit: cmd/sqlgen/gen: TestColumnOverrideNullableUnderUsePointersFalse/dialect mapping takes the nullable wrapper` | all | config-resolution: overrides.use-pointers × table-inherits |

| ID | Requirement | Tests | Applies to | Matrix cell |
|---|---|---|---|---|
| config.overrides.types.import-required-qualified | A package-qualified `type` with no `import`, naming a package sqlgen does not already know, is a config error, at global and table scope | `unit: cmd/sqlgen/gen: TestTypeOverrideRejectsUnimportableLiteral/global overrides.types without an import`; `unit: cmd/sqlgen/gen: TestTypeOverrideRejectsUnimportableLiteral/global overrides.types naming a UUID library without an import`; `unit: cmd/sqlgen/gen: TestTypeOverrideRejectsUnimportableLiteral/table-scoped overrides.types without an import`; `unit: cmd/sqlgen/gen: TestTypeOverrideImportsReachesGraphQLGen` | all |  |
| config.overrides.types.import-omitted-builtin | A builtin `type` (`string`, `int64`, …) needs no `import` | `unit: cmd/sqlgen/gen: TestTypeOverrideAcceptsImportableLiteral/builtin needs no import`; `unit: cmd/sqlgen/gen: TestTypeOverrideAcceptsImportableLiteral/table-scoped builtins need no import` | all |  |
| config.overrides.types.import-omitted-same-package | A same-package `type` (`Address`) needs no `import` | `unit: cmd/sqlgen/gen: TestTypeOverrideAcceptsImportableLiteral/same-package type needs none` | all |  |
| config.overrides.types.import-omitted-known-literal | A built-in literal that resolves its own import (`time.Time`, `json.RawMessage`, `types.JSON`, `net.IP`) needs no `import` | `unit: cmd/sqlgen/gen: TestTypeOverrideAcceptsImportableLiteral/registry member supplies its own import` | all |  |
| config.overrides.types.nullable-shorthand-shares-import | The shorthand `nullable: <Type>` takes the parent's import | `unit: cmd/sqlgen/gen: TestTypeOverrideAcceptsImportableLiteral/the nullable shorthand inherits the parent import` | all |  |
| config.overrides.types.nullable-import-uninheritable | A package-qualified `nullable.type` with no import on either half (the parent is a builtin or same-package type) is a config error | `unit: cmd/sqlgen/gen: TestTypeOverrideRejectsUnimportableLiteral/a nullable variant with no import to inherit`; `unit: cmd/sqlgen/gen: TestTypeOverrideImportsRejectsUninheritableNullWrapper` | all |  |
| config.overrides.types.zero-value-derived | With `zero_value` unset, the zero value is derived: `Type{}` for a struct, `""` for a string, `0` for a number |  | all |  |
| config.overrides.types.scanner-valuer | A named type implementing `sql.Scanner` and `driver.Valuer` crosses the driver boundary on its own: values round-trip | `example: postgres: TestKSUIDOverrideRoundTrip`; `example: sqlite: TestKSUIDOverrideRoundTrip` | all |  |
| config.overrides.types.no-conversion | sqlgen performs no conversion of its own: the scan target is always the resolved Go type | `unit: cmd/sqlgen/config: TestSchemaV1_rejectsKnownMistakes/removed convert block on a type override` | all |  |
| config.overrides.types.unfillable-runtime-error | A type the driver cannot fill is a runtime scan error naming the column, not a generation-time error |  | all |  |
| config.overrides.nullable.valid-field-and-method | Setting both `nullable.valid_field` and `nullable.valid_method` is rejected | `unit: cmd/sqlgen/config: TestValidatePreParse_NullableValidConflict`; `unit: cmd/sqlgen/config: TestValidatePreParse_NullableValidConfigsPass/valid_field alone`; `unit: cmd/sqlgen/config: TestValidatePreParse_NullableValidConfigsPass/valid_method alone` | all |  |
| config.overrides.nullable.valid-invert-without-method | Setting `nullable.valid_invert: true` without `nullable.valid_method` is rejected | `unit: cmd/sqlgen/config: TestValidatePreParse_NullableInvertWithoutMethod`; `unit: cmd/sqlgen/config: TestValidatePreParse_NullableValidConfigsPass/valid_method + invert` | all |  |
| config.overrides.nullable.underlying-field-without-type | Setting `nullable.underlying_field` without `nullable.type` is rejected | `unit: cmd/sqlgen/config: TestValidatePreParse_NullableUnderlyingFieldWithoutType`; `unit: cmd/sqlgen/config: TestValidatePreParse_NullableValidConfigsPass/default (Valid)` | all |  |

| Matrix cell | Reason | Accepted |
|---|---|---|
| config-resolution: overrides.types × view-wins | §4.9: ViewConfig has no `overrides` block | |
| config-resolution: overrides.use-pointers × view-wins | §4.9: ViewConfig has no `overrides` block | |
| config-resolution: overrides.use-pointers × view-inherits | §4.9: ViewConfig has no `overrides` block | |

## §4.8 TableConfig (per-table)
<!-- prd: 4.8 sha256: 9d39adf0fc4a3e1b -->

| ID | Requirement | Tests | Applies to | Matrix cell |
|---|---|---|---|---|
| config.tables.struct-name.default | With `tables.<t>.struct_name` unset, the table's struct name is the singular PascalCase of the table name | `unit: cmd/sqlgen/gen: TestStructName/products no collision`; `unit: cmd/sqlgen/gen: TestStructName/order_items no collision` | all | config-fields: tables.struct-name × default |
| config.tables.struct-name.explicit | An explicit `tables.<t>.struct_name` is the table's struct name | `unit: cmd/sqlgen/gen: TestTableContext_SnakeName_StructNameOverride` | all | config-fields: tables.struct-name × explicit |
| config.tables.struct-name.schema | `v1.json` declares `tables.<t>.struct_name` as a string | `unit: cmd/sqlgen/config: TestSchemaV1_noDriftFromConfigStructs` | all | config-fields: tables.struct-name × schema |
| config.tables.description.overrides-sql-comment | `tables.<t>.description` is the struct's doc comment, in place of the parsed SQL comment | `unit: cmd/sqlgen/gen: TestDocCommentResolution_configOverridesSQL` | all | |
| config.tables.primary-key.fields | `primary_key` takes `strategy`, `uuid_version` and `columns`, and `columns` may be set beside `strategy` | `unit: cmd/sqlgen/config: TestLoadConfig_PrimaryKeyColumns` | all | |
| config.tables.overrides.same-structure | `tables.<t>.overrides` has the structure of the global `overrides` block | | all | |

| ID | Requirement | Tests | Applies to | Matrix cell |
|---|---|---|---|---|
| config.tables.uuid-version.app-only | `primary_key.uuid_version` applies only when the table's strategy is `app` | | all | |

| ID | Requirement | Tests | Applies to | Matrix cell |
|---|---|---|---|---|
| config.tables.column-map.name.default | With `column_map.<col>.name` unset, the field name is the PascalCase of the SQL column name | `unit: cmd/sqlgen/gen: TestFieldName/created_at` | all | config-fields: tables.column-map.name × default |
| config.tables.column-map.name.explicit | An explicit `column_map.<col>.name` is the column's Go field name | `unit: cmd/sqlgen/gen: TestColumnNameOverride_ResolvesOnColumnContext` | all | config-fields: tables.column-map.name × explicit |
| config.tables.column-map.name.schema | `v1.json` declares `column_map.<col>.name` as a string | `unit: cmd/sqlgen/config: TestSchemaV1_noDriftFromConfigStructs` | all | config-fields: tables.column-map.name × schema |
| config.tables.column-map.access.default | With `column_map.<col>.access` unset, the column's role is `public` | `unit: cmd/sqlgen/gen: TestResolveAccess/empty resolves to public`; `unit: cmd/sqlgen/gen: TestBuildTableContexts_AccessResolution` | all | config-fields: tables.column-map.access × default |
| config.tables.column-map.access.explicit | An explicit `read_only`, `write_only`, `hidden` or `internal` is the column's role | `unit: cmd/sqlgen/gen: TestBuildTableContexts_AccessResolution` | all | config-fields: tables.column-map.access × explicit |
| config.tables.column-map.access.schema | `v1.json` declares `column_map.<col>.access` as a string with exactly the closed set `public`, `read_only`, `write_only`, `hidden`, `internal` | `unit: cmd/sqlgen/config: TestSchemaV1_rejectsKnownMistakes/invalid column access role` | all | config-fields: tables.column-map.access × schema |
| config.tables.column-map.description.overrides-sql-comment | `column_map.<col>.description` is the field's doc comment, in place of the parsed SQL comment | `unit: cmd/sqlgen/gen: TestDocCommentResolution_configOverridesSQL` | all | |
| config.tables.column-map.type.nullable-no-wrapper | A nullable column retyped by `column_map.<col>.type` never takes an `overrides.types` `nullable:` wrapper: `decimal.Decimal` resolves to `*decimal.Decimal`, not `decimal.NullDecimal` | | all | |
| config.doc-comment.struct-sql-fallback | With no `tables.<t>.description`, the struct's doc comment is the parsed table comment | `unit: cmd/sqlgen/gen: TestDocCommentResolution_SQLWhenNoConfig` | all | |
| config.doc-comment.field-sql-fallback | With no `column_map.<col>.description`, the field's doc comment is the parsed column comment | `unit: cmd/sqlgen/gen: TestDocCommentResolution_SQLWhenNoConfig` | all | |
| config.doc-comment.postgres-comment-on | On PostgreSQL, `COMMENT ON TABLE` and `COMMENT ON COLUMN` produce the struct and field doc comments without config | `golden: postgres` | postgres | |
| config.doc-comment.mysql-inline-comment | On MySQL, the inline table and column `COMMENT` produce the struct and field doc comments without config | `golden: mysql` | mysql | |
| config.doc-comment.none | With neither a config `description` nor a SQL comment, no doc comment is generated, for structs and fields alike | `unit: cmd/sqlgen/gen: TestDocCommentResolution_noneWhenNeitherExists`; `unit: cmd/sqlgen/gen: TestModelTemplate_noDocCommentWhenEmpty`; `unit: cmd/sqlgen/gen: TestFieldTemplate_noDocCommentWhenEmpty` | all | |
| config.doc-comment.verbatim | A description, from SQL or from config, is rendered verbatim with no lowercasing or other transformation | `unit: cmd/sqlgen/gen: TestDocCommentResolution_SQLWhenNoConfig`; `unit: cmd/sqlgen/gen: TestModelTemplate_docComment`; `unit: cmd/sqlgen/gen: TestFieldTemplate_withDocComment` | all | |
| config.doc-comment.not-merged | A config `description` replaces the SQL comment entirely; the two are not merged | `unit: cmd/sqlgen/gen: TestDocCommentResolution_configOverridesSQL` | all | |

| ID | Requirement | Tests | Applies to | Matrix cell |
|---|---|---|---|---|
| config.tables.relationships.type.spellings | A relationship's `type` accepts `o2o`, `o2m`, `m2m`, `one_to_one`, `one_to_many` and `many_to_many` | `unit: cmd/sqlgen/config: TestValidatePreParse_RelationshipType/o2o`; `unit: cmd/sqlgen/config: TestValidatePreParse_RelationshipType/one_to_one`; `unit: cmd/sqlgen/config: TestValidatePreParse_RelationshipType/o2m`; `unit: cmd/sqlgen/config: TestValidatePreParse_RelationshipType/one_to_many`; `unit: cmd/sqlgen/config: TestValidatePreParse_RelationshipType/m2m`; `unit: cmd/sqlgen/config: TestValidatePreParse_RelationshipType/many_to_many` | all | |
| config.tables.relationships.type.case-insensitive | `type` is matched case-insensitively | `unit: cmd/sqlgen/config: TestValidatePreParse_RelationshipType/upper-case spelling`; `unit: cmd/sqlgen/config: TestSchemaV1_relationshipSpellingsMatchValidate/O2O with fk`; `unit: cmd/sqlgen/config: TestSchemaV1_relationshipSpellingsMatchValidate/One_To_One with fk`; `unit: cmd/sqlgen/config: TestSchemaV1_relationshipSpellingsMatchValidate/O2m with fk`; `unit: cmd/sqlgen/config: TestSchemaV1_relationshipSpellingsMatchValidate/ONE_TO_MANY with fk`; `unit: cmd/sqlgen/config: TestSchemaV1_relationshipSpellingsMatchValidate/M2M with junction`; `unit: cmd/sqlgen/config: TestSchemaV1_relationshipSpellingsMatchValidate/Many_To_Many with junction` | all | |
| config.tables.relationships.type.other-value | Any other `type` value is a config error | `unit: cmd/sqlgen/config: TestValidatePreParse_RelationshipType/hyphenated spelling`; `unit: cmd/sqlgen/config: TestValidatePreParse_RelationshipType/ratio spelling`; `unit: cmd/sqlgen/config: TestValidatePreParse_RelationshipType/missing underscores`; `unit: cmd/sqlgen/config: TestValidatePreParse_RelationshipType/padded spelling` | all | |
| config.tables.relationships.type.omitted | An omitted `type` is a config error | `unit: cmd/sqlgen/config: TestValidatePreParse_RelationshipType/missing type` | all | |
| config.tables.relationships.type.schema | `v1.json` requires `type` and admits exactly the six spellings in any letter case | `unit: cmd/sqlgen/config: TestSchemaV1_rejectsKnownMistakes/relationship missing type`; `unit: cmd/sqlgen/config: TestSchemaV1_relationshipSpellingsMatchValidate/hyphenated type`; `unit: cmd/sqlgen/config: TestSchemaV1_relationshipSpellingsMatchValidate/type with surrounding spaces`; `unit: cmd/sqlgen/config: TestSchemaV1_relationshipSpellingsMatchValidate/type with a trailing space`; `unit: cmd/sqlgen/config: TestSchemaV1_relationshipSpellingsMatchValidate/O2O with fk`; `unit: cmd/sqlgen/config: TestSchemaV1_relationshipSpellingsMatchValidate/Many_To_Many with junction` | all | |
| config.tables.relationships.side.default | On an O2O entry with `side` unset, the entry is the `parent` side | `unit: cmd/sqlgen/gen: TestBuildTableContexts_relationshipFKOnTarget/both O2O edges declare the same Side` | all | config-fields: tables.relationships.side × default |
| config.tables.relationships.side.explicit | An explicit `side: child` is kept, and the manifest emits `kind: "m2o"` for the entry | `unit: cmd/sqlgen/manifest: TestBuild_O2OSideDisambiguation` | all | config-fields: tables.relationships.side × explicit |
| config.tables.relationships.side.schema | `v1.json` declares `side` as a string with exactly the closed set `parent`, `child` in any letter case | `unit: cmd/sqlgen/config: TestSchemaV1_rejectsKnownMistakes/misspelled relationship side`; `unit: cmd/sqlgen/config: TestSchemaV1_rejectsKnownMistakes/relationship side with surrounding spaces`; `unit: cmd/sqlgen/config: TestSchemaV1_relationshipSpellingsMatchValidate/side Child`; `unit: cmd/sqlgen/config: TestSchemaV1_relationshipSpellingsMatchValidate/side PARENT` | all | config-fields: tables.relationships.side × schema |
| config.tables.relationships.side.case-insensitive | `side` is matched case-insensitively | `unit: cmd/sqlgen/config: TestSchemaV1_relationshipSpellingsMatchValidate/side Child`; `unit: cmd/sqlgen/config: TestSchemaV1_relationshipSpellingsMatchValidate/side PARENT` | all | |
| config.tables.relationships.side.ignored-non-o2o | `side` is ignored on `o2m` and `m2m` entries | | all | |
| config.tables.relationships.table.required | A relationship missing `table` is a config error | | all | |
| config.tables.relationships.table.schema | `v1.json` requires a relationship's `table` | `unit: cmd/sqlgen/config: TestSchemaV1_rejectsKnownMistakes/relationship missing table` | all | |
| config.tables.relationships.junction.inferred-keys | Without `junction_local_fk` / `junction_reference_fk`, an M2M entry's junction columns are inferred from the linked table names (`product_id`, `tag_id`) | | all | |
| config.tables.relationships.junction.local-fk-override | `junction_local_fk` names the junction column for this table in place of the inferred one | | all | |
| config.tables.relationships.junction.reference-fk-override | `junction_reference_fk` names the junction column for the related table in place of the inferred one | | all | |
| config.tables.relationships.discriminator.column-required | A `discriminator` without `column` is a config error | `unit: cmd/sqlgen/config: TestValidateNestedMutations_RequiredKeys/discriminator without column` | all | |
| config.tables.relationships.discriminator.value-required | A `discriminator` without `value` is a config error; an empty-string `value` is a value | `unit: cmd/sqlgen/config: TestValidateNestedMutations_RequiredKeys/discriminator without value`; `unit: cmd/sqlgen/config: TestValidateNestedMutations_RequiredKeys/explicit empty-string value is not absence` | all | |
| config.tables.relationships.discriminator.value-scalar | `discriminator.value` is a scalar: a string, number or boolean is accepted, and a sequence or mapping is a config error | `unit: cmd/sqlgen/config: TestValidateDiscriminatorValue/string`; `unit: cmd/sqlgen/config: TestValidateDiscriminatorValue/int`; `unit: cmd/sqlgen/config: TestValidateDiscriminatorValue/bool`; `unit: cmd/sqlgen/config: TestValidateDiscriminatorValue/float`; `unit: cmd/sqlgen/config: TestValidateDiscriminatorValue/a sequence has no rendering`; `unit: cmd/sqlgen/config: TestValidateDiscriminatorValue/a mapping has no rendering` | all | |
| config.tables.relationships.discriminator.schema | `v1.json` requires `column` and `value`, and types `value` as a scalar | `unit: cmd/sqlgen/config: TestSchemaV1_acceptsUnexercisedSurface/relationship discriminator with a string value`; `unit: cmd/sqlgen/config: TestSchemaV1_acceptsUnexercisedSurface/relationship discriminator with an integer value`; `unit: cmd/sqlgen/config: TestSchemaV1_acceptsUnexercisedSurface/relationship discriminator with a boolean value` | all | |
| config.tables.relationships.sort.applied | An O2M or M2M `sort` orders the relationship loader when the caller passes no `Sorts` | `example: postgres: TestRelationshipStaticSort`; `unit: cmd/sqlgen/gen: TestClientTemplate_relationshipDefaultSort` | all | |
| config.tables.relationships.sort.omitted | With `sort` omitted, the loader applies no ordering | `unit: cmd/sqlgen/gen: TestClientTemplate_relationshipDefaultSort` | all | |
| config.tables.relationships.sort.direction.default | A `sort` entry with `direction` unset orders ascending | | all | config-fields: tables.relationships.sort.direction × default |
| config.tables.relationships.sort.direction.explicit | `direction: desc` orders that column descending | `example: postgres: TestRelationshipStaticSort` | all | config-fields: tables.relationships.sort.direction × explicit |
| config.tables.relationships.sort.direction.schema | `v1.json` declares `direction` as a string | `unit: cmd/sqlgen/config: TestSchemaV1_rejectsKnownMistakes/relationship sort entry with an unknown direction`; `unit: cmd/sqlgen/config: TestSchemaV1_acceptsUnexercisedSurface/per-table primary key and relationships` | all | config-fields: tables.relationships.sort.direction × schema |
| config.tables.relationships.description | A relationship's `description` is the Go doc comment on its field | | all | |

| ID | Requirement | Tests | Applies to | Matrix cell |
|---|---|---|---|---|
| config.tables.nested-mutations.needs-global | `tables.<t>.nested_mutations` has no effect unless `generation.nested_mutations.enabled` is true | `unit: cmd/sqlgen/gen: TestNestedEligibility_DisabledEmitsNothing` | all | |
| config.tables.nested-mutations.relationships.default | With `relationships` unset, every eligible edge is reachable | `unit: cmd/sqlgen/gen: TestNestedEligibility_EdgesAreAlphabetical` | all | config-fields: tables.nested-mutations.relationships × default |
| config.tables.nested-mutations.relationships.explicit | With `relationships` set, only the listed edges are reachable | `unit: cmd/sqlgen/gen: TestNestedEligibility_AllowlistNarrowsAndHardErrors/allowlist narrows the reachable set` | all | config-fields: tables.nested-mutations.relationships × explicit |
| config.tables.nested-mutations.relationships.schema | `v1.json` declares `relationships` as an array of `{name, allow_reparent}` entries with `name` required | `unit: cmd/sqlgen/config: TestSchemaV1_acceptsUnexercisedSurface/per-table nested mutation allowlist` | all | config-fields: tables.nested-mutations.relationships × schema |
| config.tables.nested-mutations.name-required | An allowlist entry without `name` is a config error | `unit: cmd/sqlgen/config: TestValidateNestedMutations_RequiredKeys/allowlist entry without name` | all | |
| config.tables.nested-mutations.allow-reparent.default | With `allow_reparent` unset, an O2M `connect` of a row already parented elsewhere is `ErrAlreadyRelated` | `example: graphql: TestCreateWithRelated_ConnectAttribution/a target parented elsewhere is ErrAlreadyRelated` | all | config-fields: tables.nested-mutations.allow-reparent × default |
| config.tables.nested-mutations.allow-reparent.explicit | `allow_reparent: true` lets the edge's `connect` adopt a row parented elsewhere | `unit: cmd/sqlgen/gen: TestNestedEligibility_AllowReparentNeedsNoNullGuard` | all | config-fields: tables.nested-mutations.allow-reparent × explicit |
| config.tables.nested-mutations.allow-reparent.schema | `v1.json` declares `allow_reparent` as a boolean | `unit: cmd/sqlgen/config: TestSchemaV1_acceptsUnexercisedSurface/per-table nested mutation allowlist` | all | config-fields: tables.nested-mutations.allow-reparent × schema |

## §4.9 ViewConfig (per-view)
<!-- prd: 4.9 sha256: 4ed72ed3ef173fbd -->

| ID | Requirement | Tests | Applies to | Matrix cell |
|---|---|---|---|---|
| config.views.struct-name.required | A view entry without `struct_name` is a config error | | all | |
| config.views.sql.required | A view entry without `sql` is a config error | | all | |
| config.views.invalidate-on.required-when-cached | A view with `cache.enabled: true` and an empty `invalidate_on` is a hard validation error | `unit: cmd/sqlgen/config: TestValidatePreParse_CacheConfig/view cache enabled without invalidate_on`; `unit: cmd/sqlgen/config: TestValidatePreParse_CacheConfig/view cache enabled false skips invalidate_on requirement` | all | |
| config.views.invalidate-on.resolves | Every `invalidate_on` name must resolve to a parsed table, bare or schema-qualified; an unknown name is a hard error | `unit: cmd/sqlgen/config: TestValidatePostParse_ViewInvalidateOn/unknown table name is hard error`; `unit: cmd/sqlgen/config: TestValidatePostParse_ViewInvalidateOn/invalidate_on resolves to parsed tables`; `unit: cmd/sqlgen/config: TestValidatePostParse_ViewInvalidateOn/invalidate_on resolves via schema-qualified name` | all | |
| config.views.invalidate-on.excluded | An `invalidate_on` name that resolves to a table excluded from generation is a hard error | `unit: cmd/sqlgen/config: TestValidatePostParse_ViewInvalidateOn/excluded table (all columns filtered) is hard error` | all | |
| config.views.matview.invalidate-on-required | A cached materialized view requires a non-empty `invalidate_on`, exactly as a view does | | postgres | |
| config.views.no-materialized-key | `ViewConfig` has no `materialized:` or `refresh:` key | | postgres | |
| config.views.overrides-types.view-inherits | A view's columns resolve under the global `overrides.types` binding (stated in §4.13's UUID-library rule, which cites §4.9) | | all | config-resolution: overrides.types × view-inherits |

| Matrix cell | Reason | Accepted |
|---|---|---|

## §4.10 ExtraType (custom structs)
<!-- prd: 4.10 sha256: 6f42acea8b9c8a67 -->

| ID | Requirement | Tests | Applies to | Matrix cell |
|---|---|---|---|---|
| config.extras.key-is-struct-name | An `extras` key is the generated Go struct's name | `unit: cmd/sqlgen/gen: TestBuildTypeContext_extras` | all | |
| config.extras.description | `extras.<T>.description` is the struct's Go doc comment | `unit: cmd/sqlgen/gen: TestBuildTypeContext_extras` | all | |
| config.extras.fields.key-is-field-name | A `fields` key is the Go field name | `unit: cmd/sqlgen/gen: TestBuildTypeContext_extras` | all | |
| config.extras.fields.description | `fields.<F>.description` is the field's Go doc comment | | all | |
| config.extras.fields.type | `fields.<F>.type` is the field's Go type | `unit: cmd/sqlgen/gen: TestTypesTemplate_extraTypeStruct` | all | |
| config.extras.fields.import | `fields.<F>.import` is added to the generated file's imports for the field's type | | all | |
| config.extras.fields.import-required | A package-qualified `fields.<F>.type` that sqlgen does not already know, with no `import`, is a config error; a builtin needs none | `unit: cmd/sqlgen/gen: TestTypeOverrideRejectsUnimportableLiteral/an extras field without an import`; `unit: cmd/sqlgen/gen: TestTypeOverrideAcceptsImportableLiteral/an extras field on a builtin`; `unit: cmd/sqlgen/gen: TestTypeOverrideImportsReportedTogether` | all | |
| config.extras.fields.tags | `fields.<F>.tags` become the field's struct tags, keyed by tag name | `unit: cmd/sqlgen/gen: TestTypesTemplate_extraTypeStruct` | all | |
| config.extras.fields.schema | `v1.json` declares `extras.<T>` with `description` and `fields.<F>.{description, type, import, tags}` | `unit: cmd/sqlgen/config: TestSchemaV1_acceptsUnexercisedSurface/extras with tags` | all | |

## §4.11 Enum Overrides
<!-- prd: 4.11 sha256: e373cdc551d8ff9d -->

| ID | Requirement | Tests | Applies to | Matrix cell |
|---|---|---|---|---|
| config.enums.key-bare-or-qualified | An `enums:` entry is keyed by SQL enum name, bare or schema-qualified, resolved as `tables:` keys are | `unit: cmd/sqlgen/config: TestLoadConfig_EnumsBlock`; `unit: cmd/sqlgen/gen: TestEnumConfig_descriptionOverridesSQLComment/schema-qualified key resolves` | all | |
| config.enums.struct-name.default | With `enums.<e>.struct_name` unset, the Go type name is the PascalCased SQL enum name | `unit: cmd/sqlgen/gen: TestBuildEnumContexts` | all | config-fields: enums.struct-name × default |
| config.enums.struct-name.explicit | An explicit `enums.<e>.struct_name` is the generated Go type name | `unit: cmd/sqlgen/config: TestLoadConfig_EnumsBlock`; `unit: cmd/sqlgen/gen: TestEnumStructNameOverride_resolvesCollision`; `unit: cmd/sqlgen/manifest: TestBuildEnums_namesFollowTheGeneratedGoType` | all | config-fields: enums.struct-name × explicit |
| config.enums.struct-name.schema | `v1.json` declares `enums.<e>.struct_name` as a string | `unit: cmd/sqlgen/config: TestSchemaV1_noDriftFromConfigStructs` | all | config-fields: enums.struct-name × schema |
| config.enums.description | `enums.<e>.description` is the generated type's doc comment, over the SQL `COMMENT ON TYPE` when both are present | `unit: cmd/sqlgen/gen: TestEnumConfig_descriptionOverridesSQLComment/override wins`; `unit: cmd/sqlgen/gen: TestEnumConfig_descriptionOverridesSQLComment/no override keeps the SQL comment` | all | |
| config.enums.struct-name.moves-constants | The override renames the enum's constants (`<Type><Value>`) | | all | |
| config.enums.struct-name.moves-all-var | The override renames the `All<Type>` var | | all | |
| config.enums.struct-name.moves-validate | The override renames the `Validate<Type>` function | | all | |
| config.enums.struct-name.moves-slice | The override renames the named slice type `<Type>Slice` | | postgres | |
| config.enums.struct-name.moves-graphql-enum | The GraphQL enum name is the overridden Go type name; there is no separate GraphQL alias | `unit: cmd/sqlgen/gen: TestEnumStructNameOverride_resolvesCollision`; `unit: cmd/sqlgen/manifest: TestBuildEnums_namesFollowTheGeneratedGoType` | all | |
| config.enums.struct-name.moves-gqlgen-binding | The override renames the gqlgen `models:` binding | | all | |
| config.enums.struct-name.moves-comparator | The override renames `<Type>Comparator` and its translator | `unit: cmd/sqlgen/gen: TestEnumStructNameOverride_resolvesCollision` | all | |
| config.enums.struct-name.moves-manifest | The override renames the manifest's published `go_type` | `unit: cmd/sqlgen/manifest: TestBuildEnums_namesFollowTheGeneratedGoType` | all | |
| config.enums.struct-name.keeps-sql-literals | The SQL literals stored in the column are unaffected by the override | | all | |
| config.enums.struct-name.keeps-wire-identifiers | The SCREAMING_SNAKE_CASE GraphQL value identifiers are unaffected by the override | | all | |

## §4.12 Full Example
<!-- prd: 4.12 sha256: d9a9df0b4e40e62c -->

| ID | Requirement | Tests | Applies to | Matrix cell |
|---|---|---|---|---|
| config.full-example.loads | The §4.12 example loads with `LoadConfig` | | all | |
| config.full-example.pre-parse-valid | The §4.12 example passes pre-parse validation | | all | |
| config.full-example.schema-valid | The §4.12 example validates against `cmd/sqlgen/config/schema/v1.json` | `unit: cmd/sqlgen/config: TestSchemaV1_acceptsPRDFullExample` | all | |

## PRD gaps

Behavior the code has, most of it pinned by tests, that no PRD text states. Each gap waits for
the user's ruling: amend the PRD, which turns the gap into requirement rows, or drop the pins.

| Gap | Behavior | Tests | Section | Ruling |
|---|---|---|---|---|
| gap.config.unknown-key-rejected | The loader decodes strictly: an unrecognized key anywhere (top level or nested) is a config error naming the key. §4.14 makes only the JSON Schema strict; §4/§4.13 state no loader rule | `unit: cmd/sqlgen/config: TestLoadConfig_RejectsUnknownKeys/the typo that caused the incident`; `unit: cmd/sqlgen/config: TestLoadConfig_RejectsUnknownKeys/unknown top-level block`; `unit: cmd/sqlgen/config: TestLoadConfig_RejectsUnknownKeys/unknown key nested under generation.manifest` | §4, §4.14 |  |
| gap.config.empty-document-loads | An empty `sqlgen.yml` loads as an all-defaults config rather than a parse error | `unit: cmd/sqlgen/config: TestLoadConfig_EmptyDocument` | §4 |  |
| gap.output.dir-omitted-warns | An omitted `output.dir` warns (`output.dir is not set — generating into the current working directory…`); an explicit `"."` does not. No §4.13 rule states it (the message also cites "PRD §4.2"; `output.dir` is §4.5) | `unit: cmd/sqlgen/config: TestValidatePreParse_OutputDirWarning/omitted, defaults to the working directory`; `unit: cmd/sqlgen/config: TestValidatePreParse_OutputDirWarning/explicitly the module root`; `unit: cmd/sqlgen/config: TestValidatePreParse_OutputDirWarning/explicit subdirectory` | §4.5, §4.13 |  |
| gap.output.package-name-normalized | The derived `output.package` is the directory's base name lowercased with `-` replaced by `_` (`./my-models` → `my_models`); §4.5 says only "derived from the directory name" | `unit: cmd/sqlgen/config: TestLoadConfig_OutputPackageDerivedFromDir/hyphenated directory` | §4.5 |  |
| gap.version.unvalidated | `version` is defaulted to `v1` but never read or checked: any string (`v2`, `banana`) loads and is ignored. §4.1 says sqlgen "interprets the file accordingly" without saying what an unknown version does |  | §4.1 |  |
| gap.exclude-tables.schema-qualified | An `exclude_tables` pattern is also matched against `schema.table`, so `audit.*` drops every table in schema `audit` | `unit: cmd/sqlgen/config: TestMatchExcludeTablePattern/schema-qualified`; `unit: cmd/sqlgen/gen: TestBuildTableContexts_ExcludeTables_SchemaQualified` | §4.4, §6.4 |  |
| gap.exclude-tables.match-syntax | Patterns go through `filepath.Match`, so `[...]` classes and `\` escapes also work, and a malformed pattern (e.g. `[`) silently matches nothing instead of erroring (`config.go:2373`) |  | §4.4 |  |
| gap.introspect.tables-glob | `input.introspect.tables` entries are glob patterns (`user*`), which §4.4 does not state | `integration: parser/introspect: TestTableFilteringWithWildcards` | §4.4 |  |
| gap.input.schema.introspection-default | A non-`*` `input.schema` is also passed to introspection as its default schema (`cmd/sqlgen/cli/connection.go:77`), while §4.2 scopes it to parsing SQL files |  | §4.2 |  |
| gap.connection.env-url-creates-block | With `SQLGEN_DB_URL` set and no `input.connection` block, the loader creates the block, which also satisfies `config.connection-missing` | `unit: cmd/sqlgen/config: TestLoadConfig_EnvVarSQLGEN_DB_URL_NoConnection`; `unit: cmd/sqlgen/config: TestLoadConfig_SQLGENDBURLSatisfiesConnectionFields/no connection block, env url set` | §4.3, §4.13 |  |
| gap.connection.env-empty-ignored | A `SQLGEN_DB_URL` or `SQLGEN_DB_PASSWORD` set to the empty string is treated as unset (`config.go:2058`, `:2072`) | `unit: cmd/sqlgen/config: TestValidatePreParse_IgnoredPasswordEnv/password unset` | §4.3 |  |
| gap.generation.exclude-deleted-key | `generation.exclude_deleted` (bool) is accepted by the loader and declared in the JSON Schema, and an explicit `false` turns off the default not-deleted filter. §4.6 lists no such field; §9.8 / §11 speak of `exclude_deleted` only as a state | `unit: cmd/sqlgen/gen: TestExcludeDeleted_respectsExplicitConfigOverride` | §4.6 |  |
| gap.generation.empty-list-means-default | An explicit empty `generation.cursor_keys: []`, `soft_delete_columns: []` or `update_columns: []` is replaced by the default list (config.go:1946, 1955, 1958: `len(...) == 0`), so soft-delete or update-column detection cannot be switched off with an empty list. §4.6 gives the empty list no meaning |  | §4.6 |  |
| gap.generation.unknown-subkeys-rejected | The loader decodes strictly: an unknown key under `generation.nested_mutations`, `tables.<t>.nested_mutations`, a relationship `discriminator`, or `generation.manifest` (`include_internal`) is a load error naming the key. §4.14 makes the JSON Schema strict; no PRD text makes the loader strict | `unit: cmd/sqlgen/config: TestLoadConfig_RejectsUnknownNestedMutationKeys`; `unit: cmd/sqlgen/config: TestLoadConfig_ManifestIncludeInternalRejected` | §4.6, §4.14 |  |
| gap.generation.closed-value-rejected | `ValidatePreParse` rejects a value outside its closed set, case-sensitively, with an allowed-set hint: `uuid_version: V7`, soft-delete `type: TIMESTAMP` (and `dialect`, `source`, `parse_mode`, `driver`, `layout`, `primary_key.strategy` / `uuid_version`); an empty value takes the default. §4.14 lists these closed sets for the schema only; §4.13 has no such rule | `unit: cmd/sqlgen/config: TestValidatePreParse_EnumValues` | §4.6, §4.13 |  |
| gap.generation.offset-nil | A `nil` `Offset` sends no `OFFSET`. §4.6 says of both `Limit` and `Offset` that "`nil` applies the `generation.query_limit` default", which cannot be meant for `Offset`. A negative `Limit` / `Offset` is also treated as no clause (get.go.tmpl `v > 0`), which §4.6 does not state |  | §4.6 |  |
| gap.generation.per-table-subset | Only six §4.6 fields can be set per table (`query_limit`, `batch_size`, `page_size`, `cursor_keys`, `exclude_columns`, `strict_updates`; plus `uuid_version` under `primary_key`); `tables.<t>.soft_delete_columns` or `.update_columns` is an unknown-key load error. §4.6 says "Every field in this section can be overridden per-table"; §4.8 lists the six and §4.6 Nested Mutations says it has no per-table toggle |  | §4.6, §4.8 |  |
| gap.tables.relationships.discriminator-o2o-backslash | A `discriminator.value` containing a backslash is a config error on an `o2o` edge (interpolated into the JOIN) and accepted on `o2m` / `m2m` (bound) | `unit: cmd/sqlgen/config: TestValidateDiscriminatorValue/backslash on an interpolated o2o edge`; `unit: cmd/sqlgen/config: TestValidateDiscriminatorValue/backslash on a bound o2m edge`; `unit: cmd/sqlgen/config: TestValidateDiscriminatorValue/backslash on a bound m2m edge` | §4.8, §13.4 |  |
| gap.tables.relationships.discriminator-empty-column-one-error | An empty `discriminator.column` reports only the required-key error, not also the column-existence error | `unit: cmd/sqlgen/config: TestValidateRelationshipDiscriminators_EmptyColumnDefersToRequired` | §4.8, §4.13 |  |
| gap.views.invalidate-on-unchecked-when-uncached | `invalidate_on` names are not resolved when the view's cache is off; §4.9 states the resolution rule unconditionally | `unit: cmd/sqlgen/config: TestValidatePostParse_ViewInvalidateOn/cache disabled skips invalidate_on validation` | §4.9 |  |
| gap.extras.key-pascal-cased | An `extras` key and its `fields` keys are PascalCased (`metadata` → `Metadata`, `key` → `Key`), though §4.10 says the key *is* the Go name | `unit: cmd/sqlgen/gen: TestBuildTypeContext_extras` | §4.10 |  |
| gap.tables.relationships.inferred-description | An inferred relationship with no `description` gets a generated doc comment (`one-to-one relationship with the companies table.`) | `unit: cmd/sqlgen/gen: TestBuildTableContexts_relationshipDescription` | §4.8 |  |

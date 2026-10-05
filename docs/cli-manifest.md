# `sqlgen manifest` — CLI Reference

When manifest emission is enabled (`generation.manifest.enabled: true`), sqlgen writes a
machine-readable `manifest_gen.json` describing the generated surface — entities, columns,
relationships, method signatures, and enabled features.

Two subcommands operate on that file. Both are purely on-disk and have no runtime or database
dependency, which makes them suitable for CI and PR-review workflows.

## `sqlgen manifest validate`

Validates a manifest against its JSON Schema.

```bash
sqlgen manifest validate ./db/manifest/manifest_gen.json
```

It loads the file, validates it against the schema referenced by its `$schema` URL (falling back
to the embedded schema when offline), and prints a one-line summary: `schema_version`, entity
count, enum count, and extra count. Both the `single` and `per_entity` on-disk layouts are
supported.

## `sqlgen manifest diff`

Diffs two manifests to review schema evolution — useful as a PR check that surfaces exactly what
a schema change did to the generated API.

```bash
sqlgen manifest diff ./old/manifest_gen.json ./new/manifest_gen.json
sqlgen manifest diff --json old.json new.json   # machine-readable
```

## Exit codes

| Command | 0 | 1 | 2 |
|---------|---|---|---|
| `manifest validate` | valid | schema / parse failure | I/O failure (missing / unreadable file) |
| `manifest diff` | no differences | differences found | I/O failure |

Because `diff` exits `1` on any difference, a CI job can gate on "the generated surface changed"
without parsing output.

## `--json` diff shape

All arrays are sorted for deterministic output.

```json
{
  "entities": {
    "added":   ["<table>"],
    "removed": ["<table>"],
    "changed": [
      {
        "table": "<table>",
        "columns":  { "added": ["<col>"], "removed": ["<col>"], "changed": [{ "key": "<col>", "old": "...", "new": "..." }] },
        "methods":  { "added": ["<method>"], "removed": ["<method>"], "changed": [{ "key": "<method>", "old": "<sig>", "new": "<sig>" }] },
        "features": { "added": ["<key>=<val>"], "removed": ["<key>=<val>"], "changed": [{ "key": "<feature.field>", "old": "...", "new": "..." }] }
      }
    ]
  },
  "sentinels":         { "added": ["<name>"], "removed": ["<name>"], "changed": [{ "key": "<name>", "old": "...", "new": "..." }] },
  "generation_config": [{ "key": "<toggle>", "old": "false", "new": "true" }]
}
```

## Enabling the manifest

```yaml
generation:
  manifest:
    enabled: true
    json_layout: single     # or: per_entity
```

See [Configuration](./configuration.md) for the surrounding `generation` block.

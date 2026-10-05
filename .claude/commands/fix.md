Create, list, or resolve fix items discovered during E2E testing.

## Instructions

The `/fix` command manages a structured fix tracker at `docs/tracker/fixes.md`. Fixes are bugs or issues discovered in earlier phases that surface during later phases (E2E testing, verification, auto-review, integration sweeps).

Each entry is an H3 section so the body has room for the four required parts — Issue / Root cause / Suggested fix / References. Single-line entries are no longer accepted in Add mode; they read poorly and force readers to mentally pull the structure out of paragraph soup.

Parse `$ARGUMENTS` to determine the mode:

- `add (phase N, module) [severity] — Title\n<body>` → **Add mode**
- `list` → **List mode**
- `list --resolved` → **List mode** (resolved entries only)
- `resolve FIX-NNN — notes` → **Resolve mode**

**Severity values** (closed set):

| Severity | Meaning | Resolution gate |
|---|---|---|
| `blocking` | Surfaced during `/verify` as FAIL, or surfaced during `/implement` as a hard regression. Must be resolved before `/done` on the originating sub-item. | Pre-`/done` |
| `tracked` | Default. Surfaced as PARTIAL / UNTESTED / MISSING test, or via `/fix add` without severity. Must be resolved before phase closure (`/close-phase`). | Pre-`/close-phase` |
| `nit` | Cosmetic, style, or future-improvement carry-over. Can survive phase closure with explicit defer in the closure summary. | None — carried forward |

---

## Entry Format

Every fix is an H3 section. The five-line header is metadata; the four labeled paragraphs are the body.

```markdown
### FIX-NNN

- **Status:** Open
- **Severity:** tracked
- **Origin:** phase N, module/path
- **Surfaced by:** _optional — sub-item ID / verify pass / auto-review / integration sweep / user report_
- **Vetted:** _optional, written only by `/fix-vet` — `ready` | `corrected` | `needs ruling` | `not reproducible` at `<sha>` (<date>)_

**Issue.** One paragraph stating what's wrong. Cite file:line where the
issue lives. Distinguish the symptom (what someone observes) from the
root cause (next section).

**Root cause.** Why the issue exists at all — usually a specific line
in a specific file. This is the part that forces the author to actually
understand the bug rather than just describe the symptom. Required for
`blocking` and `tracked`; for `nit` you can write `_n/a — cosmetic._`.

**Suggested fix.**
- Prescriptive bullets: the specific edits to make.
- File:line references when possible.
- Include the regression test(s) to add.
- For `nit` entries this can be `_carry forward; no action this phase._`.

**References.**
- PRD section(s) the fix is anchored to.
- Related FIX IDs (e.g., `FIX-098` — multi-column filter qualification).
- Existing code that demonstrates the pattern, similar prior fixes.
- Anything reviewers may want to read alongside the entry.

**Resolution notes.** _Filled in by `/fix resolve`._
```

### Format rules

- The four bold-labeled paragraphs (`**Issue.**`, `**Root cause.**`, `**Suggested fix.**`, `**References.**`) are all required and must appear in that order.
- `**Resolution notes.**` is required in the template but starts as a placeholder; `/fix resolve` rewrites it.
- The metadata lines `- **Status:**`, `- **Severity:**` and `- **Origin:**` are required. `- **Surfaced by:**` is optional and may be omitted from the bullet list entirely when there's no useful provenance. `- **Vetted:**` is written only by `/fix-vet` and is never added by `/fix add`. `/fix-implement` reads it (Step 3) to decide whether to start from the saved prototype in `.claude/prototypes/FIX-NNN.patch`.
- Use ``backticks`` for file paths, function names, identifiers. Use file:line refs (`parser/schema.go:172-181`) where they help — they're the most-cited shape in this tracker.
- Title (the part after `—` in the add command) becomes the first sentence inside `**Issue.**` if no separate title is provided; otherwise it's bolded as the first sentence of Issue. Keep titles ≤ 100 chars.

---

### Locating the sections

`docs/tracker/fixes.md` is ~13k lines, and entry prose quotes the strings `## Open` and
`## Resolved` in several places. **A substring search for a section heading can therefore land
inside an entry instead of on the heading**, and that has already gone wrong once: an insertion
made "after `## Open`" consumed the `## Resolved` heading, and forty-plus resolved entries ended
up under `## Open`, where the next reader took them for open work.

The file carries four marker lines that exist only for this purpose:

```
## Open
<!-- fixes:open:begin -->      <- entries go between these two
<!-- fixes:open:end -->
## Resolved
<!-- fixes:resolved:begin -->  <- and between these two
<!-- fixes:resolved:end -->
```

Rules:

- **Insert relative to a marker, never a heading.** `/fix add` inserts before
  `<!-- fixes:open:end -->`; `/fix resolve` inserts after `<!-- fixes:resolved:begin -->`. Both
  points are bounded on the far side, so an insertion cannot swallow the section below it.
- **Match markers as whole lines**, not substrings — and expect exactly one of each.
- **Never rewrite a marker line.** If one is missing, the file is already damaged; stop and say so
  rather than inserting into a file whose boundaries are unknown.
- **Run `make tracker-check` after writing.** It fails on a missing or duplicated marker, an entry
  outside both regions, a duplicate ID, and — the one that catches the incident above — any entry
  whose `**Status:**` disagrees with the section it sits in.

---

### Add Mode

**Triggered by:** `/fix add (phase N, module) [severity] — title-and-body`

Args after `—` are parsed in one of two shapes:

**Multi-line structured form** (preferred; required for callers like `/verify`):

```
/fix add (phase 18, cmd/sqlgen/manifest) [tracked] — Parser does not expose index method / partial WHERE
Issue. Parser's `parser.Constraint` does not carry index `method`
(btree / gin / gist / hash) or partial `WHERE` clause. The manifest
builder hardcodes `method: "btree"` and omits `where`, so PRD §30.7's
full index surface is not represented.
Root cause. `parser/schema.go:172-181` defines `Constraint` with `Name`,
`Type`, `Columns`, `Reference{...}`, `CheckExpression` — no `Method` or
`Where`. Dialect parsers discard both fields at parse time.
Suggested fix.
- Extend `parser.Constraint` with `Method string` + `Where string`.
- Postgres: `parser/postgres/index.go` — `pg_index.indpred` + `pg_am.amname`.
- MySQL: `information_schema.STATISTICS.INDEX_TYPE`.
- SQLite: rqlite/sql AST for `CREATE INDEX ... WHERE ...`.
- Update `cmd/sqlgen/manifest/builder.go:546-571`.
- Add `manifest/builder_test.go` cases: partial UNIQUE + GIN.
References.
- PRD §30.7 — "indexes[] | top-level | ... `method`, optional `where`."
- `parser/introspect/postgres.go:181` (existing introspection example).
Surfaced by. 18.2 auto-review (2026-05-16)
```

The parser recognizes case-insensitive labels `Issue.`, `Root cause.`, `Suggested fix.` / `Fix.`, `References.` / `Refs.`, `Surfaced by.` at the start of a line and routes the following text into that section until the next label. Bullet items under `Suggested fix.` / `References.` are preserved as-is.

**Single-line legacy form** (`add (phase N, module) [severity] — short description`): the description becomes the **Issue** body; the other three required sections are inserted as `_TBD — fill in before /fix resolve._` placeholders. The skill emits a warning (`fix entered with placeholder sections; populate before resolving`). Use only when the caller genuinely doesn't have the structured detail yet — never from `/verify`.

#### Procedure

1. Read `docs/tracker/fixes.md`.
2. Find the highest existing FIX-NNN ID (scan both Open and Resolved sections). If none, start at FIX-001.
3. Assign the next sequential ID.
4. Parse severity: if a `[blocking]` / `[tracked]` / `[nit]` token appears between the parens and the `—`, use it; otherwise default to `tracked`. Reject any other bracketed token with a clear error citing the closed set.
5. Parse the body into the four required sections + optional Surfaced-by metadata. If a required section is missing AND the call used the legacy single-line form, fill with the `_TBD_` placeholder. If the multi-line form was used and a section is missing, refuse — show the entry that would have been emitted, list which sections are missing, and ask the user to re-issue with all four.
6. Build the H3 entry per the **Entry Format** template above. Wrap body paragraphs at ~80 columns where natural; preserve bullet lists verbatim.
7. Append to the **Open** section in ascending-ID order (which equals insertion order, since IDs grow monotonically) — insert immediately **before the `<!-- fixes:open:end -->` line**, never after the `## Open` heading. See **Locating the sections** below. If the Open section contains `_No open fixes._`, remove that placeholder first.
8. Write the updated file.
9. Run `make tracker-check`. If it fails, the edit broke the file's structure — fix it before reporting success.
10. Report the assigned ID, severity, and any `_TBD_` placeholders the user must populate before `/fix resolve`.

---

### List Mode

**Triggered by:** `/fix list` or `/fix list --resolved`

1. Read `docs/tracker/fixes.md`.
2. Scan H3 headers (`^### FIX-`) inside the Open section (or Resolved section with `--resolved`). For each header, parse the metadata bullets (Status / Severity / Origin / Surfaced by / Vetted) and the first sentence of the Issue paragraph (the title).
3. Display a table grouped by severity (blocking first, then tracked, then nit):

```
| ID | Severity | Origin | Surfaced by | Vetted | Title |
|----|----------|--------|-------------|--------|-------|
| FIX-107 | tracked | phase 18, cmd/sqlgen/manifest | 18.2 auto-review | ready at `abc1234` | Parser does not expose index method / partial WHERE |
```

Pre-existing entries written in the legacy single-line bullet format (any FIX-NNN whose entry does not start with `### `) display in a separate table footnoted `legacy bullet format — re-author on next touch` with whatever metadata is parseable.

4. Show counts: `N blocking, M tracked, K nit (X open total, Y resolved)`.

---

### Resolve Mode

**Triggered by:** `/fix resolve FIX-001` or `/fix resolve FIX-001 — fixed in abc1234`

1. Read `docs/tracker/fixes.md`.
2. Find the entry matching the given FIX-NNN ID. If it's in **Resolved** already, report that. If it's not found at all, report the error and show open fixes for reference.
3. Determine the resolution note: explicit text after `—`, or — if no text — the most recent commit hash + a one-line title pulled from the originating fix's Issue.
4. Inside the entry, rewrite the Status line from `**Status:** Open` to `**Status:** Resolved`.
5. Replace the `**Resolution notes.** _Filled in by /fix resolve._` placeholder with `**Resolution notes.** <note text>`. Preserve everything else in the entry verbatim.
6. Move the entry from **Open** to the top of the **Resolved** section — insert immediately **after the `<!-- fixes:resolved:begin -->` line**. Resolved is ordered **most-recently-resolved first**, which is *not* descending ID: an older entry resolved today goes above a higher-numbered one resolved last week. See **Locating the sections** below.
7. If the Open section is now empty, re-add the `_No open fixes._` placeholder. If the Resolved section had `_No resolved fixes yet._`, remove it.
8. Write the updated file.
9. Run `make tracker-check`. It verifies the markers, the nesting, and that every entry's `**Status:**` agrees with the section it sits in — the last of which is exactly what a mis-placed move breaks. If it fails, fix the structure before reporting success.
10. Report the resolution to the user, including the resolution-note text that was recorded.

---

### Batch Add Mode (used by /verify integration)

Callers like `/verify` produce multiple entries per run. Each entry must use the multi-line structured form (legacy single-line is rejected in batch). The caller sends one `/fix add` per entry:

```
/fix add (phase N, module) [severity] — title1
Issue. ...
Root cause. ...
Suggested fix. ...
References. ...
Surfaced by. /verify {sub-item ID} ({date})
```

`[severity]` is required in batch mode (the caller derives it from the verification verdict: FAIL → `blocking`, PARTIAL → `tracked`, MISSING test → `tracked`, NIT → `nit`). Process each one following the Add Mode steps. Report all assigned IDs + severities at the end.

---

## Findings triage

Every finding outside the scope of the entry being worked on (an **adjacent finding**, from an implementer, a vetter, a verifier or a reviewer) goes to exactly one of three buckets. This is the anti-waterfall rule: one fix must not spawn several entries. Two `/fix-loop` runs in 2026-09/10 committed 11 fixes and filed 8 new entries plus 10 unfiled findings, so the queue barely moved.

1. **Inline.** Fix it in the current change and name it in the resolution notes (or, during `/fix-vet`, in the prototype or as a stale fact). This covers doc and comment corrections, dead code, stale line refs and tracker text, and a defect with the **same root cause at the same fix site**.
2. **Candidate FIX.** A **measured**, user-visible failure: generated code that does not compile, a wrong query result or wrong data, a runtime error or a crash, with the exact command and output. It still needs the user's yes before `/fix add`. During a **queue freeze** (below), it goes to the backlog tagged `[measured]` instead.
3. **Backlog.** Everything else: code-reading suspicions, coverage gaps, cosmetic issues, design musings, anything unmeasured. Append one line to `docs/tracker/backlog.md` (format below). No vetting, no rulings, no entry. `/close-phase` reviews the backlog.

**Scope cap.** A vet or an implementation may widen its entry only when the extra case has the same root cause **and** the same fix site. Anything else is an adjacent finding.

**Backlog line format** (`make tracker-check` enforces it, between the `<!-- backlog:begin -->` and `<!-- backlog:end -->` markers):

```
- [YYYY-MM-DD] [measured|reading] (area) One-sentence finding — evidence (from FIX-NNN | N.X | <source>)
```

`area` is a short module tag such as `gen`, `config`, `parser/mysql`, `api`, `cache` or `docs`. `[measured]` needs a quoted command result in the evidence; otherwise use `[reading]`.

**Queue freeze.** When `docs/tracker/backlog.md`'s header says `Queue freeze: on`, no new FIX entry is proposed from adjacent findings: candidate FIXes go to the backlog tagged `[measured]`. `/fix add` typed by the user and `/verify`'s sub-item logging are unaffected. The user lifts the freeze by editing that line.

**Auto-accepting a recommended option.** A vet or implementation question does not need the user when the recommended option meets **all** of the following:
- the vet or implementer prototyped it and its gates are green;
- choosing it causes no golden churn compared with the alternative;
- it adds or removes no public surface: no exported Go identifier in the runtime or generated code, no config key, no GraphQL field or argument;
- it reverses no earlier user ruling and no behavior the PRD states (adding PRD text for an unspecified case, or correcting a doc to match the code, is fine).

Apply it, and record under Suggested fix: "Auto-accepted the recommended option on <date> (`/fix` auto-accept rule): <question> → <option>, because <which conditions held>." List every auto-accept in the run's final report so the user can veto it. Any doubt about a condition means ask.

---

### Notes on existing entries

The repo's `docs/tracker/fixes.md` carries ~106 legacy entries written in the old single-line bullet format. These remain valid — don't re-author them speculatively. When you touch one (resolve it, refer to it, file a follow-up that links it), opportunistically upgrade it to the H3 format in the same edit so the tracker drifts toward structure naturally.

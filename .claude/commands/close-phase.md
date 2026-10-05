Run the closure sweep for an implementation phase and update all trackers.

## Instructions

The user wants to close Phase `$ARGUMENTS` (e.g., `18`, `17`, `16`).

This skill walks the same recipe that `/done` followed by hand on phases 14, 15, 16.8g, 17.4 — `make check` + `make check-examples` + `make test-integration` sweep under `-race`, FIX triage by severity, sibling design-doc status flip, schema freeze where applicable, STATUS.md row update, Current Focus rewrite. The recipe has been executed ~10 times by hand; each one drifts a little. This skill compresses 30+ minutes of bookkeeping per phase close and prevents the "did I update the Current Focus?" misses.

### Step 0: Parse arguments

Split `$ARGUMENTS` on whitespace. First token is the phase number `N` (e.g., `18`). Recognize these flags anywhere in the remaining tokens:

- `--dry-run` — execute Steps 1–4 (verification) but skip Steps 5–8 (mutations). Reports what would change without writing.
- `--skip-integration` — skip `make test-integration` in Step 3. Use only when integration tests have already been run and verified in this session. Note in the closure record that it was skipped + the reference run.

### Step 1: Verify all sub-items are Complete

Read `docs/tracker/phase-N.md`. For each sub-item section (e.g., `## N.1`, `## N.2`, ...):

- Confirm `**Status:** Complete`
- Confirm all `- [ ]` task checkboxes are `- [x]`
- Confirm the **Completion Record** is filled in (not the placeholder `_Filled in when complete: ..._`)

If any sub-item is incomplete, **stop and report** which ones — closure cannot proceed. Suggest the user run `/done` or `/verify` on the open sub-items first.

### Step 2: Triage open FIX entries by severity

Read `docs/tracker/fixes.md`. Enumerate the Open section by scanning between the `<!-- fixes:open:begin -->` and `<!-- fixes:open:end -->` marker lines, not by searching for the `## Open` heading — entry prose quotes that heading and a substring match lands inside an entry (see `/fix` → **Locating the sections**). List all **Open** entries — both the H3-structured form (`### FIX-NNN` with `**Severity:** ...` metadata line) and the legacy single-line bullet form (`- [ ] **FIX-NNN** (...) [severity]`). For mixed shapes, parse whichever applies per entry; see `/fix` for the canonical formats.

For each open FIX, parse the severity tag — from the `**Severity:**` metadata line in structured entries, or the `[blocking]` / `[tracked]` / `[nit]` token in legacy bullets (default `tracked` for legacy entries without a tag).

**Closure gate per severity** (per `/fix.md`):

| Severity | Resolution required for phase close? |
|---|---|
| `blocking` | Yes — must be resolved (or explicitly reclassified) before close |
| `tracked` | Yes if the origin phase is the closing phase or earlier; carry-forward only with explicit user OK |
| `nit` | No — carry forward by default |

For each open entry, classify as:

- **Must-resolve**: `blocking` originating in any phase ≤ N, or `tracked` originating in phase N.
- **Carry-forward candidate**: `tracked` originating in phase < N (already deferred once), or any `nit`.
- **Out-of-scope**: `blocking` or `tracked` originating in phase > N (impossible if STATUS.md is consistent — flag as a bug).

Report the classification table. If any **Must-resolve** entries exist, stop and ask the user how to proceed (resolve them inline via `/fix-implement`, reclassify with a defended rationale, or abort the closure).

For **Carry-forward candidates**, ask the user to confirm carry-forward (one-line rationale per entry recorded in the closure record).

### Step 2b: Review the findings backlog

Read the lines between `<!-- backlog:begin -->` and `<!-- backlog:end -->` in `docs/tracker/backlog.md` (see `/fix` → **Findings triage**). Group them by `(area)`, `[measured]` first. Present them in one AskUserQuestion batch per group, at most four questions per call. For each line the options are: promote it to `/fix add` (the user's yes is the filing approval), keep it, or delete it. Default to keep, so that silence changes nothing. Apply the answers, run `make tracker-check`, and record in the closure report how many items were promoted, kept and deleted. The backlog never blocks closure.

If the header says `Queue freeze: on` and the open FIX queue is empty, ask whether to lift the freeze.

### Step 3: Run the full test sweep

Run sequentially (each must complete before the next; capture timings):

```bash
make check
make check-examples
make test-integration  # skipped if --skip-integration
```

Record for each:
- Exit status (0 = clean)
- Wall time (longest leg under each invocation if known)
- Any flake patterns observed

If any leg fails, stop and report. Suggest re-running the failing leg in isolation to confirm flake vs. real regression. Real regressions must be `/fix add (phase N, module) [blocking] — ...` and resolved before closure resumes.

If `--dry-run`, stop here. Report the verification + sweep summary; do not write any files.

### Step 4: Confirm sibling design-doc status (if applicable)

Some phases have a sibling design document under `docs/design/` (e.g., Phase 12 → `docs/design/CACHE.md`, Phase 13 → `docs/design/TENANCY.md`, Phase 16 → `docs/design/archive/GRAPHQL.md`, Phase 17 implicit via PRD §13.7, Phase 18 → `docs/design/MANIFEST.md`).

If a sibling exists:
- Verify its top-of-file Status header reflects `SYNCED — superseded by PRD §X` (or equivalent project convention) with the closure date.
- If not, ask the user whether to flip it now or defer. If flip-now, update the Status line with today's date.

If no sibling exists, skip.

### Step 5: Freeze schemas / artifacts (if specified by the phase)

Some phases include a "freeze X" or "tag Y" task in their final sub-item's spec (e.g., Phase 18.10 freezes manifest JSON Schema v1.0.0; future phases may freeze other versioned contracts).

Read the final sub-item's spec text. If it includes a freeze / tag / version-bump task, execute it now:

- For JSON Schema: confirm the `schema_version` field in the schema file is bumped to the frozen version; confirm the schema file path matches the final-sub-item spec.
- For other contracts: follow the per-phase spec exactly.

If the final sub-item has no freeze task, skip.

### Step 6: Update `docs/tracker/STATUS.md`

Edit the Overview table row for phase N:
- Set `Complete` column to match `Sub-Items` column (all done).
- Set `Status` column to `Complete` (plus any closure-date qualifier if convention exists in adjacent rows).

Prepend a new Current Focus entry (newest entries at the top). Use this skeleton, filled in from Steps 1–5:

```markdown
**Phase N (<Name>) closed YYYY-MM-DD.** All <K> sub-items complete: <one-line summary per sub-item, semicolon-separated>.

**N.<final> closure sweep (landed YYYY-MM-DD):** `make check` clean across all <M> modules under `-race` (<longest-leg> ~<time>s); `make check-examples` clean across all <M> example modules (<longest-leg> ~<time>s); `make test-integration` clean (longest legs <legs> ~<time>s); <flake observations or "no flakes">. **FIX triage:** <count> resolved inline during the phase; <count> carried forward (<list with one-line rationale per entry>). **Surface delta summary (across Phase N):** <2–4 sentence narrative of the user-visible / API-visible / build-visible change set>. **Next:** <follow-on phase or scheduling note>.
```

The narrative section ("Surface delta summary") is the load-bearing prose — pull from the Completion Records of all sub-items rather than re-deriving. The pattern across 14.x / 15.x / 16.8g / 17.4 is: 2–4 sentences naming the new types / config blocks / commands / templates / examples added, followed by `Next:`.

### Step 7: Update `docs/tracker/IMPLEMENTATION_ORDER.md` (if applicable)

If the phase entry in `IMPLEMENTATION_ORDER.md` carries a "Status: in progress" header or similar marker, flip it to `Status: Complete (closed YYYY-MM-DD)`. Most phases don't have this — skip if absent.

### Step 8: Report

Output a structured closure report:

```
## Phase N closure: <Name>

**Sub-items:** K/K complete
**Tests:** check / check-examples / test-integration all clean under -race
**FIX triage:** A resolved inline, B carried forward, C must-resolve resolved during closure
**Sibling design doc:** <SYNCED YYYY-MM-DD | none>
**Frozen artifacts:** <list, or "none">

### Surface delta
<2–4 sentence narrative — what shipped>

### Carry-forward FIXes
| ID | Severity | Origin | Description | Rationale |
|----|----------|--------|-------------|-----------|
| FIX-NNN | tracked | phase N-2, module | description | one-line user-supplied rationale |

### Trackers updated
- docs/tracker/STATUS.md — phase N row + new Current Focus entry
- docs/tracker/phase-N.md — _no changes (already complete)_
- docs/tracker/IMPLEMENTATION_ORDER.md — <changed | unchanged>
- docs/design/<design>.md — Status header flipped to SYNCED <YYYY-MM-DD> (if applicable)

### Sweep timings
- make check: <time>s (longest leg: <pkg> ~<time>s)
- make check-examples: <time>s (longest leg: <pkg> ~<time>s)
- make test-integration: <time>s (longest legs: <legs>)

### Next
<Suggested next action — usually the next phase number, or a scheduling note if multiple in-progress phases remain.>
```

### Rules

- **Never auto-resolve FIX entries** during closure. The classification step asks the user; resolution happens via `/fix-implement` or explicit `/fix resolve` outside this skill.
- **Never skip a failing test leg.** A flake suspicion is not a pass — re-run in isolation, or `/fix add (phase N, module) [blocking] — flake pattern X in Y` and resolve before resuming.
- **Never edit the sub-item Completion Records** at closure time. Those should already be filled in from `/done`; closure synthesizes from them.
- **Never advance the phase counter in STATUS.md without confirming the sub-item count matches.** Mismatched counts are a sign of an in-flight sub-item split (e.g., 16.8 → 16.8a–g) — handle explicitly.
- **`--dry-run` writes nothing.** Useful before the integration sweep to confirm the closure pre-conditions hold without committing.

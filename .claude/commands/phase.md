Break down an implementation phase into trackable tasks.

## Instructions

The user wants to break down Phase $ARGUMENTS from `docs/tracker/IMPLEMENTATION_ORDER.md` into a structured task file.

### Step 1: Read the phase

Read the specified phase section from `docs/tracker/IMPLEMENTATION_ORDER.md`. Identify:
- All sub-items (e.g., 0.1, 0.2, 0.3)
- What to build for each sub-item
- Dependencies between sub-items
- PRD section references

### Step 2: Read the PRD sections

For each PRD section referenced by the phase, read the relevant content from `docs/PRD.md`. Extract:
- Specific requirements and behavior that define "done"
- Edge cases and validation rules
- Expected inputs/outputs
- Test requirements

### Step 3: Generate the phase task file

Write a task file to `docs/tracker/phase-{N}.md` using this format:

```markdown
# Phase {N}: {Name}

Status: Not Started
PRD Sections: {comma-separated list}

## {N}.{X} {component} — {description}

**PRD Reference:** Section {N}

**Status:** Not Started

### Tasks

- [ ] {specific implementable task}
- [ ] {specific implementable task}
- [ ] ...

### Acceptance Criteria

- {measurable criterion from PRD — what must be true when done}
- {measurable criterion from PRD}
- ...

### Tests Required

- [ ] {specific test case from PRD or IMPLEMENTATION_ORDER}
- [ ] {specific test case}
- ...

### Completion Record

_Filled in when complete: files changed, commit references, notes._
```

Rules for task breakdown:
- Each task should be a single, concrete unit of work (a function, a type, a method, a test suite)
- Tasks within a sub-item should be ordered by dependency
- Acceptance criteria come from the PRD — they are measurable, not vague
- Tests required come from both the PRD and IMPLEMENTATION_ORDER.md
- Do NOT include tasks for things outside this phase's scope

### Step 4: Update STATUS.md

Read `docs/tracker/STATUS.md` and add or update the entry for this phase. Each phase gets one line showing its status and sub-item count.

### Step 5: Report

Tell the user what was created and give a brief summary of the task count.

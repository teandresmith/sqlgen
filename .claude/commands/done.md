Mark a task or sub-item as complete and update the tracker.

## Instructions

The user wants to mark work as complete. The argument is a sub-item identifier (e.g., `0.1`, `2.3`, `6.4a`).

### Step 1: Find the task

Read the relevant phase file from `docs/tracker/` (e.g., `phase-0.md` for sub-item `0.1`). Locate the sub-item section.

### Step 2: Verify completion

Check that all tasks (checkboxes) in the sub-item are checked. If any are unchecked, ask the user which tasks are actually done and update accordingly.

### Step 3: Update the phase file

- Check off completed tasks
- Update the sub-item's `**Status:**` to `Complete`
- Fill in the Completion Record with:
  - Files changed (from git diff or user input)
  - Date completed
  - Any notes

### Step 4: Update STATUS.md

Read `docs/tracker/STATUS.md` and update the counts for this phase. If all sub-items in the phase are complete, mark the phase as complete.

### Step 5: Report

Confirm what was marked complete and show the next actionable item.

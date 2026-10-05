MODULES = . parser cmd/sqlgen event/natsbus cache/memory cache/redis cache/msgpack metrics/otel
EXAMPLE_MODULES = $(wildcard cmd/sqlgen/testdata/examples/*/go.mod)
EXAMPLE_DIRS = $(dir $(EXAMPLE_MODULES))

# Timeout for the slow suites. Deliberately generous: a test timeout exists to
# catch a hang, not to bound a normal run. It was sized when `cmd/sqlgen/cli`
# alone took 7-10 minutes under -race — goimports re-scanned the module cache
# once per generated file, so suite time tracked GOMODCACHE size rather than the
# work done. That re-scan is gone; the whole `cmd/sqlgen` module now runs in
# well under two minutes under -race. The bound stays high because it is a hang
# detector, not a budget. The example suites keep their own tighter bound.
TEST_TIMEOUT ?= 30m

.PHONY: refs-check test test-integration test-examples lint lint-examples tools-check check check-all fmt vet update-golden update-golden-e2e coverage coverage-html clean help

## Testing

test: ## Run unit tests across all modules (skips integration tests)
	@for mod in $(MODULES); do \
		echo "==> test $$mod"; \
		(cd $$mod && go test -short -race -count=1 -timeout=$(TEST_TIMEOUT) ./...) || exit 1; \
	done

test-integration: ## Run full test suite including integration tests (requires Docker)
	@for mod in $(MODULES); do \
		echo "==> test-integration $$mod"; \
		(cd $$mod && go test -race -count=1 -timeout=$(TEST_TIMEOUT) ./...) || exit 1; \
	done

test-examples: ## Run E2E example tests against real databases (requires Docker)
	@for dir in $(EXAMPLE_DIRS); do \
		echo "==> test-examples $$dir"; \
		(cd $$dir && GOWORK=off go test -race -count=1 -timeout=5m ./...) || exit 1; \
	done

## Code Quality

lint: ## Run golangci-lint across all modules
	@for mod in $(MODULES); do \
		echo "==> lint $$mod"; \
		(cd $$mod && golangci-lint run --timeout=5m) || exit 1; \
	done

lint-examples: ## Run golangci-lint on E2E example modules
	@for dir in $(EXAMPLE_DIRS); do \
		echo "==> lint-examples $$dir"; \
		(cd $$dir && GOWORK=off golangci-lint run --timeout=5m) || exit 1; \
	done

fmt: ## Run gofumpt across all modules
	@for mod in $(MODULES); do \
		echo "==> fmt $$mod"; \
		(cd $$mod && gofumpt -w .) || exit 1; \
	done

fmt-examples: ## Run gofumpt fmt on handwritten files in E2E example modules (skips generator output under expected/, models/, and graph/)
	@for dir in $(EXAMPLE_DIRS); do \
		echo "==> fmt-examples $$dir"; \
		find $$dir -type d \( -name expected -o -name models -o -name graph \) -prune -o -type f -name '*.go' -print0 | xargs -0 -r gofumpt -w || exit 1; \
	done

vet: ## Run go vet across all modules
	@for mod in $(MODULES); do \
		echo "==> vet $$mod"; \
		(cd $$mod && go vet ./...) || exit 1; \
	done

vet-examples: ## Run go vet on E2E example modules
	@for dir in $(EXAMPLE_DIRS); do \
		echo "==> vet-examples $$dir"; \
		(cd $$dir && GOWORK=off go vet ./...) || exit 1; \
	done

fix: # Run go fix across all modules
	@for mod in $(MODULES); do \
		echo "==> fix $$mod"; \
		(cd $$mod && go fix ./...) || exit 1; \
	done

fix-examples: # Run go fix across all E2E example modules
	@for dir in $(EXAMPLE_DIRS); do \
		echo "==> fix-examples $$dir"; \
		(cd $$dir && GOWORK=off go fix ./...) || exit 1; \
	done





tools-check: ## Warn if installed tool versions drift from the .tool-versions pin
	@warn=0; \
	for t in golang golangci-lint gofumpt; do \
		want=$$(sed -n "s/^$$t[[:space:]]\{1,\}//p" .tool-versions | tr -d '[:space:]'); \
		case $$t in \
			golang)        got=$$(go version 2>/dev/null | awk '{print $$3}' | sed 's/^go//');; \
			golangci-lint) got=$$(golangci-lint version --short 2>/dev/null);; \
			gofumpt)       got=$$(gofumpt --version 2>/dev/null | awk '{print $$1}' | sed 's/^v//');; \
		esac; \
		if [ -z "$$got" ]; then got="MISSING"; fi; \
		if [ "$$want" = "$$got" ]; then printf '  ok   %-14s %s\n' "$$t" "$$got"; \
		else printf '  WARN %-14s pinned %-10s installed %s\n' "$$t" "$$want" "$$got"; warn=1; fi; \
	done; \
	if [ "$$warn" = 1 ]; then echo "  drift is a warning, not a failure - a mismatched gofumpt reformats files differently"; fi
	@floor=$$(sed -n 's/^go[[:space:]]\{1,\}//p' go.mod | head -1); \
	built=$$(golangci-lint --version 2>/dev/null | sed -n 's/.*built with go\([0-9][0-9.]*\).*/\1/p'); \
	if [ -z "$$built" ]; then \
		printf '  WARN %-14s golangci-lint not installed - cannot check it against the go.mod floor %s\n' "build-go" "$$floor"; \
	elif [ "$$(printf '%s\n%s\n' "$$floor" "$$built" | sort -V | head -1)" != "$$floor" ]; then \
		printf '  WARN %-14s built with go%s, below the go.mod floor %s\n' "golangci-lint" "$$built" "$$floor"; \
		echo "       it will refuse to run at all - bump .tool-versions"; \
	else \
		printf '  ok   %-14s built with go%s, go.mod floor %s\n' "build-go" "$$built" "$$floor"; \
	fi

TRACKER ?= docs/tracker/fixes.md
BACKLOG ?= docs/tracker/backlog.md

tracker-check: ## Verify fixes.md's Open/Resolved structure and backlog.md's line format
	@awk '\
	/^<!-- fixes:open:begin -->$$/     { m["open:begin"]++;     region="Open";     next } \
	/^<!-- fixes:open:end -->$$/       { m["open:end"]++;       region="";         next } \
	/^<!-- fixes:resolved:begin -->$$/ { m["resolved:begin"]++; region="Resolved"; next } \
	/^<!-- fixes:resolved:end -->$$/   { m["resolved:end"]++;   region="";         next } \
	/^### FIX-/ { \
		id=$$2; \
		if (region == "") { printf "  FAIL  %s (line %d) sits outside both marker regions\n", id, NR; bad=1 } \
		if (id in seen)   { printf "  FAIL  duplicate entry %s (line %d, first at %d)\n", id, NR, seen[id]; bad=1 } \
		seen[id]=NR; cur=id; curregion=region; n[region]++; next } \
	/^- \*\*Status:\*\* / { \
		if (cur == "") next; \
		if ($$3 != curregion) { printf "  FAIL  %s is under %s but its Status says %s\n", cur, curregion, $$3; bad=1 } \
		cur=""; next } \
	END { \
		split("open:begin open:end resolved:begin resolved:end", want, " "); \
		for (i=1; i<=4; i++) if (m[want[i]] != 1) { \
			printf "  FAIL  marker <!-- fixes:%s --> appears %d times, want exactly 1\n", want[i], m[want[i]]+0; bad=1 } \
		if (bad) { print "  tracker structure is broken - see the editing note at the top of the file"; exit 1 } \
		printf "  ok   fixes.md      %d open, %d resolved, markers intact\n", n["Open"]+0, n["Resolved"]+0 }' \
	$(TRACKER)
	@awk '\
	/^<!-- backlog:begin -->$$/ { m["begin"]++; in_=1; next } \
	/^<!-- backlog:end -->$$/   { m["end"]++;   in_=0; next } \
	in_ && /^[[:space:]]*$$/ { next } \
	in_ { \
		if ($$0 !~ /^- \[[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]\] \[(measured|reading)\] \([^)]+\) .+ \(from [^)]+\)$$/) { \
			printf "  FAIL  backlog line %d does not match \"- [YYYY-MM-DD] [measured|reading] (area) finding — evidence (from SOURCE)\"\n", NR; bad=1 } \
		n++; next } \
	END { \
		if (m["begin"] != 1 || m["end"] != 1) { print "  FAIL  backlog markers <!-- backlog:begin/end --> must each appear exactly once"; bad=1 } \
		if (bad) { print "  backlog format is broken - see /fix -> Findings triage"; exit 1 } \
		printf "  ok   backlog.md    %d item(s)\n", n+0 }' \
	$(BACKLOG)

REFS_EXCLUDE := ':!docs/design/' ':!docs/tracker/' ':!.claude/' ':!CLAUDE.md' ':!CHANGELOG.md'

refs-check: ## Fail on FIX/phase IDs outside the tracker and design docs, on any reference in templates, and on build tags
	@bad=0; \
	out=$$(git grep -nE 'FIX-?[0-9]{2,}' -- . $(REFS_EXCLUDE)); \
	if [ -n "$$out" ]; then echo "  FAIL  FIX IDs belong in commit messages, not code or docs:"; echo "$$out" | sed 's/^/        /'; bad=1; fi; \
	out=$$(git grep -nE '[Pp]hases? [0-9]+' -- . $(REFS_EXCLUDE) | grep -vE 'phase [123]([^0-9.]|$$)'); \
	if [ -n "$$out" ]; then echo "  FAIL  project phase numbers in code or docs:"; echo "$$out" | sed 's/^/        /'; bad=1; fi; \
	out=$$(git grep -nE 'docs/tracker|phase-[0-9]+\.md|IMPLEMENTATION_ORDER' -- . $(REFS_EXCLUDE) ':!Makefile'); \
	if [ -n "$$out" ]; then echo "  FAIL  tracker paths in code or docs:"; echo "$$out" | sed 's/^/        /'; bad=1; fi; \
	out=$$(git grep -nE 'PRD|§|docs/|[A-Z_]{3,}\.md' -- '*.tmpl'); \
	if [ -n "$$out" ]; then echo "  FAIL  templates emit into user code; no PRD, design-doc or docs/ references:"; echo "$$out" | sed 's/^/        /'; bad=1; fi; \
	out=$$(git grep -nE '^//(go:build|[[:space:]]*\+build)' -- '*.go'); \
	if [ -n "$$out" ]; then echo "  FAIL  build tags are not allowed; module boundaries isolate dependencies, and go.mod 'tool' directives pin tools:"; echo "$$out" | sed 's/^/        /'; bad=1; fi; \
	if [ $$bad -ne 0 ]; then echo "  see guidelines/GO.md -> When to Comment and guidelines/TEMPLATES.md §8"; exit 1; fi; \
	echo "  ok   refs          no internal references in code, templates or published docs"

## Combined Checks

check: tools-check tracker-check refs-check fmt lint vet test ## Run lint + unit tests (pre-push check)

check-examples: tools-check fmt-examples lint-examples vet-examples test-examples ## Run lint + tests on E2E example modules (requires Docker)

## Code Generation

update-golden: ## Regenerate golden files after intentional template changes
	cd cmd/sqlgen && go test -timeout=$(TEST_TIMEOUT) ./gen/... -update

update-golden-e2e: ## Regenerate E2E golden files (expected/ dirs) after intentional changes
	cd cmd/sqlgen && go test -run TestE2EGoldenFiles -update-e2e -count=1 -timeout=10m

## Coverage

coverage: ## Run unit + integration coverage across all modules and print a per-module summary (requires Docker)
	@printf "%-22s %10s\n" "MODULE" "COVERAGE"
	@printf "%-22s %10s\n" "----------------------" "----------"
	@for mod in $(MODULES); do \
		out=$$(cd $$mod && go test -count=1 -timeout=10m -coverprofile=coverage.out ./... 2>/dev/null); \
		pct=$$(cd $$mod && go tool cover -func=coverage.out 2>/dev/null | awk '/^total:/ {print $$3}'); \
		[ -z "$$pct" ] && pct="n/a"; \
		printf "%-22s %10s\n" "$$mod" "$$pct"; \
	done

coverage-html: ## Open an HTML coverage report for one module (usage: make coverage-html MOD=parser)
	@if [ -z "$(MOD)" ]; then echo "usage: make coverage-html MOD=<module>  (e.g. MOD=parser)"; exit 1; fi
	@cd $(MOD) && go test -count=1 -timeout=10m -coverprofile=coverage.out ./... >/dev/null && go tool cover -html=coverage.out

## Maintenance

clean: ## Remove build artifacts and test caches
	@for mod in $(MODULES); do \
		(cd $$mod && go clean -testcache && rm -f coverage.out) || exit 1; \
	done

## Help

help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-20s\033[0m %s\n", $$1, $$2}'

.DEFAULT_GOAL := help

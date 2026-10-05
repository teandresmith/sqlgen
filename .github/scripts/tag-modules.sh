#!/usr/bin/env bash
# Tags every submodule at a release that Release Please has already tagged at
# the repository root.
#
# Usage: tag-modules.sh vX.Y.Z   (run from the repo root, at the release commit)
#
# Go installs a submodule from a path-prefixed tag (cmd/sqlgen/vX.Y.Z), and
# `go install …@vX.Y.Z` refuses a go.mod whose go.sum is incomplete. So:
#
#   1. A module with no in-repo dependency is tagged at the release commit.
#   2. Every other module gets its in-repo requirements pinned to vX.Y.Z, with
#      the go.sum entries that needs, in one commit on top of the release
#      commit, and is tagged there.
#
# The pin commit is reachable only through its tags; it is never pushed to a
# branch. Inside the repo, go.work resolves the modules locally, so the pins
# matter only to consumers.
set -euo pipefail

version=${1:?usage: tag-modules.sh vX.Y.Z}
root=github.com/teandresmith/sqlgen

# Our own modules are fetched straight from the repo and kept out of the
# checksum database: the tags were pushed moments ago.
export GOPRIVATE=$root

# Workspace modules other than the root, from go.work.
modules=()
while IFS= read -r m; do modules+=("$m"); done < <(go work edit -json | jq -r '.Use[].DiskPath' | sed 's#^\./##' | grep -vx '\.' | sort)

# In-repo modules a module imports, resolved through the workspace. A failed
# lookup must stop the script: read as "no dependencies", it would tag the
# module unpinned.
deps_of() {
	local paths
	paths=$(cd "$1" && go list -deps -f '{{with .Module}}{{.Path}}{{end}}' ./...) || return 1
	printf '%s\n' "$paths" | { grep -E "^$root(/|\$)" || true; } | { grep -vx "$root/$1" || true; } | sort -u
}

leaves=()
dependents=()
for m in "${modules[@]}"; do
	deps=$(deps_of "$m") || { echo "cannot list the dependencies of $m" >&2; exit 1; }
	if [ -z "$deps" ]; then leaves+=("$m"); else dependents+=("$m"); fi
done
echo "release commit: ${leaves[*]:-none}"
echo "pin commit:     ${dependents[*]:-none}"

if [ ${#leaves[@]} -gt 0 ]; then
	for m in "${leaves[@]}"; do
		git tag "$m/$version"
		echo "tagged $m/$version at the release commit"
	done
	git push origin "${leaves[@]/%//$version}"
fi

if [ ${#dependents[@]} -eq 0 ]; then
	exit 0
fi

for m in "${dependents[@]}"; do
	deps=()
	while IFS= read -r d; do deps+=("$d"); done < <(deps_of "$m")
	echo "pinning $m to ${deps[*]/%/@$version}"
	# Require the modules by path, then let tidy fill in go.sum. `go get` would
	# resolve each path as a package first and can settle on the root module,
	# which does not contain the parser's packages.
	reqs=()
	for d in "${deps[@]}"; do reqs+=("-require=$d@$version"); done
	(cd "$m" && go mod edit "${reqs[@]}" && GOWORK=off go mod tidy)
	git add "$m/go.mod" "$m/go.sum"
done

git -c user.name="github-actions[bot]" -c user.email="41898282+github-actions[bot]@users.noreply.github.com" \
	commit -q -m "chore(deps): pin in-repo modules to $version"
for m in "${dependents[@]}"; do
	git tag "$m/$version"
	echo "tagged $m/$version at the pin commit"
done
git push origin "${dependents[@]/%//$version}"

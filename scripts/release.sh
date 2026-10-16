#!/usr/bin/env bash
# Release Droi: `scripts/release.sh <version> [git commit options]`, from main with
# CHANGELOG.md's first section already `## <version> - <date>` (its release notes).
# Bumps the version in apps/desktop/package.json, apps/native/mygo.json and devVersion
# in apps/native/main.go, commits them with the changelog as `chore(release): <version>`
# (extra arguments go to git commit, e.g. --trailer), pushes main and the tag
# v<version>, then waits for .github/workflows/release.yml to publish the Release.
# The workflow tags apps/native/v<version> itself. See apps/native/docs/releases.md.
set -euo pipefail

cd "$(dirname "$0")/.."
version="${1:-}"
shift || true
if ! [[ "$version" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
  echo "usage: scripts/release.sh <major.minor.patch> [git commit options]" >&2
  exit 2
fi
tag="v$version"

fail() { echo "release: $*" >&2; exit 1; }

[ "$(git symbolic-ref --short HEAD)" = main ] || fail "not on main"
git diff HEAD --quiet -- . ':!CHANGELOG.md' ||
  fail "commit or stash the changes first (only CHANGELOG.md may be changed)"
git fetch -q origin main
[ -z "$(git rev-list HEAD..origin/main)" ] || fail "main is behind origin/main"
if git rev-parse -q --verify "refs/tags/$tag" >/dev/null || [ -n "$(git ls-remote --tags origin "refs/tags/$tag")" ]; then
  fail "$tag already exists"
fi
first=$(grep -m1 '^## ' CHANGELOG.md || true)
[[ "$first" == "## $version - "* ]] || fail "CHANGELOG.md's first section is \"$first\", want \"## $version - <date>\""

perl -pi -e 's/^(  "version": )"[^"]*"/$1"'"$version"'"/' apps/desktop/package.json apps/native/mygo.json
perl -pi -e 's/^const devVersion = "[^"]*"/const devVersion = "'"$version"'"/' apps/native/main.go
# identity_test.go checks that the three agree.
go test ./apps/native/ -count=1 >/dev/null || fail "go test ./apps/native/ failed with the new version"

git add CHANGELOG.md apps/desktop/package.json apps/native/mygo.json apps/native/main.go
git commit -q -m "chore(release): $version" "$@"
git tag "$tag"
git push -q origin main
git push -q origin "refs/tags/$tag"
echo "Pushed main and $tag; waiting for the Release workflow."

run=""
for _ in $(seq 30); do
  run=$(gh run list --workflow release.yml --branch "$tag" --limit 1 --json databaseId --jq '.[0].databaseId // empty')
  [ -n "$run" ] && break
  sleep 5
done
[ -n "$run" ] || fail "no Release run for $tag"
# gh run watch also stops on a network error; the run's own conclusion decides.
conclusion=""
for _ in $(seq 10); do
  gh run watch "$run" --interval 30 >/dev/null 2>&1 || sleep 10
  conclusion=$(gh run view "$run" --json status,conclusion --jq 'select(.status == "completed") | .conclusion' 2>/dev/null || true)
  [ -n "$conclusion" ] && break
done
if [ "$conclusion" != success ]; then
  echo "release: run $run ${conclusion:-did not finish}: gh run view $run --log-failed" >&2
  echo "A flaky test: fix it on main, then gh run rerun $run --failed (no new version)." >&2
  exit 1
fi
[ "$(gh release view "$tag" --json isDraft --jq .isDraft)" = false ] || fail "$tag is still a draft"
echo "Released $tag: https://github.com/$(gh repo view --json nameWithOwner --jq .nameWithOwner)/releases/tag/$tag"

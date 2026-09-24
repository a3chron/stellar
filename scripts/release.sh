#!/usr/bin/env bash
#
# Cut a stellar release: tag it, then update the nix packaging to match.
#
#   scripts/release.sh 1.5.0
#   scripts/release.sh 1.5.0 --dry-run
#
# Why this exists: nix/package.nix carries two values that have to move
# together on every release - the version and the SRI hash of the release
# tarball. They cannot be collapsed into one edit,
# because the hash is a content hash of the *tagged* tarball and therefore does
# not exist until the tag is pushed. So the order is fixed: tag, push, hash,
# pin. This script does that in the right order, or refuses.
#
# flake.nix deliberately has no version to bump - it derives one from the
# commit, because it builds the working tree rather than a release.

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
PACKAGE_NIX="$REPO_ROOT/nix/package.nix"
GITHUB_OWNER="a3chron"
GITHUB_REPO="stellar"

RED=$'\033[31m'; GREEN=$'\033[32m'; YELLOW=$'\033[33m'; CYAN=$'\033[36m'; RESET=$'\033[0m'

die()  { echo "${RED}error:${RESET} $*" >&2; exit 1; }
info() { echo "${CYAN}==>${RESET} $*"; }
ok()   { echo "${GREEN} ok${RESET} $*"; }

DRY_RUN=false
VERSION=""

for arg in "$@"; do
  case "$arg" in
    --dry-run) DRY_RUN=true ;;
    -h|--help) sed -n '2,20p' "${BASH_SOURCE[0]}" | sed 's/^# \{0,1\}//'; exit 0 ;;
    -*)        die "unknown flag: $arg" ;;
    *)         [ -n "$VERSION" ] && die "give exactly one version"; VERSION="$arg" ;;
  esac
done

[ -n "$VERSION" ] || die "usage: scripts/release.sh <version> [--dry-run]"
VERSION="${VERSION#v}"
[[ "$VERSION" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]] || die "version must look like 1.5.0 (got '$VERSION')"

TAG="v$VERSION"
cd "$REPO_ROOT"

# ---------------------------------------------------------------- preflight
info "Preflight"

for tool in git nix nix-prefetch-url; do
  command -v "$tool" >/dev/null || die "$tool is not on PATH (run inside 'nix develop')"
done

# Remote state first: reasoning about branches from stale refs is how you end
# up tagging the wrong commit.
git fetch --quiet --tags origin

[ -z "$(git status --porcelain)" ] || die "working tree is dirty - commit or stash first"

BRANCH="$(git rev-parse --abbrev-ref HEAD)"
[ "$BRANCH" = "main" ] || die "on branch '$BRANCH' - releases are cut from main"

[ "$(git rev-parse HEAD)" = "$(git rev-parse origin/main)" ] \
  || die "HEAD is not identical to origin/main - pull or push first"

git rev-parse -q --verify "refs/tags/$TAG" >/dev/null \
  && die "tag $TAG already exists locally"
git ls-remote --exit-code --tags origin "refs/tags/$TAG" >/dev/null 2>&1 \
  && die "tag $TAG already exists on origin"

LATEST="$(git tag --sort=-v:refname | head -1)"
COMMIT="$(git rev-parse HEAD)"

ok "branch main, clean, in sync with origin"
ok "latest tag: ${LATEST:-<none>}  ->  new tag: $TAG"
ok "release commit: ${COMMIT:0:12}"

if [ "$DRY_RUN" = true ]; then
  echo
  echo "${YELLOW}dry run${RESET} - would tag $TAG at ${COMMIT:0:12}, push it,"
  echo "         then pin version/hash in nix/package.nix and push that."
  exit 0
fi

echo
read -r -p "Tag $TAG and push to origin? [y/N]: " reply
[[ "$reply" =~ ^[Yy]$ ]] || { echo "aborted."; exit 1; }

# ---------------------------------------------------------------- tag
info "Tagging"
git tag -a "$TAG" -m "$TAG"
git push origin "$TAG"
ok "pushed $TAG (goreleaser builds from here)"

# ------------------------------------------------------- hash the tarball
# GitHub generates this archive on demand, so it is available immediately -
# but only once the tag exists on the remote, which is why this step is here
# and not earlier.
info "Hashing the release tarball"
TARBALL="https://github.com/$GITHUB_OWNER/$GITHUB_REPO/archive/refs/tags/$TAG.tar.gz"
RAW_HASH="$(nix-prefetch-url --unpack "$TARBALL" 2>/dev/null | tail -1)"
[ -n "$RAW_HASH" ] || die "could not fetch $TARBALL"
SRI_HASH="$(nix hash convert --hash-algo sha256 --to sri "$RAW_HASH")"
ok "$SRI_HASH"

# ---------------------------------------------------------------- pin
info "Pinning nix/package.nix"
[ -f "$PACKAGE_NIX" ] || die "missing $PACKAGE_NIX"

# Anchored so these can only match their own lines.
sed -i \
  -e "s|^  version = \".*\";|  version = \"$VERSION\";|" \
  -e "s|^    hash = \"sha256-.*\";|    hash = \"$SRI_HASH\";|" \
  "$PACKAGE_NIX"

grep -q "version = \"$VERSION\";" "$PACKAGE_NIX" || die "version pin did not apply"
grep -q "hash = \"$SRI_HASH\";"   "$PACKAGE_NIX" || die "hash pin did not apply"
ok "version and hash updated"

# --------------------------------------------------------------- verify
info "Building the pinned release expression"
if nix-build -E "with import <nixpkgs> {}; callPackage $PACKAGE_NIX {}" --no-out-link >/dev/null 2>&1; then
  ok "nix/package.nix builds from the tagged source"
else
  echo "${YELLOW}warning:${RESET} the pinned expression did not build." >&2
  echo "         The tag is already pushed; fix nix/package.nix and commit by hand." >&2
fi

# --------------------------------------------------------------- commit
info "Committing the pin"
git add "$PACKAGE_NIX"
git commit -m "chore(nix): pin $TAG sources

Updates version and source hash in nix/package.nix to
match the $TAG release. Generated by scripts/release.sh."
git push origin main
ok "pushed"

echo
echo "${GREEN}Released $TAG${RESET}"
echo "  - goreleaser is building from the tag"
echo "  - nix/package.nix now points at it (ready for a nixpkgs PR)"

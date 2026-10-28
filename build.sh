#!/bin/bash

prog=$(realpath "$0") || exit 1
root=$(dirname "$prog")
rc=0
if [ -z "${VERSION:-}" ]; then
  latest=$(git describe --tags --abbrev=0 2>/dev/null || echo "")
  version="${latest:+${latest}-}dev"
else
  version=$VERSION
fi

function log_info { echo "[INFO] $*"; }
function log_error { echo "[ERRO] $*" >&2; }

function do_echo {
  echo "[CMD ] $*"
  local code
  TIMEFORMAT="[TIME] took %3lR"
  if time "$@"; then
    return 0
  else
    code=$?
    if [ "$rc" -eq 0 ]; then
      rc=$code
    fi
    log_error "return code $code"
    return "$code"
  fi
}

function usage {
  log_info "Usage: $(basename "$prog") [-h] [-b] [-t] [-v] [-l] [-g] [-i] [-r [VERSION]]"
  log_info "  -h, help:    Show this help message"
  log_info "  -b, build:   Build bin/lunacy"
  log_info "  -t, test:    Run Go tests with per-package coverage"
  log_info "  -v, vet:     Run go vet"
  log_info "  -l, lint:    Run golangci-lint"
  log_info "  -g, gen:     Run go fix and format with gofumpt"
  log_info "  -i, install: Install lunacy"
  log_info "  -r, release: Validate, tag, and push; bump patch unless VERSION is supplied"
  log_info "Pass multiple actions as separate arguments, e.g. -g -t -v -l -b."
  log_info "Release must be the final action. Without VERSION, fetch tags and bump the latest stable patch (initially v0.0.1)."
}

function require_clean_worktree {
  local status
  if ! status=$(git status --porcelain); then
    log_error "Cannot inspect the Git worktree"
    return 1
  fi
  if [ -n "$status" ]; then
    log_error "Commit or stash all changes, including untracked files, before releasing"
    return 1
  fi
}

function do_release {
  local tag=${1:-} branch local_tag remote_tag tags candidate
  if [ "$rc" -ne 0 ]; then
    log_error "Refusing to release after a failed action"
    return "$rc"
  fi
  if [[ -n "$tag" && ! "$tag" =~ ^v[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?$ ]]; then
    log_error "Expected vMAJOR.MINOR.PATCH or vMAJOR.MINOR.PATCH-prerelease"
    return 1
  fi
  require_clean_worktree || return "$?"
  if ! branch=$(git symbolic-ref --quiet --short HEAD); then
    log_error "Check out a branch before releasing; detached HEAD is not supported"
    return 1
  fi
  if [ -z "$tag" ]; then
    do_echo git fetch --tags origin || return "$?"
    if ! tags=$(git tag --list 'v*' --sort=-version:refname); then
      log_error "Cannot inspect tags to choose the next patch version"
      return 1
    fi
    tag=v0.0.1
    while IFS= read -r candidate; do
      if [[ "$candidate" =~ ^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$ ]]; then
        tag="v${BASH_REMATCH[1]}.${BASH_REMATCH[2]}.$((10#${BASH_REMATCH[3]} + 1))"
        break
      fi
    done <<< "$tags"
    log_info "Next patch release: $tag"
  fi
  if ! local_tag=$(git tag --list "$tag"); then
    log_error "Cannot inspect local tags"
    return 1
  fi
  if [ -n "$local_tag" ]; then
    log_error "Tag $tag already exists locally; refusing to replace it"
    return 1
  fi
  if ! remote_tag=$(git ls-remote --tags origin "refs/tags/$tag"); then
    log_error "Cannot inspect tags on origin"
    return 1
  fi
  if [ -n "$remote_tag" ]; then
    log_error "Tag $tag already exists on origin; refusing to replace it"
    return 1
  fi
  do_echo env VERSION="$tag" "$prog" test vet lint build || return "$?"
  require_clean_worktree || return "$?"
  do_echo git tag -a "$tag" -m "Release $tag" || return "$?"
  if ! do_echo git push --atomic --no-follow-tags origin \
    "HEAD:refs/heads/$branch" "refs/tags/$tag:refs/tags/$tag"; then
    log_error "Push failed; local tag $tag remains. Resolve the error and retry the printed git push command"
    return "$rc"
  fi
  log_info "Release $tag pushed; GitHub Actions will publish the image, binaries, and release notes"
}

cd "$root" || exit 1

if [ "$#" -eq 0 ]; then
  usage
fi

while [ "$#" -gt 0 ]; do
  arg=$1
  shift

  case "$arg" in
  -h | help)
    usage
    ;;
  -b | build)
    do_echo mkdir -p bin/
    do_echo go build -trimpath -ldflags "-s -w -X main.version=$version" -o bin/lunacy ./cmd/lunacy
    ;;
  -t | test)
    do_echo go test -cover ./...
    ;;
  -v | vet)
    do_echo go vet ./...
    ;;
  -l | lint)
    do_echo go tool golangci-lint run
    ;;
  -g | gen)
    do_echo go fix ./... && do_echo go tool gofumpt -w -extra .
    ;;
  -i | install)
    do_echo go install -trimpath -ldflags "-s -w -X main.version=$version" ./cmd/lunacy
    ;;
  -r | release)
    if [ "$#" -gt 1 ]; then
      log_error "Release accepts an optional version and must be the final action"
      usage
      exit 1
    fi
    do_release "${1:-}" || exit "$?"
    if [ "$#" -gt 0 ]; then
      shift
    fi
    ;;
  *)
    log_error "Unknown argument $arg"
    usage
    exit 1
    ;;
  esac
done

exit "$rc"

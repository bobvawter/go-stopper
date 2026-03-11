#!/usr/bin/env bash
# Copyright 2025 Bob Vawter (bob@vawter.org)
# SPDX-License-Identifier: MIT

set -eo pipefail

failed=0
step() {
  local label=$1
  shift
  echo "::group::$label"
  echo Running "$@"
  "$@" || {
    echo "::error::$label"
    ((failed+=1))
  }
  echo "::endgroup::"
}

LEGACY_GO=go1.21.13
STATICCHECK=honnef.co/go/tools/cmd/staticcheck@v0.7.0

# Enable synctest.
export GODEBUG=asynctimerchan=0

# On Mac, we need to link the old test binaries externally to satisfy
# modern loader requirements.
[[ "$OSTYPE" == "darwin"* ]] && EXTRA_LD_FLAGS="-ldflags=-linkmode=external"

step "go test" go test -v -race -coverpkg=./... -covermode=atomic -coverprofile=coverage.txt -timeout 10s ./...
step "staticcheck" go run $STATICCHECK -checks all ./...
step "go vet" go vet ./...
GOTOOLCHAIN=$LEGACY_GO step "go early" go test -timeout 10s "$EXTRA_LD_FLAGS" ./...

pushd compat > /dev/null
step "compat: go test" go test -v -race -coverpkg=./... -covermode=atomic -coverprofile=coverage.txt -timeout 10s  ./...
# SA1019 is deprecated symbols.
step "compat: staticcheck" go run $STATICCHECK -checks all,-SA1019 ./...
step "compat: go vet" go vet ./...
GOTOOLCHAIN=$LEGACY_GO step "compat: go early" go test -timeout 10s "$EXTRA_LD_FLAGS" ./...
popd > /dev/null

exit $failed

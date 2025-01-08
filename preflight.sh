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

step "go test" go test -v -race -coverpkg=./... -covermode=atomic -coverprofile=coverage.txt ./...
step "staticcheck" go run honnef.co/go/tools/cmd/staticcheck@latest -checks all ./...
step "go vet" go vet ./...

exit $failed
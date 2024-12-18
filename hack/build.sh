#!/usr/bin/env bash

set -e

commit_hash() {
	git rev-parse HEAD
}

now() {
	date +%Y-%m-%dT%H:%M:%S%z
}

main() {
	LDFLAGS="-s -w -buildid="
	LDFLAGS="$LDFLAGS -X zedex/cmd.GIT_COMMIT_SHA=$(commit_hash)"
	LDFLAGS="$LDFLAGS -X zedex/cmd.BUILD_TIME=$(now)"
	set -x
	CGO_ENABLED=0 go build -C src -trimpath -ldflags="$LDFLAGS" -o ../zedex
}

main $@

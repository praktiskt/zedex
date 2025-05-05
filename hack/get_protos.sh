#!/usr/bin/env bash

set -ex

OUTDIR="src/zed/pb"
ZED_TEMP_DIR="/tmp/zed-repo"

clone_dir() {
	cleanup
	git clone --sparse https://github.com/zed-industries/zed.git "$ZED_TEMP_DIR" --filter=blob:none
	pushd "$ZED_TEMP_DIR"
	git sparse-checkout set crates/proto/proto
	popd
}

cleanup() {
	rm -rf "$ZED_TEMP_DIR"
}

main() {
	clone_dir
	mv $ZED_TEMP_DIR/crates/proto/proto/* ./src/zed/pb/
	cleanup
	pushd ./src/zed/pb
	PROTOS=$(ls | grep -E '\.proto$')
	for FILE in $PROTOS; do
		sed -i '2i option go_package = "./pb";' $FILE
	done
	protoc --go_out=. --go_opt=paths=source_relative *.proto
	popd
}

main $@

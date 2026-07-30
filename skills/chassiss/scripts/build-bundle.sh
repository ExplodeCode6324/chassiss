#!/bin/sh
set -eu

script_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
skill_dir=$(CDPATH= cd -- "$script_dir/.." && pwd)
repo_dir=$(CDPATH= cd -- "$skill_dir/../.." && pwd)

bundle_version=${CHASSISS_BUNDLE_VERSION:-v1.0.0-rc.4}
source_commit=${CHASSISS_BUNDLE_SOURCE_COMMIT:-}
if [ -z "$source_commit" ]; then
	source_commit=$(git -C "$repo_dir" rev-parse HEAD)
fi

source_commit=$(git -C "$repo_dir" rev-parse --verify "$source_commit^{commit}")
case "$source_commit" in
	*[!0-9a-f]*|"")
		echo "CHASSISS_BUNDLE_SOURCE_COMMIT must resolve to a lowercase Git commit OID." >&2
		exit 2
		;;
esac
case "${#source_commit}" in
	40|64) ;;
	*)
		echo "CHASSISS_BUNDLE_SOURCE_COMMIT must resolve to a full Git commit OID." >&2
		exit 2
		;;
esac

build_root=$(mktemp -d "${TMPDIR:-/tmp}/chassiss-bundle.XXXXXX")
trap 'rm -rf "$build_root"' EXIT HUP INT TERM
source_dir="$build_root/source"
archive_path="$build_root/source.tar"
mkdir -p "$source_dir"
git -C "$repo_dir" archive --format=tar --output="$archive_path" "$source_commit"
tar -xf "$archive_path" -C "$source_dir"

for target in darwin/arm64 darwin/amd64 linux/arm64 linux/amd64; do
	bundle_os=${target%/*}
	bundle_arch=${target#*/}
	output_dir="$skill_dir/bin/$bundle_os-$bundle_arch"
	mkdir -p "$output_dir"
	(
		cd "$source_dir"
		CGO_ENABLED=0 GOOS="$bundle_os" GOARCH="$bundle_arch" go build \
			-buildvcs=false \
			-trimpath \
			-ldflags "-s -w -X github.com/ExplodeCode6324/chassiss/internal/cli.Version=$bundle_version -X github.com/ExplodeCode6324/chassiss/internal/cli.BuildDigest=$source_commit -X github.com/ExplodeCode6324/chassiss/internal/cli.ReleaseIdentity=skill-bundled" \
			-o "$output_dir/chassiss" \
			./cmd/chassiss
	)
done

(
	cd "$source_dir"
	go run ./skills/chassiss/scripts/bundle_manifest \
		--skill "$skill_dir" \
		--source "$source_commit" \
		--version "$bundle_version"
)

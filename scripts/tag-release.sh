#!/bin/sh

set -eu

version=${1:-}
if ! printf '%s\n' "$version" | grep -Eq '^[0-9]+\.[0-9]+\.[0-9]+$'; then
	echo "set VERSION=x.y.z in version.env or on the make command line" >&2
	exit 2
fi

tag="v$version"
modules="
integrations/aerospike
integrations/prometheus
integrations/redis
integrations/valkey
"

scripts/check-version.sh "$version"

if [ "$(git branch --show-current)" != "main" ]; then
	echo "release tags must be created from main" >&2
	exit 1
fi

if [ -n "$(git status --porcelain)" ]; then
	echo "working tree must be clean" >&2
	exit 1
fi

if ! git rev-parse --verify refs/remotes/origin/main >/dev/null 2>&1 ||
	[ "$(git rev-parse HEAD)" != "$(git rev-parse refs/remotes/origin/main)" ]; then
	echo "main must be synchronized with its upstream branch" >&2
	exit 1
fi

tags="$tag"
for module in $modules; do
	tags="$tags $module/$tag"
done

for release_tag in $tags; do
	if git rev-parse -q --verify "refs/tags/$release_tag" >/dev/null; then
		echo "tag already exists: $release_tag" >&2
		exit 1
	fi
done

make check
make build-basic

for release_tag in $tags; do
	git tag -a "$release_tag" -m "$release_tag"
done

echo "Created release tags:"
for release_tag in $tags; do
	echo "  $release_tag"
done
echo "Push them with: git push --atomic origin $tags"

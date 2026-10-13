#!/bin/sh

set -eu

repository_root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
cd "$repository_root"

line_count=$(wc -l < version.env | tr -d ' ')
if [ "$line_count" -ne 1 ] || ! grep -Eq '^VERSION=[0-9]+\.[0-9]+\.[0-9]+$' version.env; then
	echo "version.env must contain VERSION=x.y.z" >&2
	exit 1
fi
version=$(sed -n 's/^VERSION=//p' version.env)

if [ "$version" != "${1:-$version}" ]; then
	echo "requested version ${1} does not match version.env ($version)" >&2
	exit 1
fi

tag="v$version"
for module in \
	integrations/aerospike \
	integrations/prometheus \
	integrations/redis \
	integrations/valkey
do
	if ! grep -Fq "github.com/Intent-IQ/identity-go $tag" "$module/go.mod"; then
		echo "$module/go.mod must require github.com/Intent-IQ/identity-go $tag" >&2
		exit 1
	fi
done

echo "Version configuration is consistent: $tag"

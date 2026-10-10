#!/usr/bin/env bash
set -euo pipefail

usage() {
    echo "Usage: $0 [source|local] [image:tag]"
    echo "  source: compile with CGO inside Docker (default)"
    echo "  local:  compile with the local Linux Go/C toolchain; send only the binary to Docker"
}

if [[ ${1:-} == -h || ${1:-} == --help ]]; then
    usage
    exit 0
fi
if (( $# > 2 )); then
    usage >&2
    exit 1
fi

mode=${1:-source}
image=${2:-victoria-traces:local}
repo_root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)

case "$mode" in
    source)
        docker build --tag "$image" --file "$repo_root/Dockerfile" "$repo_root"
        ;;
    local)
        if [[ $(go env GOHOSTOS) != linux ]]; then
            echo "Local CGO builds require a Linux Go/C toolchain. Use source mode on other hosts." >&2
            exit 1
        fi
        arch=$(go env GOHOSTARCH)
        context=$(mktemp -d)
        trap 'rm -rf -- "$context"' EXIT
        CGO_ENABLED=1 GOOS=linux GOARCH="$arch" go -C "$repo_root" build \
            -mod=vendor -trimpath -buildvcs=false \
            -tags 'netgo osusergo' \
            -ldflags "-extldflags '-static'" \
            -o "$context/victoria-traces" ./app/victoria-traces
        cp -- "$repo_root/ADockerfile" "$context/Dockerfile"
        docker build --platform "linux/$arch" --tag "$image" "$context"
        ;;
    *)
        usage >&2
        exit 1
        ;;
esac

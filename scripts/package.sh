#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."
source release-naming.env
version=${VERSION:-0.1.0-poc}
mkdir -p dist
for target in darwin/arm64 darwin/amd64 linux/amd64 linux/arm64 windows/amd64; do
  os=${target%/*}; arch=${target#*/}; dir="dist/$CLI_NAME-$version-$os-$arch"
  mkdir -p "$dir"; ext=""; [[ $os != windows ]] || ext=.exe
  GOOS=$os GOARCH=$arch CGO_ENABLED=0 go build -buildvcs=false -trimpath -ldflags "-s -w -X main.version=$version" -o "$dir/$BINARY_NAME$ext" "$BUILD_TARGET"
  cp README.md LICENSE NOTICE openapi.json "$dir/"
  mkdir -p "$dir/docs"
  cp -R docs/. "$dir/docs/"
  (cd dist && zip -qr "$(basename "$dir").zip" "$(basename "$dir")")
done
(cd dist && sha256sum ./*.zip > SHA256SUMS)

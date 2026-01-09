#!/usr/bin/env bash

BINARY_NAME=dyndns

build() {
  # Disable cgo to make the binary statically linked because it wouldn't work on a system with old glibc
  echo "Building $1 $2"
  CGO_ENABLE=0 GOOS=$1 GOARCH=$2 go build -o "build/$BINARY_NAME-$1-$2"
}

rm -r build
mkdir build

build linux amd64
build linux arm
build linux arm64
build linux 386
#!/bin/bash
set -o errexit -o nounset -o pipefail

export CARGO_REGISTRIES_CRATES_IO_PROTOCOL=sparse

# ref: https://wapl.es/rust/2019/02/17/rust-cross-compile-linux-to-macos.html
export PATH="/opt/osxcross/target/bin:$PATH"
export LIBZ_SYS_STATIC=1

# See https://github.com/CosmWasm/wasmvm/issues/222#issuecomment-880616953 for two approaches to
# enable stripping through cargo (if that is desired).

echo "Starting aarch64-apple-darwin build"
export CC=aarch64-apple-darwin20.4-clang
export CXX=aarch64-apple-darwin20.4-clang++
cargo build --release --target aarch64-apple-darwin

echo "Starting x86_64-apple-darwin build"
export CC=o64-clang
export CXX=o64-clang++
cargo build --release --target x86_64-apple-darwin

# Create a universal library with both archs
lipo -output artifacts/libwasmvm.dylib -create \
  target/x86_64-apple-darwin/release/deps/libwasmvm.dylib \
  target/aarch64-apple-darwin/release/deps/libwasmvm.dylib

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
cargo build --release --target aarch64-apple-darwin --example wasmvmstatic

echo "Starting x86_64-apple-darwin build"
export CC=o64-clang
export CXX=o64-clang++
cargo build --release --target x86_64-apple-darwin --example wasmvmstatic

# Go 1.27.4+ refuses cgo archives that carry LLVM bitcode, which Rust's prebuilt std embeds. This image
# has no Mach-O objcopy, so strip __LLVM,__bitcode and __LLVM,__cmdline from the result with a
# llvm-objcopy matching rustc's LLVM (rustup component add llvm-tools) before committing it.
# Create a universal library with both archs
lipo -output artifacts/libwasmvmstatic_darwin.a -create \
  target/x86_64-apple-darwin/release/examples/libwasmvmstatic.a \
  target/aarch64-apple-darwin/release/examples/libwasmvmstatic.a

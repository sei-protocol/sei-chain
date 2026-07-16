# Pinned pre-gcc-12 libgcc for the static seid build

`libgcc.a` and `libgcc_eh.a` from Alpine 3.15's gcc 10.3.1 package. The static
build (`scripts/build-static.sh`) links these ahead of the build image's own
libgcc via `STATIC_EXTRA_LDFLAGS=-L<this dir>`.

## Why

gcc >= 12 replaced libgcc's DWARF unwind-frame registry with a lock-free
b-tree (`libgcc/unwind-dw2-btree.h`). Wasmer registers unwind frames for every
JIT-compiled wasm module (`__register_frame`, called from
`wasmer_compiler::engine::unwind::systemv::UnwindRegistry::publish`), and under
that registration pattern the b-tree corrupts and SIGSEGVs (`btree_insert`
walks a null/garbage node pointer). A statically linked musl `seid` gets the
registry from the build image's toolchain (current Alpine ships gcc 15), which
made every static binary crash on ~70% of boots at the genesis wasm store.
Dynamically linked glibc builds use the shared `libgcc_s` runtime path and
never hit this; upstream wasmd's static binaries link a pre-b-tree libgcc,
which is what this pin restores.

`scripts/build-static.sh` verifies these archives' checksums before the build
and asserts the final binary contains no `version_lock_lock_exclusive` symbol,
so a toolchain upgrade cannot silently reintroduce the b-tree registry.

## Provenance

Extracted from
`https://dl-cdn.alpinelinux.org/alpine/v3.15/main/x86_64/gcc-10.3.1_git20211027-r0.apk`

- apk sha256: `dbbe8d585eb1f8fdb5815168944d442f28c79fbc9be98bba7cfffaff1e5c10bb`
- libgcc.a sha256: `d3e066fafde74d53a89d48f2ceb9ed9934249a5d450e281edd22947a829469d8`
- libgcc_eh.a sha256: `d14c9973a735909e11a863b0c850300bfd3aa683ef4689cbe76a53139766ed79`

To re-derive:

```sh
wget https://dl-cdn.alpinelinux.org/alpine/v3.15/main/x86_64/gcc-10.3.1_git20211027-r0.apk
tar -xzf gcc-10.3.1_git20211027-r0.apk \
  usr/lib/gcc/x86_64-alpine-linux-musl/10.3.1/libgcc.a \
  usr/lib/gcc/x86_64-alpine-linux-musl/10.3.1/libgcc_eh.a
```

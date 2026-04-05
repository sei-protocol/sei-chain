# Cosmovisor and `libwasmvm154` During the v6.4 Upgrade Window

## Summary

During the transition from **v6.3.x** to **v6.4.0**, nodes running under `cosmovisor` may still execute the **v6.3.x** binary until the upgrade height is reached. That binary depends on the legacy shared library:

- `libwasmvm154.x86_64.so`

To keep the currently active binary working while **v6.4.0** is staged, the required wasm shared libraries must remain available in the runtime library path during the upgrade window.

For **v6.4**, the fix is to **place the required shared libraries in the expected runtime location** so the active pre-upgrade binary can continue to start normally.

For **v6.5**, the legacy `v154` dependency is no longer needed and can be removed.

---

## Why this happens

`cosmovisor` is expected to keep the **old binary active** until the chain reaches the configured upgrade height.

That means the following is normal before the upgrade executes:

- `current` still points to the **v6.3.x** binary
- the **v6.4.0** binary may already be staged on disk
- both versions may need to coexist temporarily

The issue is that the active **v6.3.x** binary still links against:

- `libwasmvm.x86_64.so`
- `libwasmvm152.x86_64.so`
- `libwasmvm154.x86_64.so`
- `libwasmvm155.x86_64.so`

If `libwasmvm154.x86_64.so` is no longer available when the node is still running the **v6.3.x** binary, startup fails with an error like:

```text
error while loading shared libraries: libwasmvm154.x86_64.so: cannot open shared object file: No such file or directory
```

# TODO Locations

This file tracks the temporary `//TODO:` markers that gate benchmarking hacks. Replace each stub guard with a real condition once ante handlers are wired back in.

- `app/app.go:1328` & `app/app.go:1352` — Restore light invariance checks when ante handlers are re-enabled.
- `app/app.go:1780` & `app/app.go:1800` — Re-enable deferred balance writes once ante handlers run.
- `x/evm/module.go:358` — Reinstate EVM surplus redistribution after ante handler execution is restored.
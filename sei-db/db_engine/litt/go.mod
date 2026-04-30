// Nested Go module boundary for the LittDB subtree.
//
// The sei-db/db_engine/litt/ tree is a raw import from the upstream LittDB
// project (originally vendored from github.com/Layr-Labs/eigenda/litt). It
// has not yet been adapted to this repo's dependency set — all files are
// guarded by `//go:build littdb_wip`.
//
// Declaring this subtree as a separate module hides it from the parent
// module's `go mod tidy`, `go test ./...`, `go build ./...`, `go vet ./...`,
// and `golangci-lint run` — none of which cross module boundaries. See
// `sei-db/db_engine/litt/README.md` ("Work-in-progress guard") for the
// incremental integration policy.
//
// Integration status
// ------------------
// Internal cross-package imports have been rewritten from the upstream
// `github.com/Layr-Labs/eigenda/litt/...` prefix to this repo's native
// `github.com/sei-protocol/sei-chain/sei-db/db_engine/litt/...` prefix, and
// the helpers originally imported from `github.com/Layr-Labs/eigenda/common`
// (cache, structures, enforce, pprof), `github.com/Layr-Labs/eigenda/core`
// and `github.com/Layr-Labs/eigenda/test{,/random}` have been pulled in-tree
// under `./util/`. The subtree currently builds under `-tags littdb_wip`.
//
// Logging has been migrated from `github.com/Layr-Labs/eigensdk-go/logging`
// to the standard library's `log/slog`; no external logger dependency
// remains in this subtree.
//
// This file (and this module boundary) can be removed once the litt package
// fully compiles and passes lint inside the parent sei-chain module.
module github.com/sei-protocol/sei-chain/sei-db/db_engine/litt

go 1.25.6

require (
	github.com/dchest/siphash v1.2.3
	github.com/docker/docker v28.2.2+incompatible
	github.com/docker/go-connections v0.5.0
	github.com/docker/go-units v0.5.0
	github.com/prometheus/client_golang v1.23.2
	github.com/stretchr/testify v1.11.1
	github.com/syndtr/goleveldb v1.0.1-0.20210819022825-2ae1ddf74ef7
	github.com/urfave/cli/v2 v2.27.7
	golang.org/x/crypto v0.50.0
	golang.org/x/time v0.15.0
)

require (
	github.com/Microsoft/go-winio v0.6.2 // indirect
	github.com/beorn7/perks v1.0.1 // indirect
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/containerd/errdefs v1.0.0 // indirect
	github.com/containerd/errdefs/pkg v0.3.0 // indirect
	github.com/containerd/log v0.1.0 // indirect
	github.com/cpuguy83/go-md2man/v2 v2.0.7 // indirect
	github.com/davecgh/go-spew v1.1.1 // indirect
	github.com/distribution/reference v0.6.0 // indirect
	github.com/felixge/httpsnoop v1.0.4 // indirect
	github.com/fsnotify/fsnotify v1.6.0 // indirect
	github.com/go-logr/logr v1.4.3 // indirect
	github.com/go-logr/stdr v1.2.2 // indirect
	github.com/gogo/protobuf v1.3.2 // indirect
	github.com/golang/snappy v0.0.5-0.20220116011046-fa5810519dcb // indirect
	github.com/moby/docker-image-spec v1.3.1 // indirect
	github.com/moby/sys/atomicwriter v0.1.0 // indirect
	github.com/moby/term v0.5.0 // indirect
	github.com/morikuni/aec v1.0.0 // indirect
	github.com/munnerz/goautoneg v0.0.0-20191010083416-a7dc8b61c822 // indirect
	github.com/opencontainers/go-digest v1.0.0 // indirect
	github.com/opencontainers/image-spec v1.1.1 // indirect
	github.com/pkg/errors v0.9.1 // indirect
	github.com/pmezard/go-difflib v1.0.0 // indirect
	github.com/prometheus/client_model v0.6.2 // indirect
	github.com/prometheus/common v0.66.1 // indirect
	github.com/prometheus/procfs v0.16.1 // indirect
	github.com/russross/blackfriday/v2 v2.1.0 // indirect
	github.com/xrash/smetrics v0.0.0-20240521201337-686a1a2994c1 // indirect
	go.opentelemetry.io/auto/sdk v1.2.1 // indirect
	go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp v0.68.0 // indirect
	go.opentelemetry.io/otel v1.43.0 // indirect
	go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp v1.43.0 // indirect
	go.opentelemetry.io/otel/metric v1.43.0 // indirect
	go.opentelemetry.io/otel/trace v1.43.0 // indirect
	go.yaml.in/yaml/v2 v2.4.2 // indirect
	golang.org/x/sys v0.43.0 // indirect
	google.golang.org/protobuf v1.36.11 // indirect
	gopkg.in/yaml.v2 v2.4.0 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
	gotest.tools/v3 v3.5.2 // indirect
)

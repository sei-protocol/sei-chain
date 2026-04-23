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
// `github.com/sei-protocol/sei-chain/sei-db/db_engine/litt/...` prefix.
//
// Upstream helpers that have not yet been ported live under the
// `placeholder/` subtree (e.g. `./placeholder/eigenda/common`,
// `./placeholder/eigensdk-go/logging`). Those import paths are expected to
// fail to resolve until the corresponding packages are pulled in-tree (or
// replaced by sei-chain equivalents). The compile is intentionally broken
// while that migration happens.
//
// This file (and this module boundary) can be removed once the litt package
// fully compiles and passes lint inside the parent sei-chain module.
module github.com/sei-protocol/sei-chain/sei-db/db_engine/litt

go 1.25.6

// Only third-party dependencies whose imports are already resolved in-tree
// are listed here. External `github.com/Layr-Labs/...` helpers must be
// ported into `./placeholder/...` (or replaced by sei-chain equivalents)
// before they can be required directly.
//
// docker/docker was renamed to github.com/moby/moby starting with v28; pin
// to a pre-rename version so `github.com/docker/docker/api/types` still
// resolves from a single module.
require (
	github.com/dchest/siphash v1.2.3
	github.com/docker/docker v27.5.1+incompatible
	github.com/docker/go-connections v0.7.0
	github.com/docker/go-units v0.5.0
	github.com/prometheus/client_golang v1.23.2
	github.com/stretchr/testify v1.11.1
	github.com/syndtr/goleveldb v1.0.0
	github.com/urfave/cli/v2 v2.27.7
	golang.org/x/crypto v0.50.0
	golang.org/x/time v0.15.0
)

require (
	github.com/beorn7/perks v1.0.1 // indirect
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/cpuguy83/go-md2man/v2 v2.0.7 // indirect
	github.com/davecgh/go-spew v1.1.1 // indirect
	github.com/distribution/reference v0.6.0 // indirect
	github.com/felixge/httpsnoop v1.0.4 // indirect
	github.com/go-logr/logr v1.4.3 // indirect
	github.com/go-logr/stdr v1.2.2 // indirect
	github.com/gogo/protobuf v1.3.2 // indirect
	github.com/golang/snappy v0.0.0-20180518054509-2e65f85255db // indirect
	github.com/moby/docker-image-spec v1.3.1 // indirect
	github.com/munnerz/goautoneg v0.0.0-20191010083416-a7dc8b61c822 // indirect
	github.com/opencontainers/go-digest v1.0.0 // indirect
	github.com/opencontainers/image-spec v1.1.1 // indirect
	github.com/pkg/errors v0.9.1 // indirect
	github.com/pmezard/go-difflib v1.0.0 // indirect
	github.com/prometheus/client_model v0.6.2 // indirect
	github.com/prometheus/common v0.66.1 // indirect
	github.com/russross/blackfriday/v2 v2.1.0 // indirect
	github.com/xrash/smetrics v0.0.0-20240521201337-686a1a2994c1 // indirect
	go.opentelemetry.io/auto/sdk v1.2.1 // indirect
	go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp v0.68.0 // indirect
	go.opentelemetry.io/otel v1.43.0 // indirect
	go.opentelemetry.io/otel/metric v1.43.0 // indirect
	go.opentelemetry.io/otel/trace v1.43.0 // indirect
	go.yaml.in/yaml/v2 v2.4.2 // indirect
	golang.org/x/sys v0.43.0 // indirect
	google.golang.org/protobuf v1.36.8 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)

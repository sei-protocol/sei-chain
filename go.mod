module github.com/sei-protocol/sei-chain

go 1.25.6

require (
	cosmossdk.io/errors v1.0.2
	github.com/99designs/keyring v1.2.1
	github.com/BurntSushi/toml v1.4.1-0.20240526193622-a339e1f7089c
	github.com/adlio/schema v1.3.9
	github.com/alitto/pond v1.8.3
	github.com/armon/go-metrics v0.4.1
	github.com/aws/aws-sdk-go v1.44.327
	github.com/benbjohnson/immutable v0.4.3
	github.com/bgentry/speakeasy v0.2.0
	github.com/btcsuite/btcd/btcec/v2 v2.3.5
	github.com/cockroachdb/pebble/v2 v2.1.3
	github.com/coinbase/rosetta-sdk-go v0.7.0
	github.com/confio/ics23/go v0.9.0
	github.com/cosmos/btcutil v1.0.5
	github.com/cosmos/go-bip39 v1.0.0
	github.com/cosmos/ledger-cosmos-go v1.0.0
	github.com/creachadair/atomicfile v0.4.0
	github.com/creachadair/taskgroup v0.3.2
	github.com/creachadair/tomledit v0.0.29
	github.com/duckdb/duckdb-go/v2 v2.5.3
	github.com/dvsekhvalnov/jose2go v1.7.0
	github.com/ethereum/evmc/v12 v12.1.0
	github.com/ethereum/go-ethereum v1.16.8
	github.com/fortytw2/leaktest v1.3.0
	github.com/go-kit/kit v0.13.0
	github.com/gofrs/flock v0.13.0
	github.com/gogo/gateway v1.1.0
	github.com/gogo/protobuf v1.3.3
	github.com/golang-jwt/jwt/v4 v4.5.1
	github.com/golang/mock v1.7.0-rc.1
	github.com/golang/protobuf v1.5.4
	github.com/google/btree v1.1.3
	github.com/google/go-cmp v0.7.0
	github.com/google/gofuzz v1.2.0
	github.com/google/orderedcode v0.0.1
	github.com/google/uuid v1.6.0
	github.com/gorilla/handlers v1.5.2
	github.com/gorilla/mux v1.8.1
	github.com/gorilla/websocket v1.5.3
	github.com/grafana/pyroscope-go/godeltaprof v0.1.8
	github.com/grpc-ecosystem/go-grpc-middleware v1.4.0
	github.com/grpc-ecosystem/go-grpc-prometheus v1.2.0
	github.com/grpc-ecosystem/grpc-gateway v1.16.0
	github.com/hashicorp/golang-lru/v2 v2.0.7
	github.com/hdevalence/ed25519consensus v0.2.0
	github.com/holiman/uint256 v1.3.2
	github.com/improbable-eng/grpc-web v0.15.0
	github.com/jhump/protoreflect v1.18.0
	github.com/ledgerwatch/erigon-lib v0.0.0-20230210071639-db0e7ed11263
	github.com/lib/pq v1.10.9
	github.com/libp2p/go-buffer-pool v0.1.0
	github.com/linxGnu/grocksdb v1.8.11
	github.com/magiconair/properties v1.8.10
	github.com/mattn/go-isatty v0.0.20
	github.com/mroth/weightedrand v1.0.0
	github.com/oasisprotocol/curve25519-voi v0.0.0-20230904125328-1f23a7beb09a
	github.com/ory/dockertest v3.3.5+incompatible
	github.com/parquet-go/parquet-go v0.25.1
	github.com/patrickmn/go-cache v2.1.0+incompatible
	github.com/pkg/errors v0.9.1
	github.com/prometheus/client_golang v1.23.2
	github.com/prometheus/client_model v0.6.2
	github.com/prometheus/common v0.66.1
	github.com/rakyll/statik v0.1.7
	github.com/rs/cors v1.11.1
	github.com/sasha-s/go-deadlock v0.3.5
	github.com/sei-protocol/goutils v0.0.2
	github.com/sei-protocol/sei-load v0.0.0-20251007135253-78fbdc141082
	github.com/sei-protocol/sei-tm-db v0.0.5
	github.com/sei-protocol/seilog v0.0.3
	github.com/snikch/goodman v0.0.0-20171125024755-10e37e294daa
	github.com/spf13/cast v1.10.0
	github.com/spf13/cobra v1.10.2
	github.com/spf13/pflag v1.0.10
	github.com/spf13/viper v1.21.0
	github.com/stretchr/testify v1.11.1
	github.com/syndtr/goleveldb v1.0.1-0.20220721030215-126854af5e6d
	github.com/tendermint/crypto v0.0.0-20191022145703-50d29ede1e15
	github.com/tendermint/go-amino v0.16.0
	github.com/tendermint/tm-db v0.6.8-0.20220519162814-e24b96538a12
	github.com/tidwall/btree v1.7.0
	github.com/tidwall/gjson v1.14.2
	github.com/tidwall/wal v1.2.1
	github.com/zbiljic/go-filelock v0.0.0-20170914061330-1dbf7103ab7d
	github.com/zeebo/blake3 v0.2.4
	go.opentelemetry.io/otel v1.39.0
	go.opentelemetry.io/otel/exporters/jaeger v1.9.0
	go.opentelemetry.io/otel/exporters/prometheus v0.60.0
	go.opentelemetry.io/otel/metric v1.39.0
	go.opentelemetry.io/otel/sdk v1.39.0
	go.opentelemetry.io/otel/sdk/metric v1.39.0
	go.opentelemetry.io/otel/trace v1.39.0
	golang.org/x/crypto v0.47.0
	golang.org/x/exp v0.0.0-20260112195511-716be5621a96
	golang.org/x/net v0.49.0
	golang.org/x/sync v0.19.0
	golang.org/x/sys v0.40.0
	golang.org/x/time v0.13.0
	google.golang.org/genproto/googleapis/api v0.0.0-20250707201910-8d1bb00bc6a7
	google.golang.org/grpc v1.75.0
	google.golang.org/protobuf v1.36.11
	gopkg.in/yaml.v2 v2.4.0
	k8s.io/component-base v0.35.0
	pgregory.net/rapid v1.2.0
)

require (
	github.com/lufia/plan9stats v0.0.0-20211012122336-39d0f177ccd0 // indirect
	github.com/mitchellh/mapstructure v1.5.0 // indirect
	github.com/power-devops/perfstat v0.0.0-20210106213030-5aafc221ea8c // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)

require (
	github.com/regen-network/cosmos-proto v0.3.1
	github.com/rivo/uniseg v0.4.7 // indirect
)

require (
	filippo.io/edwards25519 v1.1.0 // indirect
	github.com/Azure/go-ansiterm v0.0.0-20230124172434-306776ec8161 // indirect
	github.com/DataDog/zstd v1.5.7 // indirect
	github.com/Microsoft/go-winio v0.6.2 // indirect
	github.com/Nvveen/Gotty v0.0.0-20120604004816-cd527374f1e5 // indirect
	github.com/RaduBerinde/axisds v0.0.0-20250419182453-5135a0650657 // indirect
	github.com/RaduBerinde/btreemap v0.0.0-20250419174037-3d62b7205d54 // indirect
	github.com/VictoriaMetrics/fastcache v1.12.2 // indirect
	github.com/andybalholm/brotli v1.2.0 // indirect
	github.com/apache/arrow-go/v18 v18.4.1 // indirect
	github.com/beorn7/perks v1.0.1 // indirect
	github.com/bits-and-blooms/bitset v1.24.3 // indirect
	github.com/cenkalti/backoff v2.2.1+incompatible // indirect
	github.com/cenkalti/backoff/v4 v4.3.0 // indirect
	github.com/cespare/xxhash v1.1.0 // indirect
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/cockroachdb/crlib v0.0.0-20241112164430-1264a2edc35b // indirect
	github.com/cockroachdb/errors v1.12.0 // indirect
	github.com/cockroachdb/fifo v0.0.0-20240606204812-0bbfbd93a7ce // indirect
	github.com/cockroachdb/logtags v0.0.0-20241215232642-bb51bb14a506 // indirect
	github.com/cockroachdb/pebble v1.1.5 // indirect
	github.com/cockroachdb/redact v1.1.6 // indirect
	github.com/cockroachdb/swiss v0.0.0-20251224182025-b0f6560f979b // indirect
	github.com/cockroachdb/tokenbucket v0.0.0-20230807174530-cc333fc44b06 // indirect
	github.com/consensys/gnark-crypto v0.18.0 // indirect
	github.com/containerd/continuity v0.3.0 // indirect
	github.com/cosmos/gorocksdb v1.2.0 // indirect
	github.com/cpuguy83/go-md2man/v2 v2.0.7 // indirect
	github.com/crate-crypto/go-ipa v0.0.0-20240724233137-53bbb0ceb27a // indirect
	github.com/crate-crypto/go-kzg-4844 v1.1.0 // indirect
	github.com/danieljoos/wincred v1.1.2 // indirect
	github.com/davecgh/go-spew v1.1.2-0.20180830191138-d8f796af33cc // indirect
	github.com/deckarep/golang-set/v2 v2.6.0 // indirect
	github.com/decred/dcrd/dcrec/secp256k1/v4 v4.4.0 // indirect
	github.com/desertbit/timer v0.0.0-20180107155436-c41aec40b27f // indirect
	github.com/dgraph-io/badger/v3 v3.2103.2 // indirect
	github.com/dgraph-io/ristretto v0.2.0 // indirect
	github.com/dlclark/regexp2 v1.7.0 // indirect
	github.com/docker/cli v24.0.7+incompatible // indirect
	github.com/docker/go-connections v0.4.0 // indirect
	github.com/docker/go-units v0.5.0 // indirect
	github.com/dop251/goja v0.0.0-20230605162241-28ee0ee714f3 // indirect
	github.com/duckdb/duckdb-go-bindings v0.1.23 // indirect
	github.com/duckdb/duckdb-go-bindings/darwin-amd64 v0.1.23 // indirect
	github.com/duckdb/duckdb-go-bindings/darwin-arm64 v0.1.23 // indirect
	github.com/duckdb/duckdb-go-bindings/linux-amd64 v0.1.23 // indirect
	github.com/duckdb/duckdb-go-bindings/linux-arm64 v0.1.23 // indirect
	github.com/duckdb/duckdb-go-bindings/windows-amd64 v0.1.23 // indirect
	github.com/duckdb/duckdb-go/arrowmapping v0.0.26 // indirect
	github.com/duckdb/duckdb-go/mapping v0.0.25 // indirect
	github.com/dustin/go-humanize v1.0.1 // indirect
	github.com/ethereum/c-kzg-4844 v1.0.0 // indirect
	github.com/ethereum/go-verkle v0.2.2 // indirect
	github.com/felixge/httpsnoop v1.0.4 // indirect
	github.com/fsnotify/fsnotify v1.9.0 // indirect
	github.com/getsentry/sentry-go v0.35.0 // indirect
	github.com/go-logr/logr v1.4.3 // indirect
	github.com/go-logr/stdr v1.2.2 // indirect
	github.com/go-ole/go-ole v1.3.0 // indirect
	github.com/go-sourcemap/sourcemap v2.1.3+incompatible // indirect
	github.com/go-viper/mapstructure/v2 v2.4.0 // indirect
	github.com/goccy/go-json v0.10.5 // indirect
	github.com/godbus/dbus v0.0.0-20190726142602-4481cbc300e2 // indirect
	github.com/golang/groupcache v0.0.0-20241129210726-2c02b8208cf8 // indirect
	github.com/golang/snappy v1.0.0 // indirect
	github.com/google/flatbuffers v25.2.10+incompatible // indirect
	github.com/google/pprof v0.0.0-20230207041349-798e818bf904 // indirect
	github.com/gotestyourself/gotestyourself v2.2.0+incompatible // indirect
	github.com/grafana/regexp v0.0.0-20240518133315-a468a5bfb3bc // indirect
	github.com/gsterjov/go-libsecret v0.0.0-20161001094733-a6f4afe4910c // indirect
	github.com/hashicorp/go-bexpr v0.1.10 // indirect
	github.com/hashicorp/go-immutable-radix v1.3.1 // indirect
	github.com/hashicorp/golang-lru v1.0.2 // indirect
	github.com/holiman/billy v0.0.0-20240216141850-2abb0c79d3c4 // indirect
	github.com/holiman/bloomfilter/v2 v2.0.3 // indirect
	github.com/huin/goupnp v1.3.0 // indirect
	github.com/inconshreveable/mousetrap v1.1.0 // indirect
	github.com/jackpal/go-nat-pmp v1.0.2 // indirect
	github.com/jhump/protoreflect/v2 v2.0.0-beta.1 // indirect
	github.com/jmespath/go-jmespath v0.4.0 // indirect
	github.com/jmhodges/levigo v1.0.0 // indirect
	github.com/keybase/go-keychain v0.0.0-20190712205309-48d3d31d256d // indirect
	github.com/klauspost/compress v1.18.3 // indirect
	github.com/klauspost/cpuid/v2 v2.3.0 // indirect
	github.com/kr/pretty v0.3.1 // indirect
	github.com/kr/text v0.2.0 // indirect
	github.com/mattn/go-colorable v0.1.14 // indirect
	github.com/mattn/go-runewidth v0.0.16 // indirect
	github.com/minio/minlz v1.0.1-0.20250507153514-87eb42fe8882 // indirect
	github.com/mitchellh/pointerstructure v1.2.0 // indirect
	github.com/mtibben/percent v0.2.1 // indirect
	github.com/munnerz/goautoneg v0.0.0-20191010083416-a7dc8b61c822 // indirect
	github.com/nxadm/tail v1.4.11 // indirect
	github.com/olekukonko/tablewriter v0.0.5 // indirect
	github.com/opencontainers/go-digest v1.0.0 // indirect
	github.com/opencontainers/image-spec v1.1.0-rc2 // indirect
	github.com/opencontainers/runc v1.1.14 // indirect
	github.com/pelletier/go-toml/v2 v2.2.4 // indirect
	github.com/petermattis/goid v0.0.0-20260113132338-7c7de50cc741 // indirect
	github.com/pierrec/lz4/v4 v4.1.22 // indirect
	github.com/pion/dtls/v2 v2.2.7 // indirect
	github.com/pion/logging v0.2.2 // indirect
	github.com/pion/stun/v2 v2.0.0 // indirect
	github.com/pion/transport/v2 v2.2.1 // indirect
	github.com/pion/transport/v3 v3.0.1 // indirect
	github.com/pmezard/go-difflib v1.0.1-0.20181226105442-5d4384ee4fb2 // indirect
	github.com/prometheus/otlptranslator v0.0.2 // indirect
	github.com/prometheus/procfs v0.17.0 // indirect
	github.com/rogpeppe/go-internal v1.14.1 // indirect
	github.com/russross/blackfriday/v2 v2.1.0 // indirect
	github.com/sagikazarmark/locafero v0.11.0 // indirect
	github.com/shirou/gopsutil v3.21.11+incompatible // indirect
	github.com/shirou/gopsutil/v3 v3.23.2
	github.com/sirupsen/logrus v1.9.3 // indirect
	github.com/sourcegraph/conc v0.3.1-0.20240121214520-5f936abd7ae8 // indirect
	github.com/spf13/afero v1.15.0 // indirect
	github.com/stretchr/objx v0.5.2 // indirect
	github.com/subosito/gotenv v1.6.0 // indirect
	github.com/supranational/blst v0.3.16-0.20250831170142-f48500c1fdbe // indirect
	github.com/tidwall/match v1.1.1 // indirect
	github.com/tidwall/pretty v1.2.0 // indirect
	github.com/tidwall/tinylru v1.1.0 // indirect
	github.com/tklauser/go-sysconf v0.3.15 // indirect
	github.com/tklauser/numcpus v0.10.0 // indirect
	github.com/urfave/cli/v2 v2.27.5 // indirect
	github.com/xrash/smetrics v0.0.0-20240521201337-686a1a2994c1 // indirect
	github.com/yusufpapurcu/wmi v1.2.4 // indirect
	github.com/zeebo/xxh3 v1.0.2 // indirect
	github.com/zondax/golem v0.27.0 // indirect
	github.com/zondax/hid v0.9.2 // indirect
	github.com/zondax/ledger-go v1.0.1 // indirect
	go.etcd.io/bbolt v1.4.0-alpha.0.0.20240404170359-43604f3112c5 // indirect
	go.opencensus.io v0.24.0 // indirect
	go.opentelemetry.io/auto/sdk v1.2.1 // indirect
	go.uber.org/multierr v1.11.0 // indirect
	go.uber.org/zap v1.27.0 // indirect
	go.yaml.in/yaml/v2 v2.4.3 // indirect
	go.yaml.in/yaml/v3 v3.0.4 // indirect
	golang.org/x/mod v0.32.0
	golang.org/x/telemetry v0.0.0-20260109210033-bd525da824e2 // indirect
	golang.org/x/term v0.39.0 // indirect
	golang.org/x/text v0.33.0 // indirect
	golang.org/x/tools v0.41.0 // indirect
	golang.org/x/xerrors v0.0.0-20240903120638-7835f813f4da // indirect
	google.golang.org/genproto v0.0.0-20250603155806-513f23925822 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20250818200422-3122310a409c // indirect
	gopkg.in/natefinch/lumberjack.v2 v2.2.1 // indirect
	gotest.tools v2.2.0+incompatible // indirect
	nhooyr.io/websocket v1.8.6 // indirect
)

replace (
	github.com/99designs/keyring => github.com/cosmos/keyring v1.1.7-0.20210622111912-ef00f8ac3d76
	github.com/btcsuite/btcd => github.com/btcsuite/btcd v0.23.2
	github.com/confio/ics23/go => github.com/cosmos/cosmos-sdk/ics23/go v0.8.0
	github.com/ethereum/go-ethereum => github.com/sei-protocol/go-ethereum v1.15.7-sei-16
	github.com/gin-gonic/gin => github.com/gin-gonic/gin v1.7.0
	github.com/gogo/protobuf => github.com/regen-network/protobuf v1.3.3-alpha.regen.1
	github.com/keybase/go-keychain => github.com/keybase/go-keychain v0.0.0-20190712205309-48d3d31d256d
	// Latest goleveldb is broken, we have to stick to this version
	github.com/syndtr/goleveldb => github.com/syndtr/goleveldb v1.0.1-0.20210819022825-2ae1ddf74ef7
	github.com/tendermint/tm-db => github.com/sei-protocol/tm-db v0.0.4
	golang.org/x/crypto => golang.org/x/crypto v0.31.0
	// The earliest version supporting reflection needed by cosmos; see:
	//   - https://github.com/grpc/grpc-go/releases/tag/v1.57.0
	google.golang.org/grpc => google.golang.org/grpc v1.57.1
)

module github.com/sei-protocol/sei-chain

go 1.21

require (
	github.com/BurntSushi/toml v1.3.2
	github.com/CosmWasm/wasmd v0.27.0
	github.com/CosmWasm/wasmvm v1.0.1
	github.com/armon/go-metrics v0.4.1
	github.com/cosmos/cosmos-sdk v0.45.10
	github.com/cosmos/go-bip39 v1.0.0
	github.com/cosmos/iavl v0.21.0-alpha.1.0.20230904092046-df3db2d96583
	github.com/cosmos/ibc-go/v3 v3.0.0
	github.com/ethereum/go-ethereum v1.13.2
	github.com/go-playground/validator/v10 v10.4.1
	github.com/gogo/protobuf v1.3.3
	github.com/golang/protobuf v1.5.3
	github.com/golangci/golangci-lint v1.46.0
	github.com/gorilla/mux v1.8.0
	github.com/gorilla/websocket v1.5.0
	github.com/grpc-ecosystem/grpc-gateway v1.16.0
	github.com/justinas/alice v1.2.0
	github.com/k0kubun/pp/v3 v3.2.0
	github.com/mitchellh/mapstructure v1.5.0
	github.com/pkg/errors v0.9.1
	github.com/rs/cors v1.8.2
	github.com/rs/zerolog v1.30.0
	github.com/sei-protocol/goutils v0.0.2
	github.com/sei-protocol/sei-db v0.0.23
	github.com/sirkon/goproxy v1.4.8
	github.com/spf13/cast v1.5.0
	github.com/spf13/cobra v1.6.1
	github.com/stretchr/testify v1.8.4
	github.com/syndtr/goleveldb v1.0.1-0.20220721030215-126854af5e6d
	github.com/tendermint/tendermint v0.37.0-dev
	github.com/tendermint/tm-db v0.6.8-0.20220519162814-e24b96538a12
	go.opentelemetry.io/otel v1.9.0
	go.opentelemetry.io/otel/trace v1.9.0
	golang.org/x/exp v0.0.0-20231110203233-9a3e6036ecaa
	golang.org/x/sync v0.5.0
	golang.org/x/text v0.14.0
	golang.org/x/time v0.3.0
	google.golang.org/genproto/googleapis/api v0.0.0-20230920204549-e6e6cdab5c13
	google.golang.org/grpc v1.58.3
	google.golang.org/protobuf v1.31.0
	gopkg.in/yaml.v2 v2.4.0
	gopkg.in/yaml.v3 v3.0.1
)

replace (
	github.com/CosmWasm/wasmd => github.com/sei-protocol/sei-wasmd v0.0.3-0.20231031145448-477bc8749e39
	github.com/confio/ics23/go => github.com/cosmos/cosmos-sdk/ics23/go v0.8.0
	github.com/cosmos/cosmos-sdk => github.com/sei-protocol/sei-cosmos v0.2.67-0.20240205132256-16a5b680f031
	github.com/cosmos/iavl => github.com/sei-protocol/sei-iavl v0.1.8-0.20230726213826-031d03d26f2d
	github.com/cosmos/ibc-go/v3 => github.com/sei-protocol/sei-ibc-go/v3 v3.3.0
	github.com/ethereum/go-ethereum => github.com/sei-protocol/go-ethereum v1.13.5-sei-6
	github.com/gogo/protobuf => github.com/regen-network/protobuf v1.3.3-alpha.regen.1
	github.com/sei-protocol/sei-db => github.com/sei-protocol/sei-db v0.0.30
	// Latest goleveldb is broken, we have to stick to this version
	github.com/syndtr/goleveldb => github.com/syndtr/goleveldb v1.0.1-0.20210819022825-2ae1ddf74ef7
	github.com/tendermint/tendermint => github.com/sei-protocol/sei-tendermint v0.2.37
	github.com/tendermint/tm-db => github.com/sei-protocol/tm-db v0.0.4
	google.golang.org/grpc => google.golang.org/grpc v1.33.2
)

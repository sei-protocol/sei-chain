module github.com/sei-protocol/sei-chain

go 1.23.0

toolchain go1.23.7

require (
	github.com/BurntSushi/toml v1.4.0
	github.com/CosmWasm/wasmd v0.27.0
	github.com/CosmWasm/wasmvm v1.5.4
	github.com/armon/go-metrics v0.4.1
	github.com/btcsuite/btcd/btcec/v2 v2.3.2
	github.com/cosmos/cosmos-sdk v0.45.10
	github.com/cosmos/go-bip39 v1.0.0
	github.com/cosmos/iavl v0.21.0-alpha.1.0.20230904092046-df3db2d96583
	github.com/cosmos/ibc-go/v3 v3.0.0
	github.com/ethereum/go-ethereum v1.13.2
	github.com/go-playground/validator/v10 v10.11.1
	github.com/gogo/protobuf v1.3.3
	github.com/golang-jwt/jwt/v4 v4.5.1
	github.com/golang/protobuf v1.5.4
	github.com/golangci/golangci-lint v1.46.0
	github.com/gorilla/mux v1.8.0
	github.com/gorilla/websocket v1.5.0
	github.com/grpc-ecosystem/grpc-gateway v1.16.0
	github.com/hashicorp/golang-lru/v2 v2.0.7
	github.com/holiman/uint256 v1.3.2
	github.com/justinas/alice v1.2.0
	github.com/k0kubun/pp/v3 v3.2.0
	github.com/mitchellh/mapstructure v1.5.0
	github.com/pkg/errors v0.9.1
	github.com/rakyll/statik v0.1.7
	github.com/rs/cors v1.8.2
	github.com/rs/zerolog v1.30.0
	github.com/sei-protocol/goutils v0.0.2
	github.com/sei-protocol/sei-db v0.0.27-0.20240123064153-d6dfa112e760
	github.com/spf13/cast v1.5.0
	github.com/spf13/cobra v1.8.1
	github.com/stretchr/testify v1.10.0
	github.com/syndtr/goleveldb v1.0.1-0.20220721030215-126854af5e6d
	github.com/tendermint/tendermint v0.37.0-dev
	github.com/tendermint/tm-db v0.6.8-0.20220519162814-e24b96538a12
	go.opentelemetry.io/otel v1.9.0
	golang.org/x/exp v0.0.0-20231110203233-9a3e6036ecaa
	golang.org/x/sync v0.11.0
	golang.org/x/time v0.9.0
	google.golang.org/genproto/googleapis/api v0.0.0-20241021214115-324edc3d5d38
	google.golang.org/grpc v1.67.1
	google.golang.org/protobuf v1.35.1
	gopkg.in/yaml.v2 v2.4.0
	gopkg.in/yaml.v3 v3.0.1
)

replace (
	github.com/CosmWasm/wasmd => github.com/sei-protocol/sei-wasmd v0.3.9
	github.com/CosmWasm/wasmvm => github.com/sei-protocol/sei-wasmvm v1.5.4-sei.0.0.3
	github.com/btcsuite/btcd => github.com/btcsuite/btcd v0.23.2
	github.com/confio/ics23/go => github.com/cosmos/cosmos-sdk/ics23/go v0.8.0
	github.com/cosmos/cosmos-sdk => github.com/sei-protocol/sei-cosmos v0.3.66
	github.com/cosmos/iavl => github.com/sei-protocol/sei-iavl v0.2.0
	github.com/cosmos/ibc-go/v3 => github.com/sei-protocol/sei-ibc-go/v3 v3.3.6
	github.com/ethereum/go-ethereum => github.com/sei-protocol/go-ethereum v1.15.7-sei-3
	github.com/gogo/protobuf => github.com/regen-network/protobuf v1.3.3-alpha.regen.1
	github.com/sei-protocol/sei-db => github.com/sei-protocol/sei-db v0.0.51
	github.com/syndtr/goleveldb => github.com/syndtr/goleveldb v1.0.1-0.20210819022825-2ae1ddf74ef7
	github.com/tendermint/tendermint => github.com/sei-protocol/sei-tendermint v0.6.1
	github.com/tendermint/tm-db => github.com/sei-protocol/tm-db v0.0.4
	golang.org/x/crypto => golang.org/x/crypto v0.31.0
	google.golang.org/grpc => google.golang.org/grpc v1.33.2
)

package main

import (
	"fmt"
	"math/rand"
	"strings"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/codec"
	"github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"

	"github.com/sei-protocol/sei-chain/utils"
)

const (
	Bank                 string = "bank"
	EVM                  string = "evm"
	ERC20                string = "erc20"
	ERC721               string = "erc721"
	CollectRewards       string = "collect_rewards"
	DistributeRewards    string = "distribute_rewards"
	FailureBankMalformed string = "failure_bank_malformed"
	FailureBankInvalid   string = "failure_bank_invalid"
	FailureDexMalformed  string = "failure_dex_malformed"
	FailureDexInvalid    string = "failure_dex_invalid"
	Dex                  string = "dex"
	Staking              string = "staking"
	Tokenfactory         string = "tokenfactory"
	Limit                string = "limit"
	Market               string = "market"
	WasmMintNft          string = "wasm_mint_nft"
	UNIV2                string = "univ2"
	Vortex               string = "vortex"
	WasmInstantiate      string = "wasm_instantiate"
	WasmOccIteratorWrite string = "wasm_occ_iterator_write"
	WasmOccIteratorRange string = "wasm_occ_iterator_range"
	WasmOccParallelWrite string = "wasm_occ_parallel_write"
)

type WasmIteratorWriteMsg struct {
	Values [][]uint64 `json:"values"`
}

type EVMAddresses struct {
	ERC20        common.Address
	ERC721       common.Address
	UniV2Swapper common.Address
}

type Config struct {
	ChainID            string                `json:"chain_id"`
	GrpcEndpoints      string                `json:"grpc_endpoints"`
	EvmRpcEndpoints    string                `json:"evm_rpc_endpoints"`
	BlockchainEndpoint string                `json:"blockchain_endpoint"`
	NodeURI            string                `json:"node_uri"`
	TargetTps          uint64                `json:"target_tps"`
	MaxAccounts        uint64                `json:"max_accounts"`
	MsgsPerTx          uint64                `json:"msgs_per_tx"`
	MessageTypes       []string              `json:"message_types"`
	PriceDistr         NumericDistribution   `json:"price_distribution"`
	QuantityDistr      NumericDistribution   `json:"quantity_distribution"`
	MsgTypeDistr       MsgTypeDistribution   `json:"message_type_distribution"`
	WasmMsgTypes       WasmMessageTypes      `json:"wasm_msg_types"`
	ContractDistr      ContractDistributions `json:"contract_distribution"`
	PerMessageConfigs  MessageConfigs        `json:"message_configs"`
	MetricsPort        uint64                `json:"metrics_port"`
	TLS                bool                  `json:"tls"`
	SeiTesterAddress   string                `json:"sei_tester_address"`
	PostTxEvmQueries   PostTxEvmQueries      `json:"post_tx_evm_queries"`
	Ticks              uint64                `json:"ticks"`

	// These are dynamically set at startup
	EVMAddresses *EVMAddresses
}

func (c *Config) EVMRpcEndpoint() string {
	endpoints := strings.Split(c.EvmRpcEndpoints, ",")
	return endpoints[0]
}

func (c *Config) ContainsAnyMessageTypes(types ...string) bool {
	for _, t := range types {
		for _, mt := range c.MessageTypes {
			if mt == t {
				return true
			}
		}
	}
	return false
}

type EncodingConfig struct {
	InterfaceRegistry types.InterfaceRegistry
	// NOTE: this field will be renamed to Codec
	Marshaler codec.Codec
	TxConfig  client.TxConfig
	Amino     *codec.LegacyAmino
}

// MessageConfig is the configuration for a message
// Specify the gas and fee for the message type
type MessageTypeConfig struct {
	Gas uint64 `json:"gas"`
	Fee uint64 `json:"fee"`
}

type MessageConfigs map[string]MessageTypeConfig

type NumericDistribution struct {
	Min         sdk.Dec `json:"min"`
	Max         sdk.Dec `json:"max"`
	NumDistinct int64   `json:"number_of_distinct_values"`
}

func (d *NumericDistribution) Sample() sdk.Dec {
	steps := sdk.NewDec(rand.Int63n(d.NumDistinct))
	return d.Min.Add(d.Max.Sub(d.Min).QuoInt64(d.NumDistinct).Mul(steps))
}

// Invalid numeric distribution sample
func (d *NumericDistribution) InvalidSample() sdk.Dec {
	steps := sdk.NewDec(rand.Int63n(d.NumDistinct))
	if rand.Float64() < 0.5 {
		return d.Min.Add(d.Max.Sub(d.Min).QuoInt64(d.NumDistinct).Mul(steps))
	}
	return d.Max.Add(d.Max.Sub(d.Min).QuoInt64(d.NumDistinct).Mul(steps))
}

type DexMsgTypeDistribution struct {
	LimitOrderPct  sdk.Dec `json:"limit_order_percentage"`
	MarketOrderPct sdk.Dec `json:"market_order_percentage"`
}

type StakingMsgTypeDistribution struct {
	DelegatePct        sdk.Dec `json:"delegate_percentage"`
	UndelegatePct      sdk.Dec `json:"undelegate_percentage"`
	BeginRedelegatePct sdk.Dec `json:"begin_redelegate_percentage"`
}
type MsgTypeDistribution struct {
	Dex     DexMsgTypeDistribution     `json:"dex"`
	Staking StakingMsgTypeDistribution `json:"staking"`
}

// Struct containing contract address
// For a specific wasm message type.
// TODO: Abstract interface for any wasm type + execute msg
type WasmMessageTypes struct {
	MintNftType WasmMintNftType     `json:"wasm_mint_nft"`
	Vortex      VortexContract      `json:"vortex"`
	Instantiate WasmInstantiateType `json:"instantiate"`
}

type WasmMintNftType struct {
	ContractAddr string `json:"contract_address"`
}

func (d *MsgTypeDistribution) SampleDexMsgs() string {
	if !d.Dex.LimitOrderPct.Add(d.Dex.MarketOrderPct).Equal(sdk.OneDec()) {
		panic("Distribution percentages must add up to 1")
	}
	randNum := sdk.MustNewDecFromStr(fmt.Sprintf("%f", rand.Float64()))
	if randNum.LT(d.Dex.LimitOrderPct) {
		return Limit
	}
	return Market
}

func (d *MsgTypeDistribution) SampleStakingMsgs() string {
	if !d.Staking.DelegatePct.Add(d.Staking.UndelegatePct).Add(d.Staking.BeginRedelegatePct).Equal(sdk.OneDec()) {
		panic("Distribution percentages must add up to 1")
	}
	randNum := sdk.MustNewDecFromStr(fmt.Sprintf("%f", rand.Float64()))
	if randNum.LT(d.Staking.DelegatePct) {
		return "delegate"
	} else if randNum.LT(d.Staking.DelegatePct.Add(d.Staking.UndelegatePct)) {
		return "undelegate"
	}
	return "begin_redelegate"
}

type ContractDistributions []ContractDistribution

func (d *ContractDistributions) Sample() string {
	if !utils.Reduce(*d, func(i ContractDistribution, o sdk.Dec) sdk.Dec { return o.Add(i.Percentage) }, sdk.ZeroDec()).Equal(sdk.OneDec()) {
		panic("Distribution percentages must add up to 1")
	}
	randNum := sdk.MustNewDecFromStr(fmt.Sprintf("%f", rand.Float64()))
	cumPct := sdk.ZeroDec()
	for _, dist := range *d {
		cumPct = cumPct.Add(dist.Percentage)
		if randNum.LTE(cumPct) {
			return dist.ContractAddr
		}
	}
	panic("this should never be triggered")
}

type ContractDistribution struct {
	ContractAddr string  `json:"contract_address"`
	Percentage   sdk.Dec `json:"percentage"`
}

type VortexContract struct {
	ContractAddr   string `json:"contract_address"`
	NumOrdersPerTx int64  `json:"num_orders_per_tx"`
}

type WasmInstantiateType struct {
	CodeID  uint64 `json:"code_id"`
	Payload string `json:"payload"`
}

type PostTxEvmQueries struct {
	BlockByNumber int `json:"block_by_number"`
	Receipt       int `json:"receipt"`
}

type SignedTx struct {
	TxBytes []byte
	EvmTx   *ethtypes.Transaction
	MsgType string
}

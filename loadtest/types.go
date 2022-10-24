package main

import (
	"fmt"
	"math/rand"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/codec"
	"github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/sei-protocol/sei-chain/utils"
)

const (
	Bank                 string = "bank"
	FailureBankMalformed string = "failure_bank_malformed"
	FailureBankInvalid   string = "failure_bank_invalid"
	FailureDexMalformed  string = "failure_dex_malformed"
	FailureDexInvalid    string = "failure_dex_invalid"
	Dex                  string = "dex"
	Staking              string = "staking"
	Tokenfactory         string = "tokenfactory"
	Limit                string = "limit"
	Market               string = "market"
)

type Config struct {
	ChainID           string                `json:"chain_id"`
	TxsPerBlock       uint64                `json:"txs_per_block"`
	MsgsPerTx         uint64                `json:"msgs_per_tx"`
	Rounds            uint64                `json:"rounds"`
	MessageType       string                `json:"message_type"`
	PriceDistr        NumericDistribution   `json:"price_distribution"`
	QuantityDistr     NumericDistribution   `json:"quantity_distribution"`
	MsgTypeDistr      MsgTypeDistribution   `json:"message_type_distribution"`
	ContractDistr     ContractDistributions `json:"contract_distribution"`
	Constant          bool                  `json:"constant"`
	ConstLoadInterval int64                 `json:"const_load_interval"`
}

type EncodingConfig struct {
	InterfaceRegistry types.InterfaceRegistry
	// NOTE: this field will be renamed to Codec
	Marshaler codec.Codec
	TxConfig  client.TxConfig
	Amino     *codec.LegacyAmino
}

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

type MsgTypeDistribution struct {
	LimitOrderPct  sdk.Dec `json:"limit_order_percentage"`
	MarketOrderPct sdk.Dec `json:"market_order_percentage"`
}

func (d *MsgTypeDistribution) Sample() string {
	if !d.LimitOrderPct.Add(d.MarketOrderPct).Equal(sdk.OneDec()) {
		panic("Distribution percentages must add up to 1")
	}
	randNum := sdk.MustNewDecFromStr(fmt.Sprintf("%f", rand.Float64()))
	if randNum.LT(d.LimitOrderPct) {
		return Limit
	}
	return Market
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

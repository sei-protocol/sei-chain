<!--
order: 2
-->

# State

## ExchangeRateVote

`ExchangeRateVote` containing validator voter's vote for a given denom for the current `VotePeriod`.

- ExchangeRateVote: `0x02<denom_Bytes><valAddress_Bytes> -> amino(ExchangeRateVote)`

```go
type ExchangeRateVote struct {
	ExchangeRate sdk.Dec        // ExchangeRate of Sei in target fiat currency
	Denom        string         // Ticker name of target fiat currency
	Voter        sdk.ValAddress // voter val address of validator
}
```

## ExchangeRate

An `sdk.Dec` that stores the current Sei exchange rate against a given denom, which is used by the [Market](../../market/spec/README.md) module for pricing swaps.

You can get the active list of denoms trading against `Sei` (denominations with votes past `VoteThreshold`) with `k.GetActiveDenoms()`.

- ExchangeRate: `0x03<denom_Bytes> -> amino(sdk.Dec)`

## FeederDelegation

An `sdk.AccAddress` (`terra-` account) address of `operator`'s delegated price feeder.

- FeederDelegation: `0x04<valAddress_Bytes> -> amino(sdk.AccAddress)`

## MissCounter

An `int64` representing the number of `VotePeriods` that validator `operator` missed during the current `SlashWindow`.

- MissCounter: `0x05<valAddress_Bytes> -> amino(int64)`

## AggregateExchangeRateVote

`AggregateExchangeRateVote` containing validator voter's aggregate vote for all denoms for the current `VotePeriod`.

- AggregateExchangeRateVote: `0x07<valAddress_Bytes> -> amino(AggregateExchangeRateVote)`

```go
type ExchangeRateTuple struct {
	Denom        string  `json:"denom"`
	ExchangeRate sdk.Dec `json:"exchange_rate"`
}

type ExchangeRateTuples []ExchangeRateTuple

type AggregateExchangeRateVote struct {
	ExchangeRateTuples ExchangeRateTuples // ExchangeRates of Sei in target fiat currencies
	Voter              sdk.ValAddress     // voter val address of validator
}
```

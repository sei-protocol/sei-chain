package types

import (
	"gopkg.in/yaml.v2"
)

// OracleExchangeRates - array of OracleExchangeRate
type PriceSnapshots []PriceSnapshot

type (
	PriceSnapshotItems []PriceSnapshotItem
	OracleTwaps        []OracleTwap
)

// String implements fmt.Stringer interface
func (snapshots PriceSnapshots) String() string {
	out, _ := yaml.Marshal(snapshots)
	return string(out)
}

// String implements fmt.Stringer interface
func (items PriceSnapshotItems) String() string {
	out, _ := yaml.Marshal(items)
	return string(out)
}

func NewPriceSnapshotItem(denom string, exchangeRate OracleExchangeRate) PriceSnapshotItem {
	return PriceSnapshotItem{
		Denom:              denom,
		OracleExchangeRate: exchangeRate,
	}
}

func NewPriceSnapshot(priceSnapshotItems PriceSnapshotItems, snapshotTimestamp int64) PriceSnapshot {
	return PriceSnapshot{
		SnapshotTimestamp:  snapshotTimestamp,
		PriceSnapshotItems: priceSnapshotItems,
	}
}

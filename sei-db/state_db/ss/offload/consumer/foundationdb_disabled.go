//go:build !foundationdb

package consumer

import "github.com/sei-protocol/sei-chain/sei-db/state_db/ss/offload/historical"

type FoundationDBConfig = historical.FoundationDBConfig

func NewFoundationDBSink(FoundationDBConfig) (Sink, error) {
	return nil, historical.ErrFoundationDBUnavailable
}

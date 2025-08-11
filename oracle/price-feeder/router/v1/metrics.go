package v1

import "github.com/sei-protocol/sei-chain/cosmos-sdk/telemetry"

type Metrics interface {
	Gather(format string) (telemetry.GatherResponse, error)
}

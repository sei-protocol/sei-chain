package v1

import "github.com/cosmos/cosmos-sdk/telemetry"

type Metrics interface {
	Gather(format string) (telemetry.GatherResponse, error)
}

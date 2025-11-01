package types

import (
	"encoding/json"
	"fmt"
)

// MustMarshalCovenant marshals covenant or panics on error.
func MustMarshalCovenant(c SeiNetCovenant) []byte {
	bz, err := json.Marshal(c)
	if err != nil {
		panic(fmt.Sprintf("marshal covenant: %v", err))
	}
	return bz
}

// MustMarshalThreatRecord marshals threat record or panics on error.
func MustMarshalThreatRecord(r SeiGuardianThreatRecord) []byte {
	bz, err := json.Marshal(r)
	if err != nil {
		panic(fmt.Sprintf("marshal threat: %v", err))
	}
	return bz
}

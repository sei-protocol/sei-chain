package types

import (
	"encoding/json"
	"fmt"
	"strings"

	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
)

func (p Plan) String() string {
	due := p.DueAt()
	return fmt.Sprintf(`Upgrade Plan
  Name: %s
  %s
  Info: %s.`, p.Name, due, p.Info)
}

// ValidateBasic does basic validation of a Plan
func (p Plan) ValidateBasic() error {
	if !p.Time.IsZero() {
		return sdkerrors.ErrInvalidRequest.Wrap("time-based upgrades have been deprecated in the SDK")
	}
	if p.UpgradedClientState != nil {
		return sdkerrors.ErrInvalidRequest.Wrap("upgrade logic for IBC has been moved to the IBC module")
	}
	if len(p.Name) == 0 {
		return sdkerrors.Wrap(sdkerrors.ErrInvalidRequest, "name cannot be empty")
	}
	if p.Height <= 0 {
		return sdkerrors.Wrap(sdkerrors.ErrInvalidRequest, "height must be greater than 0")
	}

	return nil
}

// ShouldExecute returns true if the Plan is ready to execute given the current context
func (p Plan) ShouldExecute(ctx sdk.Context) bool {
	if p.Height > 0 {
		return p.Height <= ctx.BlockHeight()
	}
	return false
}

// DueAt is a string representation of when this plan is due to be executed
func (p Plan) DueAt() string {
	return fmt.Sprintf("height: %d", p.Height)
}

// UpgradeDetails is a struct that represents the details of an upgrade
// This is held in the Info object of an upgrade Plan
type UpgradeDetails struct {
	UpgradeType string `json:"upgradeType"`
}

// UpgradeDetails parses and returns a details struct from the Info field of a Plan
// The upgrade.pb.go is generated from proto, so this is separated here
func (p Plan) UpgradeDetails() (UpgradeDetails, error) {
	if p.Info == "" {
		return UpgradeDetails{}, nil
	}
	var details UpgradeDetails
	if err := json.Unmarshal([]byte(p.Info), &details); err != nil {
		// invalid json, assume no upgrade details
		return UpgradeDetails{}, err
	}
	return details, nil
}

// IsMinorRelease returns true if the upgrade is a minor release
func (ud UpgradeDetails) IsMinorRelease() bool {
	return strings.EqualFold(ud.UpgradeType, "minor")
}

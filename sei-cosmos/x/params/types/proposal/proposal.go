package proposal

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/sei-protocol/sei-chain/sei-tendermint/types"

	yaml "gopkg.in/yaml.v2"

	govtypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/gov/types"
)

const (
	// ProposalTypeChange defines the type for a ParameterChangeProposal
	ProposalTypeChange = "ParameterChange"
)

// Assert ParameterChangeProposal implements govtypes.Content at compile-time
var _ govtypes.Content = &ParameterChangeProposal{}

func init() {
	govtypes.RegisterProposalType(ProposalTypeChange)
	govtypes.RegisterProposalTypeCodec(&ParameterChangeProposal{}, "cosmos-sdk/ParameterChangeProposal")
}

func NewParameterChangeProposal(title, description string, changes []ParamChange, isExpedited bool) *ParameterChangeProposal {
	return &ParameterChangeProposal{title, description, changes, isExpedited}
}

// GetTitle returns the title of a parameter change proposal.
func (pcp *ParameterChangeProposal) GetTitle() string { return pcp.Title }

// GetDescription returns the description of a parameter change proposal.
func (pcp *ParameterChangeProposal) GetDescription() string { return pcp.Description }

// ProposalRoute returns the routing key of a parameter change proposal.
func (pcp *ParameterChangeProposal) ProposalRoute() string { return RouterKey }

// ProposalType returns the type of a parameter change proposal.
func (pcp *ParameterChangeProposal) ProposalType() string { return ProposalTypeChange }

// ValidateBasic validates the parameter change proposal
func (pcp *ParameterChangeProposal) ValidateBasic() error {
	err := govtypes.ValidateAbstract(pcp)
	if err != nil {
		return err
	}

	return ValidateChanges(pcp.Changes)
}

// String implements the Stringer interface.
func (pcp ParameterChangeProposal) String() string {
	var b strings.Builder

	fmt.Fprintf(&b, `Parameter Change Proposal:
  Title:       %s
  Description: %s
  Changes:
`, pcp.Title, pcp.Description)

	for _, pc := range pcp.Changes {
		fmt.Fprintf(&b, `    Param Change:
      Subspace: %s
      Key:      %s
      Value:    %X
`, pc.Subspace, pc.Key, pc.Value)
	}

	return b.String()
}

func NewParamChange(subspace, key, value string) ParamChange {
	return ParamChange{subspace, key, value}
}

// String implements the Stringer interface.
func (pc ParamChange) String() string {
	out, _ := yaml.Marshal(pc)
	return string(out)
}

// ValidateChanges performs basic validation checks over a set of ParamChange. It
// returns an error if any ParamChange is invalid.
func ValidateChanges(changes []ParamChange) error {
	if len(changes) == 0 {
		return ErrEmptyChanges
	}

	hasBaseapp := false
	for _, pc := range changes {
		if len(pc.Subspace) == 0 {
			return ErrEmptySubspace
		}
		if len(pc.Key) == 0 {
			return ErrEmptyKey
		}
		if len(pc.Value) == 0 {
			return ErrEmptyValue
		}
		if pc.Subspace == "baseapp" {
			hasBaseapp = true
		}
	}

	// Verify ConsensusParams once for the whole change set. Calling this inside
	// the loop above is O(N^2) for N baseapp entries (each call re-scans and
	// unmarshals all changes). ConsensusParams are otherwise only validated
	// once the proposal passes; invalid values cause a chain halt in ApplyBlock:
	// https://github.com/sei-protocol/sei-tendermint/blob/d426f1fe475eb0c406296770ff5e9f8869b3887e/internal/state/execution.go#L320
	if hasBaseapp {
		if err := verifyConsensusParamsUsingDefault(changes); err != nil {
			return err
		}
	}

	return nil
}

func verifyConsensusParamsUsingDefault(changes []ParamChange) error {
	// Start with a default (valid) set of parameters, and update based on proposal then check
	defaultCP := types.DefaultConsensusParams()
	for _, change := range changes {
		// Note: BlockParams seems to be the only support ConsensusParams available for modifying with proposal
		switch change.Key {
		case "BlockParams":
			blockParams := types.DefaultBlockParams()
			err := json.Unmarshal([]byte(change.Value), &blockParams)
			if err != nil {
				return err
			}
			defaultCP.Block = blockParams
		}
	}
	if err := defaultCP.ValidateConsensusParams(); err != nil {
		return err
	}
	return nil
}

package staking

import "math/big"

type Delegation struct {
	Balance    Balance
	Delegation DelegationDetails
}

type Balance struct {
	Amount *big.Int
	Denom  string
}

type DelegationDetails struct {
	DelegatorAddress string
	Shares           *big.Int
	Decimals         *big.Int
	ValidatorAddress string
}

type ValidatorsResponse struct {
	Validators []Validator
	NextKey    []byte
}

type DelegationsResponse struct {
	Delegations []Delegation
	NextKey     []byte
}

type UnbondingDelegationsResponse struct {
	UnbondingDelegations []UnbondingDelegation
	NextKey              []byte
}

type RedelegationsResponse struct {
	Redelegations []Redelegation
	NextKey       []byte
}

type Validator struct {
	OperatorAddress         string
	ConsensusPubkey         []byte
	Jailed                  bool
	Status                  int32
	Tokens                  string
	DelegatorShares         string
	Description             string
	UnbondingHeight         int64
	UnbondingTime           int64
	CommissionRate          string
	CommissionMaxRate       string
	CommissionMaxChangeRate string
	CommissionUpdateTime    int64
	MinSelfDelegation       string
}

type delegationRecord struct {
	DelegatorAddress string `json:"delegator_address"`
	ValidatorAddress string `json:"validator_address"`
	Amount           string `json:"amount"`
}

type UnbondingDelegationEntry struct {
	CreationHeight int64
	CompletionTime int64
	InitialBalance string
	Balance        string
}

type UnbondingDelegation struct {
	DelegatorAddress string
	ValidatorAddress string
	Entries          []UnbondingDelegationEntry
}

type RedelegationEntry struct {
	CreationHeight int64
	CompletionTime int64
	InitialBalance string
	SharesDst      string
}

type Redelegation struct {
	DelegatorAddress    string
	ValidatorSrcAddress string
	ValidatorDstAddress string
	Entries             []RedelegationEntry
}

type HistoricalInfo struct {
	Height     int64
	Validators []Validator
}

type Pool struct {
	NotBondedTokens string
	BondedTokens    string
}

type Params struct {
	UnbondingTime                      uint64
	MaxValidators                      uint32
	MaxEntries                         uint32
	HistoricalEntries                  uint32
	BondDenom                          string
	MinCommissionRate                  string
	MaxVotingPowerRatio                string
	MaxVotingPowerEnforcementThreshold string
}

type delegationPair struct {
	DelegatorAddress string
	ValidatorAddress string
}

type redelegationTriplet struct {
	DelegatorAddress    string
	ValidatorSrcAddress string
	ValidatorDstAddress string
}

package staking

import (
	"encoding/binary"
	"errors"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
)

const (
	pageLimit  = 100 // x/staking's default
	pageKeyLen = 8
)

var errInvalidPageKey = errors.New("invalid page key")

// The ABI tuples. Field order matches each tuple's component order.
type (
	Description struct {
		Moniker         string
		Identity        string
		Website         string
		SecurityContact string
		Details         string
	}

	Validator struct {
		Operator                common.Address
		ConsensusPubkey         common.Hash
		Jailed                  bool
		Status                  uint8
		Tokens                  *big.Int
		DelegatorShares         *big.Int
		Description             Description
		UnbondingHeight         uint64
		UnbondingTime           uint64
		CommissionRate          *big.Int
		CommissionMaxRate       *big.Int
		CommissionMaxChangeRate *big.Int
		CommissionUpdateTime    uint64
		MinSelfDelegation       *big.Int
	}

	Delegation struct {
		Delegator common.Address
		Validator common.Address
		Shares    *big.Int
		Balance   *big.Int
	}

	UnbondingDelegationEntry struct {
		CreationHeight uint64
		CompletionTime uint64
		InitialBalance *big.Int
		Balance        *big.Int
	}

	UnbondingDelegation struct {
		Delegator common.Address
		Validator common.Address
		Entries   []UnbondingDelegationEntry
	}

	StagedOperation struct {
		Epoch     uint64
		Kind      uint8
		Delegator common.Address
		Validator common.Address
		Amount    *big.Int
		Shares    *big.Int
	}

	Pool struct {
		NotBondedTokens *big.Int
		BondedTokens    *big.Int
	}

	StakingParams struct {
		UnbondingTime                      uint64
		MaxValidators                      uint32
		MaxEntries                         uint32
		MaxVotingPowerRatio                *big.Int
		MaxVotingPowerEnforcementThreshold *big.Int
	}
)

// page returns the half-open range of a total-length index that pageKey
// selects, and the key of the page after it, empty on the last page.
func page(total uint64, pageKey []byte) (start, end uint64, nextKey []byte, err error) {
	return pageFrom(0, total, pageKey)
}

// pageFrom returns the half-open range of the indexes [first, last) that
// pageKey selects, and the key of the page after it, empty on the last page. A
// key below first selects the page starting at first.
func pageFrom(first, last uint64, pageKey []byte) (start, end uint64, nextKey []byte, err error) {
	if err := checkPageKey(pageKey); err != nil {
		return 0, 0, nil, err
	}
	start = first
	if len(pageKey) != 0 {
		start = max(first, binary.BigEndian.Uint64(pageKey))
	}
	if start > last {
		return 0, 0, nil, errInvalidPageKey
	}
	end = last
	if last-start > pageLimit {
		end = start + pageLimit
		nextKey = binary.BigEndian.AppendUint64(nil, end)
	}
	return start, end, nextKey, nil
}

// checkPageKey requires an empty page key or one of pageKeyLen bytes.
func checkPageKey(pageKey []byte) error {
	if len(pageKey) != 0 && len(pageKey) != pageKeyLen {
		return errInvalidPageKey
	}
	return nil
}

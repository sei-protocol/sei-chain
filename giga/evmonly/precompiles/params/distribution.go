package params

import (
	"errors"
	"math/big"

	"github.com/ethereum/go-ethereum/common"

	"github.com/sei-protocol/sei-chain/giga/evmonly/precompiles"
)

// Distribution's flat keys.
const (
	keyWithdrawAddrEnabled = "withdrawaddrenabled"
	keyDAOAddress          = "daoaddress"
)

var distributionSubspace = subspace{
	name: "distribution",
	slot: slotDistribution,
	params: []param{
		{key: keyWithdrawAddrEnabled, kind: boolKind, at: distributionWithdrawAddrEnabled},
		{key: keyDAOAddress, kind: addressKind, at: distributionDAOAddress},
	},
	check: func(v values) error { return distributionFrom(v).Validate() },
}

// Distribution is ParamsStorage.Distribution.
type Distribution struct {
	WithdrawAddrEnabled bool
	DAOAddress          common.Address
}

// ReadDistribution returns distribution's params.
func ReadDistribution(state precompiles.State) Distribution {
	return distributionFrom(distributionSubspace.read(newStore(state)))
}

// distributionDAOAddressParam is the param HasDistribution reads.
var distributionDAOAddressParam = distributionSubspace.mustParam(keyDAOAddress)

// HasDistribution reports whether distribution's params have been written.
func HasDistribution(state precompiles.State) bool {
	return distributionSubspace.get(newStore(state), distributionDAOAddressParam).Sign() != 0
}

var errDistributionWritten = errors.New("distribution's params are already written")

// WriteDistribution writes d as distribution's params, for genesis, if d is
// valid and distribution's params have not been written.
func WriteDistribution(state precompiles.State, d Distribution) error {
	if HasDistribution(state) {
		return errDistributionWritten
	}
	v := d.values()
	if err := distributionSubspace.validate(v); err != nil {
		return err
	}
	store := newStore(state)
	for _, p := range distributionSubspace.params {
		distributionSubspace.set(store, p, v[p.key])
	}
	return nil
}

func distributionFrom(v values) Distribution {
	return Distribution{
		WithdrawAddrEnabled: v[keyWithdrawAddrEnabled].Sign() != 0,
		DAOAddress:          common.BigToAddress(v[keyDAOAddress]),
	}
}

func (d Distribution) values() values {
	enabled := new(big.Int)
	if d.WithdrawAddrEnabled {
		enabled.SetUint64(1)
	}
	return values{
		keyWithdrawAddrEnabled: enabled,
		keyDAOAddress:          new(big.Int).SetBytes(d.DAOAddress.Bytes()),
	}
}

// Validate requires a nonzero DAO address.
func (d Distribution) Validate() error {
	if d.DAOAddress == (common.Address{}) {
		return errors.New("dao_address must not be the zero address")
	}
	return nil
}

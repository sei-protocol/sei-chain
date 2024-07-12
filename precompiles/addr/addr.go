package addr

import (
	"bytes"
	"embed"
	"errors"
	"fmt"
	cryptotypes "github.com/cosmos/cosmos-sdk/crypto/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/sei-protocol/sei-chain/utils"
	"math/big"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/vm"

	"github.com/btcsuite/btcd/btcec"
	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	pcommon "github.com/sei-protocol/sei-chain/precompiles/common"
	"github.com/sei-protocol/sei-chain/utils/metrics"
	"github.com/sei-protocol/sei-chain/x/evm/types"
)

const (
	GetSeiAddressMethod = "getSeiAddr"
	GetEvmAddressMethod = "getEvmAddr"
	AssociateWithGas    = "associateWithGas"
)

const (
	AddrAddress = "0x0000000000000000000000000000000000001004"
)

// Embed abi json file to the executable binary. Needed when importing as dependency.
//
//go:embed abi.json
var f embed.FS

type PrecompileExecutor struct {
	evmKeeper     pcommon.EVMKeeper
	bankKeeper    pcommon.BankKeeper
	accountKeeper pcommon.AccountKeeper

	GetSeiAddressID    []byte
	GetEvmAddressID    []byte
	AssociateWithGasID []byte
}

func NewPrecompile(evmKeeper pcommon.EVMKeeper, bankKeeper pcommon.BankKeeper, accountKeeper pcommon.AccountKeeper) (*pcommon.Precompile, error) {
	abiBz, err := f.ReadFile("abi.json")
	if err != nil {
		return nil, fmt.Errorf("error loading the addr ABI %s", err)
	}

	newAbi, err := abi.JSON(bytes.NewReader(abiBz))
	if err != nil {
		return nil, err
	}

	p := &PrecompileExecutor{
		evmKeeper:     evmKeeper,
		bankKeeper:    bankKeeper,
		accountKeeper: accountKeeper,
	}

	for name, m := range newAbi.Methods {
		switch name {
		case GetSeiAddressMethod:
			p.GetSeiAddressID = m.ID
		case GetEvmAddressMethod:
			p.GetEvmAddressID = m.ID
		case AssociateWithGas:
			p.AssociateWithGasID = m.ID
		}
	}

	return pcommon.NewPrecompile(newAbi, p, common.HexToAddress(AddrAddress), "addr"), nil
}

// RequiredGas returns the required bare minimum gas to execute the precompile.
func (p PrecompileExecutor) RequiredGas(input []byte, method *abi.Method) uint64 {
	return pcommon.DefaultGasCost(input, p.IsTransaction(method.Name))
}

func (p PrecompileExecutor) Execute(ctx sdk.Context, method *abi.Method, _ common.Address, _ common.Address, args []interface{}, value *big.Int, _ bool, _ *vm.EVM) (bz []byte, err error) {
	switch method.Name {
	case GetSeiAddressMethod:
		return p.getSeiAddr(ctx, method, args, value)
	case GetEvmAddressMethod:
		return p.getEvmAddr(ctx, method, args, value)
	case AssociateWithGas:
		return p.associateWithGas(ctx, method, args, value)
	}
	return
}

func (p PrecompileExecutor) getSeiAddr(ctx sdk.Context, method *abi.Method, args []interface{}, value *big.Int) ([]byte, error) {
	if err := pcommon.ValidateNonPayable(value); err != nil {
		return nil, err
	}

	if err := pcommon.ValidateArgsLength(args, 1); err != nil {
		return nil, err
	}

	seiAddr, found := p.evmKeeper.GetSeiAddress(ctx, args[0].(common.Address))
	if !found {
		metrics.IncrementAssociationError("getSeiAddr", types.NewAssociationMissingErr(args[0].(common.Address).Hex()))
		return nil, fmt.Errorf("EVM address %s is not associated", args[0].(common.Address).Hex())
	}
	return method.Outputs.Pack(seiAddr.String())
}

func (p PrecompileExecutor) getEvmAddr(ctx sdk.Context, method *abi.Method, args []interface{}, value *big.Int) ([]byte, error) {
	if err := pcommon.ValidateNonPayable(value); err != nil {
		return nil, err
	}

	if err := pcommon.ValidateArgsLength(args, 1); err != nil {
		return nil, err
	}

	seiAddr, err := sdk.AccAddressFromBech32(args[0].(string))
	if err != nil {
		return nil, err
	}

	evmAddr, found := p.evmKeeper.GetEVMAddress(ctx, seiAddr)
	if !found {
		metrics.IncrementAssociationError("getEvmAddr", types.NewAssociationMissingErr(args[0].(string)))
		return nil, fmt.Errorf("sei address %s is not associated", args[0].(string))
	}
	return method.Outputs.Pack(evmAddr)
}

func (p PrecompileExecutor) associateWithGas(ctx sdk.Context, method *abi.Method, args []interface{}, value *big.Int) ([]byte, error) {
	if err := pcommon.ValidateNonPayable(value); err != nil {
		return nil, err
	}

	if err := pcommon.ValidateArgsLength(args, 4); err != nil {
		return nil, err
	}

	v := args[0].(*big.Int)
	r := args[1].(*big.Int)
	s := args[2].(*big.Int)
	customMessage := args[3].(string)

	// Derive addresses
	v = new(big.Int).Add(v, utils.Big27)

	customMessageHash := crypto.Keccak256Hash([]byte(customMessage))
	evmAddr, seiAddr, pubkey, err := getAddresses(v, r, s, customMessageHash)
	if err != nil {
		return nil, err
	}

	// Check that address is not already associated
	evmAddr, found := p.evmKeeper.GetEVMAddress(ctx, seiAddr)
	if found {
		return nil, fmt.Errorf("address %s is already associated with evm address %s", seiAddr, evmAddr)
	}

	// Associate Addresses:
	err = p.AssociateAddresses(ctx, seiAddr, evmAddr, pubkey)
	if err != nil {
		return nil, err
	}

	return method.Outputs.Pack(true)
}

func (PrecompileExecutor) IsTransaction(string) bool {
	return false
}

// TODO: The methods below are adapted from preprocess.go antehandler. We should find a way to avoid code duplication.
func (p PrecompileExecutor) AssociateAddresses(ctx sdk.Context, seiAddr sdk.AccAddress, evmAddr common.Address, pubkey cryptotypes.PubKey) error {
	p.evmKeeper.SetAddressMapping(ctx, seiAddr, evmAddr)
	if acc := p.accountKeeper.GetAccount(ctx, seiAddr); acc.GetPubKey() == nil {
		if err := acc.SetPubKey(pubkey); err != nil {
			return err
		}
		p.accountKeeper.SetAccount(ctx, acc)
	}
	return p.MigrateBalance(ctx, evmAddr, seiAddr)
}

func (p PrecompileExecutor) MigrateBalance(ctx sdk.Context, evmAddr common.Address, seiAddr sdk.AccAddress) error {
	castAddr := sdk.AccAddress(evmAddr[:])
	castAddrBalances := p.bankKeeper.SpendableCoins(ctx, castAddr)
	if !castAddrBalances.IsZero() {
		if err := p.bankKeeper.SendCoins(ctx, castAddr, seiAddr, castAddrBalances); err != nil {
			return err
		}
	}
	castAddrWei := p.bankKeeper.GetWeiBalance(ctx, castAddr)
	if !castAddrWei.IsZero() {
		if err := p.bankKeeper.SendCoinsAndWei(ctx, castAddr, seiAddr, sdk.ZeroInt(), castAddrWei); err != nil {
			return err
		}
	}
	if p.bankKeeper.LockedCoins(ctx, castAddr).IsZero() {
		p.accountKeeper.RemoveAccount(ctx, authtypes.NewBaseAccountWithAddress(castAddr))
	}
	return nil
}

func getAddresses(V *big.Int, R *big.Int, S *big.Int, data common.Hash) (common.Address, sdk.AccAddress, cryptotypes.PubKey, error) {
	pubkey, err := recoverPubkey(data, R, S, V, true)
	if err != nil {
		return common.Address{}, sdk.AccAddress{}, nil, err
	}
	evmAddr, err := pubkeyToEVMAddress(pubkey)
	if err != nil {
		return common.Address{}, sdk.AccAddress{}, nil, err
	}
	seiPubkey := pubkeyBytesToSeiPubKey(pubkey)
	seiAddr := sdk.AccAddress(seiPubkey.Address())
	return evmAddr, seiAddr, &seiPubkey, nil
}

// first half of go-ethereum/core/types/transaction_signing.go:recoverPlain
func recoverPubkey(sighash common.Hash, R, S, Vb *big.Int, homestead bool) ([]byte, error) {
	if Vb.BitLen() > 8 {
		return []byte{}, ethtypes.ErrInvalidSig
	}
	V := byte(Vb.Uint64() - 27)
	if !crypto.ValidateSignatureValues(V, R, S, homestead) {
		return []byte{}, ethtypes.ErrInvalidSig
	}
	// encode the signature in uncompressed format
	r, s := R.Bytes(), S.Bytes()
	sig := make([]byte, crypto.SignatureLength)
	copy(sig[32-len(r):32], r)
	copy(sig[64-len(s):64], s)
	sig[64] = V
	// recover the public key from the signature
	return crypto.Ecrecover(sighash[:], sig)
}

// second half of go-ethereum/core/types/transaction_signing.go:recoverPlain
func pubkeyToEVMAddress(pub []byte) (common.Address, error) {
	if len(pub) == 0 || pub[0] != 4 {
		return common.Address{}, errors.New("invalid public key")
	}
	var addr common.Address
	copy(addr[:], crypto.Keccak256(pub[1:])[12:])
	return addr, nil
}

func pubkeyBytesToSeiPubKey(pub []byte) secp256k1.PubKey {
	pubKey, _ := crypto.UnmarshalPubkey(pub)
	pubkeyObj := (*btcec.PublicKey)(pubKey)
	return secp256k1.PubKey{Key: pubkeyObj.SerializeCompressed()}
}

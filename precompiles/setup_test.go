package precompiles_test

import (
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/sei-protocol/sei-chain/precompiles"
	pcommon "github.com/sei-protocol/sei-chain/precompiles/common"
	"github.com/sei-protocol/sei-chain/precompiles/distribution"
	"github.com/sei-protocol/sei-chain/precompiles/gov"
	"github.com/sei-protocol/sei-chain/precompiles/slashing"
	"github.com/sei-protocol/sei-chain/precompiles/staking"
	"github.com/sei-protocol/sei-chain/precompiles/utils"
	"github.com/sei-protocol/sei-chain/sei-cosmos/store"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	tmproto "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/types"
	"github.com/sei-protocol/sei-chain/x/evm/state"
	"github.com/stretchr/testify/require"
	tmdb "github.com/tendermint/tm-db"
)

func TestFeegrantPrecompileIsNotRegistered(t *testing.T) {
	const feegrantAddress = "0x0000000000000000000000000000000000001010"

	_, exists := precompiles.GetCustomPrecompiles(&utils.EmptyKeepers{})[common.HexToAddress(feegrantAddress)]
	require.False(t, exists)
}

// TestAuthorizationMethodsAreRetired requires every authorization method the
// staking, gov, distribution and slashing precompiles served to be absent from
// their ABI, so that calling its selector reverts as an unknown method.
func TestAuthorizationMethodsAreRetired(t *testing.T) {
	retired := map[string][]string{
		staking.StakingAddress: {
			"grantStakingAuthorization(address,string[],uint256,int64)",
			"delegateWithAuthorization(address,string)",
			"redelegateWithAuthorization(address,string,string,uint256)",
			"undelegateWithAuthorization(address,string,uint256)",
			"revokeStakingAuthorization(address)",
		},
		gov.GovAddress: {
			"grantVoteAuthorization(address,int64)",
			"grantProposalAuthorization(address,int64)",
			"voteWithAuthorization(address,uint64,int32)",
			"revokeVoteAuthorization(address)",
			"revokeProposalAuthorization(address)",
			"submitProposalWithAuthorization(address,string)",
		},
		distribution.DistrAddress: {
			"grantWithdrawAuthorization(address,int64)",
			"withdrawDelegationRewardsWithAuthorization(address,string)",
			"withdrawValidatorCommissionWithAuthorization(address)",
			"revokeWithdrawAuthorization(address)",
		},
		slashing.SlashingAddress: {
			"grantUnjailAuthorization(address,int64)",
			"unjailWithAuthorization(address)",
			"revokeUnjailAuthorization(address)",
		},
	}
	ctx := sdk.NewContext(store.NewCommitMultiStore(tmdb.NewMemDB()), tmproto.Header{}, false)
	evm := vm.EVM{StateDB: state.NewDBImpl(ctx, retiredTestStateKeeper{}, true)}
	custom := precompiles.GetCustomPrecompiles(retiredTestKeepers{EmptyKeepers: &utils.EmptyKeepers{}})

	for address, signatures := range retired {
		precompile, ok := custom[common.HexToAddress(address)].(abiPrecompile)
		require.True(t, ok, "%s is not a dynamic gas precompile", address)
		for _, signature := range signatures {
			name, _, _ := strings.Cut(signature, "(")
			t.Run(name, func(t *testing.T) {
				contractABI := precompile.GetABI()
				require.NotContains(t, contractABI.Methods, name)
				selector := crypto.Keccak256([]byte(signature))[:4]
				_, err := contractABI.MethodById(selector)
				require.Error(t, err, "the selector of %s still resolves", signature)

				_, remainingGas, err := precompile.RunAndCalculateGas(&evm, common.Address{}, common.Address{}, selector, 1_000_000, nil, nil, false, false)
				require.ErrorIs(t, err, vm.ErrExecutionReverted)
				require.Zero(t, remainingGas, "the precompile still executed %s", name)
			})
		}
	}
}

type abiPrecompile interface {
	vm.DynamicGasPrecompiledContract
	GetABI() abi.ABI
}

type retiredTestKeepers struct {
	*utils.EmptyKeepers
}

func (retiredTestKeepers) EVMK() utils.EVMKeeper { return retiredTestEVMKeeper{} }

type retiredTestEVMKeeper struct {
	utils.EVMKeeper
}

func (retiredTestEVMKeeper) GetCosmosGasLimitFromEVMGas(_ sdk.Context, evmGas uint64) uint64 {
	return evmGas
}

func (retiredTestEVMKeeper) GetEVMGasLimitFromCtx(sdk.Context) uint64 { return 1_000_000 }

type retiredTestStateKeeper struct {
	state.EVMKeeper
}

func (retiredTestStateKeeper) GetFeeCollectorAddress(sdk.Context) (common.Address, error) {
	return common.Address{}, nil
}

// TestIsTransactionMatchesABIStateMutability verifies that every precompile
// executor exposing IsTransaction agrees with its ABI: exactly the non-view
// methods are transactions. Several Execute implementations rely on this
// classification to run views on a branched, discarded context, so a method
// misclassified as a view would have its state changes silently dropped (and
// a query misclassified as a transaction would leak querier side effects).
func TestIsTransactionMatchesABIStateMutability(t *testing.T) {
	for addr, precompile := range precompiles.GetCustomPrecompiles(&utils.EmptyKeepers{}) {
		var (
			name     string
			executor interface{}
			methods  map[string]bool // method name -> is view per abi
		)
		switch p := precompile.(type) {
		case *pcommon.DynamicGasPrecompile:
			name, executor = p.GetName(), p.GetExecutor()
			methods = viewMethods(p.GetABI().Methods)
		case *pcommon.Precompile:
			name, executor = p.GetName(), p.GetExecutor()
			methods = viewMethods(p.GetABI().Methods)
		default:
			t.Fatalf("unexpected precompile wrapper type %T at %s", precompile, addr.Hex())
		}
		classifier, ok := executor.(interface{ IsTransaction(string) bool })
		if !ok {
			// fully dynamic precompiles don't classify their methods
			continue
		}
		for method, isView := range methods {
			require.Equalf(t, !isView, classifier.IsTransaction(method),
				"%s: IsTransaction(%q) disagrees with the abi stateMutability", name, method)
		}
	}
}

func viewMethods(methods map[string]abi.Method) map[string]bool {
	out := make(map[string]bool, len(methods))
	for name, m := range methods {
		out[name] = m.StateMutability == "view" || m.StateMutability == "pure"
	}
	return out
}

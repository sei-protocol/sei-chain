package precompiles_test

import (
	"testing"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/sei-protocol/sei-chain/precompiles"
	pcommon "github.com/sei-protocol/sei-chain/precompiles/common"
	"github.com/sei-protocol/sei-chain/precompiles/utils"
	"github.com/stretchr/testify/require"
)

// TestIsTransactionMatchesABIStateMutability verifies that every precompile
// executor exposing IsTransaction agrees with its ABI: exactly the non-view
// methods are transactions. Several Execute implementations rely on this
// classification to run views on a branched, discarded context, so a method
// misclassified as a view would have its state changes silently dropped (and
// a query misclassified as a transaction would leak querier side effects).
func TestIsTransactionMatchesABIStateMutability(t *testing.T) {
	const latest = "latest"
	for addr, versioned := range precompiles.GetCustomPrecompiles(latest, &utils.EmptyKeepers{}) {
		var (
			name     string
			executor interface{}
			methods  map[string]bool // method name -> is view per abi
		)
		switch p := versioned[latest].(type) {
		case *pcommon.DynamicGasPrecompile:
			name, executor = p.GetName(), p.GetExecutor()
			methods = viewMethods(p.GetABI().Methods)
		case *pcommon.Precompile:
			name, executor = p.GetName(), p.GetExecutor()
			methods = viewMethods(p.GetABI().Methods)
		default:
			t.Fatalf("unexpected precompile wrapper type %T at %s", versioned[latest], addr.Hex())
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

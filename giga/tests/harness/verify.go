package harness

import (
	"bytes"
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	evmkeeper "github.com/sei-protocol/sei-chain/x/evm/keeper"
)

// VerifyPostState verifies that the actual state matches the expected post-state from the fixture.
// This follows the same pattern as x/evm/keeper/replay.go:VerifyAccount() but adapted for test assertions.
// Note: Balance verification is skipped due to Sei's gas refund modifications
// (see https://github.com/sei-protocol/go-ethereum/pull/32)
func VerifyPostState(t *testing.T, ctx sdk.Context, keeper *evmkeeper.Keeper, expectedState ethtypes.GenesisAlloc, executorName string) {
	for addr, expectedAccount := range expectedState {
		// Verify storage
		for key, expectedValue := range expectedAccount.Storage {
			actualValue := keeper.GetState(ctx, addr, key)
			if !bytes.Equal(expectedValue.Bytes(), actualValue.Bytes()) {
				t.Errorf("%s: storage mismatch for %s key %s: expected %s, got %s",
					executorName, addr.Hex(), key.Hex(), expectedValue.Hex(), actualValue.Hex())
			}
		}

		// Verify nonce
		actualNonce := keeper.GetNonce(ctx, addr)
		if expectedAccount.Nonce != actualNonce {
			t.Errorf("%s: nonce mismatch for %s: expected %d, got %d",
				executorName, addr.Hex(), expectedAccount.Nonce, actualNonce)
		}

		// Verify code
		actualCode := keeper.GetCode(ctx, addr)
		if !bytes.Equal(expectedAccount.Code, actualCode) {
			t.Errorf("%s: code mismatch for %s: expected %d bytes, got %d bytes",
				executorName, addr.Hex(), len(expectedAccount.Code), len(actualCode))
		}

		// Note: Balance verification is intentionally skipped due to Sei-specific gas handling
		// (limiting EVM max refund to 150% of used gas)
	}
}

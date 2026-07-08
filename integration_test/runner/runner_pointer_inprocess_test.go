//go:build inprocess

// In-process arm for the hardhat EVM-interoperability suites (CW<->EVM pointers +
// misc). Each suite self-seeds through the shimmed seid (store/instantiate CW,
// register pointers, tokenfactory) — no Go seeder. One hardhat invocation per file
// (via the shared runHardhatEVM in runner_evm_inprocess_test.go), mirroring
// evm_interoperability_{pointer,misc}_tests.sh (fresh mocha state each).
//
// Ordering: the pointer suites store wasm (cw20/721/1155_base), so this file's name
// must sort AFTER runner_inprocess_test.go, whose TestInProcessSeiDBModule asserts a
// pristine max_code_id==3 baseline. See runner_precompile_inprocess_test.go.
package runner_test

import "testing"

// TestInProcessEVMInteropPointer runs the 7 CW<->EVM pointer suites (both directions
// for ERC20/721/1155 plus ERC20<->native). The ERC->CW/native helpers submit EVM txs,
// so the shimmed seid needs --evm-rpc, which InProcessEVMEnv supplies via SEI_EVM_RPC.
func TestInProcessEVMInteropPointer(t *testing.T) {
	for _, f := range []string{
		"CW20toERC20PointerTest.js", "ERC20toCW20PointerTest.js", "ERC20toNativePointerTest.js",
		"CW721toERC721PointerTest.js", "ERC721toCW721PointerTest.js",
		"CW1155toERC1155PointerTest.js", "ERC1155toCW1155PointerTest.js",
	} {
		t.Run(f, func(t *testing.T) { runHardhatEVM(t, f) })
	}
}

// TestInProcessEVMInteropMisc runs the SeiSolo/SetCodeTx/TransientStorage suites.
// SetCodeTxTest uses a viem client over WebSocket, so it needs SEI_EVM_WS repointed.
func TestInProcessEVMInteropMisc(t *testing.T) {
	wsEnv := "SEI_EVM_WS=" + sharedNet.Node(0).EVMWS()
	for _, f := range []string{"SeiSoloTest.js", "SetCodeTxTest.js", "TransientStorageTest.js"} {
		t.Run(f, func(t *testing.T) { runHardhatEVM(t, f, wsEnv) })
	}
}

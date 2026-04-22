package cryptosim

import (
	"testing"

	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/sei-protocol/sei-chain/sei-db/common/keys"
	crand "github.com/sei-protocol/sei-chain/sei-db/common/rand"
)

func makeTestKeys(t *testing.T) (feeAccount, srcAccount, dstAccount, senderSlot, receiverSlot, erc20Contract []byte) {
	t.Helper()
	keyRand := crand.NewCannedRandom(4096, 1)

	feeAccount = keys.BuildEVMKey(keys.EVMKeyCodeHash, keyRand.Address(accountPrefix, 0, keys.AddressLen))
	srcAddr := keyRand.Address(accountPrefix, 1, keys.AddressLen)
	srcAccount = keys.BuildEVMKey(keys.EVMKeyCodeHash, srcAddr)
	dstAddr := keyRand.Address(accountPrefix, 2, keys.AddressLen)
	dstAccount = keys.BuildEVMKey(keys.EVMKeyCodeHash, dstAddr)

	senderSlotBytes := make([]byte, StorageKeyLen)
	copy(senderSlotBytes[:keys.AddressLen], srcAddr)
	copy(senderSlotBytes[keys.AddressLen:], keyRand.SeededBytes(SlotLen, 11))
	senderSlot = keys.BuildEVMKey(keys.EVMKeyStorage, senderSlotBytes)

	receiverSlotBytes := make([]byte, StorageKeyLen)
	copy(receiverSlotBytes[:keys.AddressLen], dstAddr)
	copy(receiverSlotBytes[keys.AddressLen:], keyRand.SeededBytes(SlotLen, 12))
	receiverSlot = keys.BuildEVMKey(keys.EVMKeyStorage, receiverSlotBytes)

	erc20Contract = keys.BuildEVMKey(keys.EVMKeyCode, keyRand.Address(contractPrefix, 0, keys.AddressLen))
	return
}

func TestBuildERC20TransferReceipt(t *testing.T) {
	cr := crand.NewCannedRandom(1<<20, 42)
	feeAccount, srcAccount, dstAccount, senderSlot, receiverSlot, erc20Contract := makeTestKeys(t)

	receipt, err := BuildERC20TransferReceipt(
		cr, feeAccount, srcAccount, dstAccount,
		senderSlot, receiverSlot, erc20Contract,
		1_000_000, 0,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if receipt.Status != uint32(ethtypes.ReceiptStatusSuccessful) {
		t.Errorf("expected successful status, got %d", receipt.Status)
	}
	if receipt.BlockNumber != 1_000_000 {
		t.Errorf("expected block number 1000000, got %d", receipt.BlockNumber)
	}
	if len(receipt.Logs) != 1 {
		t.Fatalf("expected 1 log, got %d", len(receipt.Logs))
	}
	if receipt.Logs[0].Topics[0] != erc20TransferEventSignatureHex {
		t.Error("first log topic should be ERC20 Transfer event signature")
	}

	// Receipt must be marshallable (used by the write path).
	data, err := receipt.Marshal()
	if err != nil {
		t.Fatalf("failed to marshal receipt: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("marshalled receipt is empty")
	}
}

func TestBuildERC20TransferReceipt_InvalidInputs(t *testing.T) {
	cr := crand.NewCannedRandom(1<<20, 42)
	feeAccount, srcAccount, dstAccount, senderSlot, receiverSlot, erc20Contract := makeTestKeys(t)

	if _, err := BuildERC20TransferReceipt(nil, feeAccount, srcAccount, dstAccount, senderSlot, receiverSlot, erc20Contract, 1_000_000, 0); err == nil {
		t.Error("expected error for nil CannedRandom")
	}
	if _, err := BuildERC20TransferReceipt(cr, []byte("bad"), srcAccount, dstAccount, senderSlot, receiverSlot, erc20Contract, 1_000_000, 0); err == nil {
		t.Error("expected error for invalid fee account key")
	}
	if _, err := BuildERC20TransferReceipt(cr, feeAccount, srcAccount, dstAccount, []byte("bad"), receiverSlot, erc20Contract, 1_000_000, 0); err == nil {
		t.Error("expected error for invalid sender slot key")
	}
}

func TestSyntheticTxHashDeterminism(t *testing.T) {
	crand1 := crand.NewCannedRandom(1<<20, 42)
	crand2 := crand.NewCannedRandom(1<<20, 42)

	block := uint64(500_000)
	txIdx := uint32(7)

	hash1 := SyntheticTxHash(crand1, block, txIdx)
	hash2 := SyntheticTxHash(crand2, block, txIdx)

	if len(hash1) != 32 {
		t.Fatalf("expected 32 bytes, got %d", len(hash1))
	}
	for i := range hash1 {
		if hash1[i] != hash2[i] {
			t.Fatal("same (seed, bufferSize, block, txIdx) must produce identical hashes")
		}
	}

	// Same call again on the same instance must be stable (SeededBytes is stateless).
	hash3 := SyntheticTxHash(crand1, block, txIdx)
	for i := range hash1 {
		if hash1[i] != hash3[i] {
			t.Fatal("repeated calls with same inputs must return identical hashes")
		}
	}

	// Different (block, txIdx) must produce a different hash.
	other := SyntheticTxHash(crand1, block, txIdx+1)
	same := true
	for i := range hash1 {
		if hash1[i] != other[i] {
			same = false
			break
		}
	}
	if same {
		t.Fatal("different (block, txIdx) should produce different hashes")
	}
}

// Regression test: account keys with EVMKeyCode prefix must still be accepted.
func TestBuildERC20TransferReceipt_EVMKeyCodeAccounts(t *testing.T) {
	cr := crand.NewCannedRandom(1<<20, 42)
	keyRand := crand.NewCannedRandom(4096, 1)

	feeAccount := keys.BuildEVMKey(keys.EVMKeyCode, keyRand.Address(accountPrefix, 0, keys.AddressLen))
	srcAddr := keyRand.Address(accountPrefix, 1, keys.AddressLen)
	srcAccount := keys.BuildEVMKey(keys.EVMKeyCode, srcAddr)
	dstAccount := keys.BuildEVMKey(keys.EVMKeyCode, keyRand.Address(accountPrefix, 2, keys.AddressLen))

	senderSlotBytes := make([]byte, StorageKeyLen)
	copy(senderSlotBytes[:keys.AddressLen], srcAddr)
	copy(senderSlotBytes[keys.AddressLen:], keyRand.SeededBytes(SlotLen, 11))
	senderSlot := keys.BuildEVMKey(keys.EVMKeyStorage, senderSlotBytes)

	receiverSlotBytes := make([]byte, StorageKeyLen)
	copy(receiverSlotBytes[:keys.AddressLen], keyRand.Address(accountPrefix, 2, keys.AddressLen))
	copy(receiverSlotBytes[keys.AddressLen:], keyRand.SeededBytes(SlotLen, 12))
	receiverSlot := keys.BuildEVMKey(keys.EVMKeyStorage, receiverSlotBytes)

	erc20Contract := keys.BuildEVMKey(keys.EVMKeyCode, keyRand.Address(contractPrefix, 0, keys.AddressLen))

	_, err := BuildERC20TransferReceipt(cr, feeAccount, srcAccount, dstAccount, senderSlot, receiverSlot, erc20Contract, 1_000_000, 0)
	if err != nil {
		t.Fatalf("EVMKeyCode accounts should be accepted: %v", err)
	}
}

// Regression test: uses the exact key formats produced by data_generator.go
// (EVMKeyCodeHash for accounts, EVMKeyStorage with full StorageKeyLen payload).
func TestBuildERC20TransferReceipt_DataGeneratorKeyFormats(t *testing.T) {
	cr := crand.NewCannedRandom(1<<20, 42)
	keyRand := crand.NewCannedRandom(4096, 1)

	feeAccount := keys.BuildEVMKey(keys.EVMKeyCodeHash, keyRand.Address(accountPrefix, 0, keys.AddressLen))
	srcAccount := keys.BuildEVMKey(keys.EVMKeyCodeHash, keyRand.Address(accountPrefix, 1, keys.AddressLen))
	dstAccount := keys.BuildEVMKey(keys.EVMKeyCodeHash, keyRand.Address(accountPrefix, 2, keys.AddressLen))

	senderSlot := keys.BuildEVMKey(keys.EVMKeyStorage, keyRand.Address(ethStoragePrefix, 10, StorageKeyLen))
	receiverSlot := keys.BuildEVMKey(keys.EVMKeyStorage, keyRand.Address(ethStoragePrefix, 20, StorageKeyLen))
	erc20Contract := keys.BuildEVMKey(keys.EVMKeyCode, keyRand.Address(contractPrefix, 0, keys.AddressLen))

	receipt, err := BuildERC20TransferReceipt(cr, feeAccount, srcAccount, dstAccount, senderSlot, receiverSlot, erc20Contract, 1_000_000, 0)
	if err != nil {
		t.Fatalf("data_generator key formats should be accepted: %v", err)
	}
	if receipt.Status != uint32(ethtypes.ReceiptStatusSuccessful) {
		t.Errorf("expected successful status, got %d", receipt.Status)
	}
}

func BenchmarkBuildERC20TransferReceipt(b *testing.B) {
	keyRand := crand.NewCannedRandom(4096, 1)
	receiptRand := crand.NewCannedRandom(1<<20, 2)

	feeAccount := keys.BuildEVMKey(keys.EVMKeyCodeHash, keyRand.Address(accountPrefix, 0, keys.AddressLen))
	srcAddr := keyRand.Address(accountPrefix, 1, keys.AddressLen)
	srcAccount := keys.BuildEVMKey(keys.EVMKeyCodeHash, srcAddr)
	dstAddr := keyRand.Address(accountPrefix, 2, keys.AddressLen)
	dstAccount := keys.BuildEVMKey(keys.EVMKeyCodeHash, dstAddr)

	senderSlotBytes := make([]byte, StorageKeyLen)
	copy(senderSlotBytes[:keys.AddressLen], srcAddr)
	copy(senderSlotBytes[keys.AddressLen:], keyRand.SeededBytes(SlotLen, 11))
	senderSlot := keys.BuildEVMKey(keys.EVMKeyStorage, senderSlotBytes)

	receiverSlotBytes := make([]byte, StorageKeyLen)
	copy(receiverSlotBytes[:keys.AddressLen], dstAddr)
	copy(receiverSlotBytes[keys.AddressLen:], keyRand.SeededBytes(SlotLen, 12))
	receiverSlot := keys.BuildEVMKey(keys.EVMKeyStorage, receiverSlotBytes)

	erc20Contract := keys.BuildEVMKey(keys.EVMKeyCode, keyRand.Address(contractPrefix, 0, keys.AddressLen))

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := BuildERC20TransferReceipt(receiptRand, feeAccount, srcAccount, dstAccount, senderSlot, receiverSlot, erc20Contract, syntheticReceiptMinBlockNumber, 0)
		if err != nil {
			b.Fatal(err)
		}
	}
}

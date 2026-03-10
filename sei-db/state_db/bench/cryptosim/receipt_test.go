package cryptosim

import (
	"testing"

	"github.com/sei-protocol/sei-chain/sei-db/common/evm"
)

func BenchmarkBuildERC20TransferReceipt(b *testing.B) {
	keyRand := NewCannedRandom(4096, 1)
	receiptRand := NewCannedRandom(1<<20, 2)

	feeAccount := evm.BuildMemIAVLEVMKey(evm.EVMKeyCode, keyRand.Address(accountPrefix, 0, AddressLen))

	srcAccountAddress := keyRand.Address(accountPrefix, 1, AddressLen)
	srcAccount := evm.BuildMemIAVLEVMKey(evm.EVMKeyCode, srcAccountAddress)

	dstAccountAddress := keyRand.Address(accountPrefix, 2, AddressLen)
	dstAccount := evm.BuildMemIAVLEVMKey(evm.EVMKeyCode, dstAccountAddress)

	senderSlotBytes := make([]byte, StorageKeyLen)
	copy(senderSlotBytes[:AddressLen], srcAccountAddress)
	copy(senderSlotBytes[AddressLen:], keyRand.SeededBytes(SlotLen, 11))
	senderSlot := evm.BuildMemIAVLEVMKey(evm.EVMKeyStorage, senderSlotBytes)

	receiverSlotBytes := make([]byte, StorageKeyLen)
	copy(receiverSlotBytes[:AddressLen], dstAccountAddress)
	copy(receiverSlotBytes[AddressLen:], keyRand.SeededBytes(SlotLen, 12))
	receiverSlot := evm.BuildMemIAVLEVMKey(evm.EVMKeyStorage, receiverSlotBytes)

	erc20Contract := evm.BuildMemIAVLEVMKey(evm.EVMKeyCode, keyRand.Address(contractPrefix, 0, AddressLen))
	blockNumber := syntheticReceiptMinBlockNumber
	txIndex := uint32(0)

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		receipt, err := BuildERC20TransferReceipt(
			receiptRand,
			feeAccount,
			srcAccount,
			dstAccount,
			senderSlot,
			receiverSlot,
			erc20Contract,
			blockNumber,
			txIndex,
		)
		if err != nil {
			b.Fatalf("BuildERC20TransferReceipt failed: %v", err)
		}
		if len(receipt.Logs) != 1 {
			b.Fatalf("expected 1 log, got %d", len(receipt.Logs))
		}
	}
}

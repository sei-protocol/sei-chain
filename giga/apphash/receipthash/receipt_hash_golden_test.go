package receipthash

import (
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/stretchr/testify/require"
)

// recordReceiptHashGolden rewrites the current version's golden file from this build instead of verifying against it.
// Refused on CI: see writeReceiptHashGoldenFile().
var recordReceiptHashGolden = flag.Bool("receipt-hash-golden-record", false,
	"rewrite the current version's receipt hash golden file from this build instead of verifying against it")

// receiptHashGoldenCounts are the receipt counts of the recorded blocks, in record order.
var receiptHashGoldenCounts = []int{0, 1, 2, 3, 5, 8, 17}

const (
	// receiptHashGoldenSeed drives the random receipts of the recorded blocks.
	receiptHashGoldenSeed = 0x2ece_1b75_5eed

	// receiptHashGoldenDir holds one golden file per receipt hash version.
	receiptHashGoldenDir = "testdata/golden"
)

// receiptHashGoldenFile is the contents of one golden file.
type receiptHashGoldenFile struct {
	// The receipt hash version every record in this file was produced under.
	ReceiptHashVersion uint8 `json:"receiptHashVersion"`

	// The recorded blocks.
	Records []receiptHashGoldenRecord `json:"records"`
}

// receiptHashGoldenRecord is one recorded block: its receipts and its receipt hash. Byte fields are lowercase hex.
type receiptHashGoldenRecord struct {
	// The block's receipts, in block order.
	Receipts []receiptHashGoldenReceipt `json:"receipts"`

	// The block's receipt hash.
	ReceiptHash string `json:"receiptHash"`
}

// receiptHashGoldenReceipt is one recorded receipt. Integers are decimal strings, so readers without exact 64-bit
// JSON numbers keep every digit, and byte fields are lowercase hex.
type receiptHashGoldenReceipt struct {
	// The receipt's status.
	Status string `json:"status"`

	// The hash of the receipt's transaction.
	TxHash string `json:"txHash"`

	// The transaction's position in the block, counting every transaction.
	BlockIndex string `json:"blockIndex"`

	// The gas used by the transaction alone.
	GasUsed string `json:"gasUsed"`

	// The logs the transaction emitted, in emission order.
	Logs []receiptHashGoldenLog `json:"logs"`
}

// receiptHashGoldenLog is one recorded log. Byte fields are lowercase hex.
type receiptHashGoldenLog struct {
	// The account that emitted the log.
	Address string `json:"address"`

	// The log's topics.
	Topics []string `json:"topics"`

	// The log's data.
	Data string `json:"data"`
}

// TestReceiptHashGolden requires the current version's golden file to verify against this build.
func TestReceiptHashGolden(t *testing.T) {
	path := receiptHashGoldenFilePath(receiptHashVersion)
	if *recordReceiptHashGolden {
		recordReceiptHashGoldenFile(t, path)
		return
	}

	content, err := os.ReadFile(path)
	require.NoError(t, err,
		"no golden file for the current version; record %s with -receipt-hash-golden-record", path)
	var file receiptHashGoldenFile
	require.NoError(t, json.Unmarshal(content, &file), "parsing %s", path)
	require.Equal(t, receiptHashVersion, file.ReceiptHashVersion)
	require.Len(t, file.Records, len(receiptHashGoldenCounts))

	for i, record := range file.Records {
		receipts := make([]*ethtypes.Receipt, 0, len(record.Receipts))
		for j, recorded := range record.Receipts {
			receipts = append(receipts, decodeReceiptHashGoldenReceipt(t, recorded, i, j))
		}
		receiptHash := receiptHashOf(t, receipts)
		require.Equal(t, record.ReceiptHash, hex.EncodeToString(receiptHash[:]), "record %d", i)
	}
}

// decodeReceiptHashGoldenReceipt decodes one recorded receipt, failing the test if a field does not parse.
func decodeReceiptHashGoldenReceipt(
	t *testing.T,
	// The recorded receipt.
	recorded receiptHashGoldenReceipt,
	// The position of the receipt's record in the golden file.
	recordIndex int,
	// The position of the receipt in its record.
	receiptIndex int,
) *ethtypes.Receipt {
	t.Helper()

	context := fmt.Sprintf("record %d receipt %d", recordIndex, receiptIndex)
	status, err := strconv.ParseUint(recorded.Status, 10, 64)
	require.NoError(t, err, context)
	blockIndex, err := strconv.ParseUint(recorded.BlockIndex, 10, 64)
	require.NoError(t, err, context)
	gasUsed, err := strconv.ParseUint(recorded.GasUsed, 10, 64)
	require.NoError(t, err, context)
	receipt := &ethtypes.Receipt{
		Status:           status,
		TxHash:           common.BytesToHash(decodeReceiptHashGoldenHex(t, recorded.TxHash, context)),
		TransactionIndex: uint(blockIndex),
		GasUsed:          gasUsed,
		Logs:             make([]*ethtypes.Log, 0, len(recorded.Logs)),
	}
	for _, recordedLog := range recorded.Logs {
		log := &ethtypes.Log{
			Address: common.BytesToAddress(decodeReceiptHashGoldenHex(t, recordedLog.Address, context)),
			Topics:  make([]common.Hash, 0, len(recordedLog.Topics)),
			Data:    decodeReceiptHashGoldenHex(t, recordedLog.Data, context),
		}
		for _, topic := range recordedLog.Topics {
			topicBytes := decodeReceiptHashGoldenHex(t, topic, context)
			log.Topics = append(log.Topics, common.BytesToHash(topicBytes))
		}
		receipt.Logs = append(receipt.Logs, log)
	}
	return receipt
}

// decodeReceiptHashGoldenHex decodes one recorded byte field, failing the test if it does not parse.
func decodeReceiptHashGoldenHex(t *testing.T, recorded string, context string) []byte {
	t.Helper()

	decoded, err := hex.DecodeString(recorded)
	require.NoError(t, err, "%s: %q", context, recorded)
	return decoded
}

// receiptHashGoldenFilePath returns the path of the golden file for a receipt hash version.
func receiptHashGoldenFilePath(version uint8) string {
	return filepath.Join(receiptHashGoldenDir, fmt.Sprintf("receipt-hash-v%d.json", version))
}

// recordReceiptHashGoldenFile rewrites the current version's golden file from seeded random blocks.
func recordReceiptHashGoldenFile(t *testing.T, path string) {
	t.Helper()

	rng := rand.New(rand.NewSource(receiptHashGoldenSeed))
	file := receiptHashGoldenFile{
		ReceiptHashVersion: receiptHashVersion,
		Records:            make([]receiptHashGoldenRecord, 0, len(receiptHashGoldenCounts)),
	}
	for _, count := range receiptHashGoldenCounts {
		file.Records = append(file.Records, newReceiptHashGoldenRecord(t, randomReceipts(rng, count)))
	}
	writeReceiptHashGoldenFile(t, path, file)
}

// newReceiptHashGoldenRecord returns the golden record of one block's receipts.
func newReceiptHashGoldenRecord(t *testing.T, receipts []*ethtypes.Receipt) receiptHashGoldenRecord {
	t.Helper()

	receiptHash := receiptHashOf(t, receipts)
	record := receiptHashGoldenRecord{
		Receipts:    make([]receiptHashGoldenReceipt, 0, len(receipts)),
		ReceiptHash: hex.EncodeToString(receiptHash[:]),
	}
	for _, receipt := range receipts {
		recorded := receiptHashGoldenReceipt{
			Status:     strconv.FormatUint(receipt.Status, 10),
			TxHash:     hex.EncodeToString(receipt.TxHash[:]),
			BlockIndex: strconv.FormatUint(uint64(receipt.TransactionIndex), 10),
			GasUsed:    strconv.FormatUint(receipt.GasUsed, 10),
			Logs:       make([]receiptHashGoldenLog, 0, len(receipt.Logs)),
		}
		for _, log := range receipt.Logs {
			recordedLog := receiptHashGoldenLog{
				Address: hex.EncodeToString(log.Address[:]),
				Topics:  make([]string, 0, len(log.Topics)),
				Data:    hex.EncodeToString(log.Data),
			}
			for _, topic := range log.Topics {
				recordedLog.Topics = append(recordedLog.Topics, hex.EncodeToString(topic[:]))
			}
			recorded.Logs = append(recorded.Logs, recordedLog)
		}
		record.Receipts = append(record.Receipts, recorded)
	}
	return record
}

// writeReceiptHashGoldenFile writes a golden file. It refuses to run on CI.
func writeReceiptHashGoldenFile(t *testing.T, path string, file receiptHashGoldenFile) {
	t.Helper()

	if os.Getenv("CI") != "" {
		t.Fatal("refusing to rewrite a receipt hash golden file on CI: " +
			"the recorded values are the expected values, so a build that rewrites them verifies nothing")
	}
	content, err := json.MarshalIndent(file, "", "  ")
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, append(content, '\n'), 0o644))
	t.Logf("recorded %d records into %s; review the diff before committing", len(file.Records), path)
}

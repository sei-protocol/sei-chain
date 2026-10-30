package receipthash

import (
	"encoding/hex"
	"strings"
	"testing"

	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/giga/apphash/merkle"
)

// TestReceiptHashSpecVector pins every value of the test vectors in receipt_hash_spec.md.
func TestReceiptHashSpecVector(t *testing.T) {
	zeros := strings.Repeat("00", 32)
	logLeaves := []string{
		"1111111111111111111111111111111111111111" + "01" + strings.Repeat("22", 32) + zeros + zeros + zeros +
			"beef",
		"3333333333333333333333333333333333333333" + "00" + zeros + zeros + zeros + zeros,
	}
	wantLogLeafHashes := []string{
		"6a48e5f7482a83cce3f20dc483d0a13ccc1957972854f3404e493177c1c663d5",
		"17cfcd0320b8675c9e08a6306059ce867f9e113f50c52f987a81cc2abdc4d71c",
	}
	logsRoots := []string{
		"c6ab01a590f123c121f3dad8fcc5e8a59ef73156492f384931e0158cd6d12e4e",
		"e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
		wantLogLeafHashes[1],
	}
	receiptLeaves := []string{
		"01" + strings.Repeat("aa", 32) + "00" + "88a401" + "02" + logsRoots[0],
		"00" + strings.Repeat("bb", 32) + "02" + "90a10f" + "00" + logsRoots[1],
		"01" + strings.Repeat("cc", 32) + "03" + "d08603" + "01" + logsRoots[2],
	}
	wantReceiptLeafHashes := []string{
		"42efd94a8e5dd8eaf4fb32037af9eea4081f9ceceda48d09029ee3abf35052a4",
		"7b3ebdde4b6e5be67617bcc84ece373da7078af8f208bbd1c14989541bc40c14",
		"c94de41e9c2275fbc03c40cd4af15615d1c4323f7e37be58d0494208cf013545",
	}
	inner01 := "de8aa4695eeaaa952d42534537bae36fd063834a93ca54f51c1392a8773552d1"
	root := "d22c0221b39a0dbe6b22f830ea2615057b470b570f3d550dfdffb8d04bcc1797"
	receiptHashes := []string{
		"b76c94e23577845473017e9ffd350313a3a8fe4e63fd7ac0a869ae9ff2f9129b",
		"32b95177fd02367881c7fbadad25ace4c913704bfb0461a4933801f6707f84c8",
		"dcb5777bf0270f28d32b8753e0234b9c2944f80f6ebcae7aa6d1f29699700b37",
		"3bc5f724a5daab50b2875110abf0c268a7d3e0fd6d23b91f5537a51d9cccb358",
	}

	log0, log1 := specLogs()
	for i, log := range []*ethtypes.Log{log0, log1} {
		leaf := appendLogLeaf(nil, log.Address, log.Topics, log.Data)
		require.Equal(t, logLeaves[i], hex.EncodeToString(leaf), "log %d leaf", i)
		leafHash := merkle.MerkleLeafHash(leaf)
		require.Equal(t, wantLogLeafHashes[i], hex.EncodeToString(leafHash[:]), "log %d leaf hash", i)
	}

	receipts := specReceipts()
	leafHashes := make([][32]byte, 0, len(receipts))
	for i, receipt := range receipts {
		logHashes, _ := logLeafHashes(receipt.Logs, nil)
		logsRoot := merkle.MerkleRoot(logHashes)
		require.Equal(t, logsRoots[i], hex.EncodeToString(logsRoot[:]), "receipt %d logs root", i)
		fields := newReceiptFields(receipt)
		leaf := fields.appendLeaf(nil, logsRoot)
		require.Equal(t, receiptLeaves[i], hex.EncodeToString(leaf), "receipt %d leaf", i)
		leafHash := merkle.MerkleLeafHash(leaf)
		require.Equal(t, wantReceiptLeafHashes[i], hex.EncodeToString(leafHash[:]), "receipt %d leaf hash", i)
		leafHashes = append(leafHashes, leafHash)
	}
	computedInner01 := merkle.MerkleInnerHash(leafHashes[0], leafHashes[1])
	require.Equal(t, inner01, hex.EncodeToString(computedInner01[:]))
	computedRoot := merkle.MerkleRoot(leafHashes)
	require.Equal(t, root, hex.EncodeToString(computedRoot[:]))

	for count := 0; count <= len(receipts); count++ {
		receiptHash, err := ComputeReceiptHash(receipts[:count])
		require.NoError(t, err)
		require.Equal(t, receiptHashes[count], hex.EncodeToString(receiptHash[:]), "%d receipts", count)
	}

	for i := range receipts {
		proof, err := BuildReceiptProof(receipts, i)
		require.NoError(t, err, "receipt %d", i)
		proofHash, err := proof.ComputeReceiptHash()
		require.NoError(t, err, "receipt %d proof", i)
		require.Equal(t, receiptHashes[3], hex.EncodeToString(proofHash[:]), "receipt %d proof", i)
		for j := range receipts[i].Logs {
			logProof, err := BuildLogProof(receipts, i, j)
			require.NoError(t, err, "receipt %d log %d", i, j)
			logProofHash, err := logProof.ComputeReceiptHash()
			require.NoError(t, err, "receipt %d log %d proof", i, j)
			require.Equal(t, receiptHashes[3], hex.EncodeToString(logProofHash[:]),
				"receipt %d log %d proof", i, j)
		}
	}
}

//go:build !codeanalysis
// +build !codeanalysis

package replay

import (
	"io/ioutil"
	"os"
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/sei-protocol/sei-chain/x/nitro/types"
	"github.com/stretchr/testify/require"
)

func testAccount() types.Account {
	return types.Account{
		Pubkey:     "pubkey",
		Owner:      "owner",
		Data:       "data",
		Lamports:   1,
		Slot:       2,
		RentEpoch:  3,
		Executable: false,
	}
}

func testTransaction() types.TransactionData {
	return types.TransactionData{
		Slot:        1,
		Signature:   "signature",
		IsVote:      false,
		MessageType: 0,
		LegacyMessage: &types.LegacyMessage{
			Header: &types.MessageHeader{
				NumRequiredSignatures:       1,
				NumReadonlySignedAccounts:   2,
				NumReadonlyUnsignedAccounts: 3,
			},
			AccountKeys:     []string{"a1", "a2"},
			RecentBlockhash: "recent",
			Instructions: []*types.CompiledInstruction{
				{
					ProgramIdIndex: 1,
					Accounts:       []uint32{4, 5, 6},
					Data:           "d1",
				},
				{
					ProgramIdIndex: 2,
					Accounts:       []uint32{7, 8, 9},
					Data:           "d2",
				},
			},
		},
		Signatures:   []string{"s1", "s2"},
		MessageHash:  "message",
		WriteVersion: 12,
	}
}

func TestReplay(t *testing.T) {
	ctx := sdk.Context{}.WithBlockHeight(1)
	tx := testTransaction()
	txbz, _ := tx.Marshal()
	_, err := Replay(ctx, [][]byte{txbz}, []types.Account{}, []types.Account{testAccount()}, []types.Account{})
	require.Nil(t, err)
}

func TestWriteAccountToFile(t *testing.T) {
	dir := "/tmp/"
	path, err := writeAccountToFile(dir, testAccount())
	defer func() {
		os.Remove(path)
	}()
	require.Nil(t, err)
	require.Equal(t, "/tmp/pubkey", path)
	bz, err := ioutil.ReadFile(path)
	require.Nil(t, err)
	require.Equal(t, "pubkey|owner|1|2|f|3|data", string(bz))
}

func TestWriteTransactionToFile(t *testing.T) {
	dir := "/tmp/"
	tx := testTransaction()
	txbz, _ := tx.Marshal()
	path, err := writeTransactionToFile(dir, txbz)
	defer func() {
		os.Remove(path)
	}()
	require.Nil(t, err)
	require.Equal(t, "/tmp/signature", path)
	bz, err := ioutil.ReadFile(path)
	require.Nil(t, err)
	require.Equal(t, "0|1-2-3,a1-a2,recent,1_4:5:6_d1-2_7:8:9_d2|message|f|s1,s2", string(bz))
}

func TestTrimHexPrefix(t *testing.T) {
	require.Equal(t, "", trimHexPrefix(""))
	require.Equal(t, "a", trimHexPrefix("a"))
	require.Equal(t, "\\y", trimHexPrefix("\\y"))
	require.Equal(t, "", trimHexPrefix("\\x"))
	require.Equal(t, "abc", trimHexPrefix("abc"))
	require.Equal(t, "1234", trimHexPrefix("\\x1234"))
}

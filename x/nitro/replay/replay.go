package replay

import (
	"fmt"
	"io/ioutil"
	"os"
	"strconv"
	"strings"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/sei-protocol/sei-chain/utils"
	"github.com/sei-protocol/sei-chain/x/nitro/types"
)

func Replay(ctx sdk.Context, txs [][]byte, accounts []*types.Account, sysvarAccounts []*types.Account, programs []*types.Account) ([]types.Account, error) {
	// there can be at most one replay per Sei block
	inputDirectory := fmt.Sprintf("/tmp/replay_input_%d/", ctx.BlockHeight())
	if err := os.Mkdir(inputDirectory, os.ModePerm); err != nil {
		return nil, err
	}
	defer func() {
		os.RemoveAll(inputDirectory)
	}()
	outputDirectory := fmt.Sprintf("/tmp/replay_output_%d/", ctx.BlockHeight())
	if err := os.Mkdir(outputDirectory, os.ModePerm); err != nil {
		return nil, err
	}
	defer func() {
		os.RemoveAll(outputDirectory)
	}()
	accountFilePaths := []string{}
	for _, account := range accounts {
		path, err := writeAccountToFile(inputDirectory, account)
		if err != nil {
			return nil, err
		}
		accountFilePaths = append(accountFilePaths, path)
	}
	sysvarFilePaths := []string{}
	for _, sysvar := range sysvarAccounts {
		path, err := writeAccountToFile(inputDirectory, sysvar)
		if err != nil {
			return nil, err
		}
		sysvarFilePaths = append(sysvarFilePaths, path)
	}
	programFilePaths := []string{}
	for _, program := range programs {
		path, err := writeAccountToFile(inputDirectory, program)
		if err != nil {
			return nil, err
		}
		programFilePaths = append(programFilePaths, path)
	}
	txFilePaths := []string{}
	for _, tx := range txs {
		path, err := writeTransactionToFile(inputDirectory, tx)
		if err != nil {
			return nil, err
		}
		txFilePaths = append(txFilePaths, path)
	}
	if err := callReplayer(accountFilePaths, sysvarFilePaths, programFilePaths, txFilePaths, outputDirectory); err != nil {
		return nil, err
	}
	files, err := ioutil.ReadDir(outputDirectory)
	if err != nil {
		return nil, err
	}
	res := []types.Account{}
	for _, file := range files {
		b, err := ioutil.ReadFile(fmt.Sprintf("%s%s", outputDirectory, file.Name()))
		if err != nil {
			return nil, err
		}
		parts := strings.Split(string(b), "|")
		pubkey := parts[0]
		owner := parts[1]
		lamports, err := strconv.ParseUint(parts[2], 10, 64)
		if err != nil {
			return nil, err
		}
		executable := parts[3] == "true"
		rentEpoch, err := strconv.ParseUint(parts[4], 10, 64)
		if err != nil {
			return nil, err
		}
		data := parts[5]
		res = append(res, types.Account{
			Pubkey:     pubkey,
			Owner:      owner,
			Lamports:   lamports,
			Executable: executable,
			RentEpoch:  rentEpoch,
			Data:       data,
		})
	}
	return res, nil
}

func writeTransactionToFile(directory string, tx []byte) (string, error) {
	transactionData := types.TransactionData{}
	if err := transactionData.Unmarshal(tx); err != nil {
		return "", err
	}
	serialized := ""

	switch transactionData.MessageType {
	case 0:
		serialized = "0|"
		header := fmt.Sprintf(
			"%d-%d-%d",
			transactionData.LegacyMessage.Header.NumRequiredSignatures,
			transactionData.LegacyMessage.Header.NumReadonlySignedAccounts,
			transactionData.LegacyMessage.Header.NumReadonlyUnsignedAccounts,
		)
		serialized += header
		serialized += ","
		serialized += strings.Join(
			utils.Map(transactionData.LegacyMessage.AccountKeys, trimHexPrefix), "-")
		serialized += ","
		serialized += trimHexPrefix(transactionData.LegacyMessage.RecentBlockhash)
		serialized += ","
		instructions := utils.Map(transactionData.LegacyMessage.Instructions, func(i *types.CompiledInstruction) string {
			return fmt.Sprintf(
				"%d_%s_%s",
				i.ProgramIdIndex,
				strings.Join(utils.Map(i.Accounts, func(a uint32) string { return fmt.Sprintf("%d", a) }), ":"),
				trimHexPrefix(i.Data),
			)
		})
		serialized += strings.Join(instructions, "-")
		serialized += "|"
	case 1:
		serialized = "1|"
		header := fmt.Sprintf(
			"%d-%d-%d",
			transactionData.V0LoadedMessage.Message.Header.NumRequiredSignatures,
			transactionData.V0LoadedMessage.Message.Header.NumReadonlySignedAccounts,
			transactionData.V0LoadedMessage.Message.Header.NumReadonlyUnsignedAccounts,
		)
		serialized += header
		serialized += ","
		serialized += strings.Join(
			utils.Map(transactionData.V0LoadedMessage.Message.AccountKeys, trimHexPrefix), "-")
		serialized += ","
		serialized += trimHexPrefix(transactionData.V0LoadedMessage.Message.RecentBlockhash)
		serialized += ","
		instructions := utils.Map(transactionData.V0LoadedMessage.Message.Instructions, func(i *types.CompiledInstruction) string {
			return fmt.Sprintf(
				"%d_%s_%s",
				i.ProgramIdIndex,
				strings.Join(utils.Map(i.Accounts, func(a uint32) string { return fmt.Sprintf("%d", a) }), ":"),
				trimHexPrefix(i.Data),
			)
		})
		serialized += strings.Join(instructions, "-")
		serialized += "|"
	default:
		panic("unknown message type")
	}

	serialized += transactionData.MessageHash
	serialized += "|"
	if transactionData.IsVote {
		serialized += "t"
	} else {
		serialized += "f"
	}
	serialized += "|"
	serialized += strings.Join(transactionData.Signatures, ",")
	filepath := fmt.Sprintf("%s%s", directory, transactionData.Signature)
	return filepath, os.WriteFile(filepath, []byte(serialized), 0600)
}

func writeAccountToFile(directory string, account *types.Account) (string, error) {
	serialized := ""
	serialized += account.Pubkey
	serialized += "|"
	serialized += account.Owner
	serialized += "|"
	serialized += fmt.Sprintf("%d", account.Lamports)
	serialized += "|"
	serialized += fmt.Sprintf("%d", account.Slot)
	serialized += "|"
	if account.Executable {
		serialized += "t"
	} else {
		serialized += "f"
	}
	serialized += "|"
	serialized += fmt.Sprintf("%d", account.RentEpoch)
	serialized += "|"
	serialized += account.Data
	filepath := fmt.Sprintf("%s%s", directory, account.Pubkey)
	return filepath, os.WriteFile(filepath, []byte(serialized), 0600)
}

// input: \x1234 output: 1234
func trimHexPrefix(s string) string {
	if len(s) < 2 {
		return s
	}
	if s[:2] != "\\x" {
		return s
	}
	return s[2:]
}

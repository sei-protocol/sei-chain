package report

import (
	"strconv"

	"github.com/cosmos/cosmos-sdk/crypto/types/multisig"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
)

func (s *csvService) exportAccountsCSV(ctx sdk.Context) error {
	file, writer := s.openCSVFile("accounts.csv")
	defer file.Close()
	defer writer.Flush()

	// Write CSV header
	if err := writer.Write([]string{"account", "evm_address", "evm_nonce", "sequence", "associated", "bucket"}); err != nil {
		return err
	}

	var iterateErr error
	s.ak.IterateAccounts(ctx, func(account authtypes.AccountI) (stop bool) {
		// Get basic account info
		seiAddr := account.GetAddress()
		seiAddrStr := seiAddr.String()
		sequence := account.GetSequence()

		// Determine if it's a CW contract (62 char length check)
		isCWContract := len(seiAddrStr) == 62

		// Get EVM address info
		evmAddr, associated := s.ek.GetEVMAddress(ctx, seiAddr)
		if !associated {
			evmAddr = s.ek.GetEVMAddressOrDefault(ctx, seiAddr)
		}
		evmAddrStr := evmAddr.Hex()
		evmNonce := s.ek.GetNonce(ctx, evmAddr)

		// Check if it's an EVM contract
		code := s.ek.GetCode(ctx, evmAddr)
		isEVMContract := len(code) > 0

		// Check if it's a multisig
		isMultisig := false
		if baseAcct, ok := account.(*authtypes.BaseAccount); ok {
			_, multiOk := baseAcct.GetPubKey().(multisig.PubKey)
			isMultisig = multiOk
		}

		// Create minimal account struct for bucket determination
		acct := &Account{
			Account:       seiAddrStr,
			EVMAddress:    evmAddrStr,
			EVMNonce:      evmNonce,
			Sequence:      sequence,
			IsAssociated:  associated,
			IsCWContract:  isCWContract,
			IsEVMContract: isEVMContract,
			IsMultisig:    isMultisig,
		}

		bucket := s.determineBucket(acct)

		// Write CSV row directly without storing intermediate strings
		row := []string{
			seiAddrStr,
			evmAddrStr,
			strconv.FormatUint(evmNonce, 10),
			strconv.FormatUint(sequence, 10),
			strconv.FormatBool(associated),
			bucket,
		}

		if err := writer.Write(row); err != nil {
			ctx.Logger().Error("failed to write account CSV row", "account", seiAddrStr, "error", err)
			iterateErr = err
			return true
		}

		// Clear references to help GC
		acct = nil
		code = nil

		return false
	})

	return iterateErr
}

package report

import (
	"fmt"
	"github.com/cosmos/cosmos-sdk/crypto/types/multisig"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	"os"
)

func (s *csvService) exportAccountsCSV(ctx sdk.Context) error {
	// Open file for direct writing - no buffering at all
	filename := fmt.Sprintf("%s/accounts.csv", s.outputDir)
	file, err := os.OpenFile(filename, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		return err
	}
	defer file.Close()

	// Write header directly to file
	header := "account,evm_address,evm_nonce,sequence,associated,bucket\n"
	if _, err := file.WriteString(header); err != nil {
		return err
	}

	var iterateErr error
	s.ak.IterateAccounts(ctx, func(account authtypes.AccountI) (stop bool) {
		// Get address once
		seiAddr := account.GetAddress()
		seiAddrStr := seiAddr.String()

		// Get EVM info
		evmAddr, associated := s.ek.GetEVMAddress(ctx, seiAddr)
		if !associated {
			evmAddr = s.ek.GetEVMAddressOrDefault(ctx, seiAddr)
		}

		// Get other data
		evmNonce := s.ek.GetNonce(ctx, evmAddr)
		sequence := account.GetSequence()

		// Check contract types
		isCWContract := len(seiAddrStr) == 62
		code := s.ek.GetCode(ctx, evmAddr)
		isEVMContract := len(code) > 0

		// Check multisig
		isMultisig := false
		if baseAcct, ok := account.(*authtypes.BaseAccount); ok {
			_, multiOk := baseAcct.GetPubKey().(multisig.PubKey)
			isMultisig = multiOk
		}

		// Create minimal struct only for bucket determination
		acct := &Account{
			Account:       seiAddrStr,
			EVMAddress:    evmAddr.Hex(),
			EVMNonce:      evmNonce,
			Sequence:      sequence,
			IsAssociated:  associated,
			IsCWContract:  isCWContract,
			IsEVMContract: isEVMContract,
			IsMultisig:    isMultisig,
		}
		bucket := s.determineBucket(acct)

		// Write directly to file as single string - no CSV writer, no buffering
		line := fmt.Sprintf("%s,%s,%d,%d,%t,%s\n",
			seiAddrStr,
			evmAddr.Hex(),
			evmNonce,
			sequence,
			associated,
			bucket,
		)

		if _, err := file.WriteString(line); err != nil {
			ctx.Logger().Error("failed to write account line", "account", seiAddrStr, "error", err)
			iterateErr = err
			return true
		}

		// Force immediate write to disk - no OS buffering
		if err := file.Sync(); err != nil {
			ctx.Logger().Error("failed to sync file", "error", err)
			// Don't fail on sync errors, just log
		}

		return false
	})

	return iterateErr
}

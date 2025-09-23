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
		acct := &Account{
			Account:  account.GetAddress().String(),
			Sequence: account.GetSequence(),
		}

		acct.IsCWContract = len(account.GetAddress().String()) == 62

		evmAddr, associated := s.ek.GetEVMAddress(ctx, account.GetAddress())
		acct.IsAssociated = associated
		if !associated {
			evmAddr = s.ek.GetEVMAddressOrDefault(ctx, account.GetAddress())
		}
		acct.EVMAddress = evmAddr.Hex()
		acct.EVMNonce = s.ek.GetNonce(ctx, evmAddr)
		c := s.ek.GetCode(ctx, evmAddr)
		acct.IsEVMContract = len(c) > 0

		if baseAcct, ok := account.(*authtypes.BaseAccount); ok {
			_, multiOk := baseAcct.GetPubKey().(multisig.PubKey)
			acct.IsMultisig = multiOk
		}

		bucket := s.determineBucket(acct)

		// Write CSV row
		row := []string{
			acct.Account,
			acct.EVMAddress,
			strconv.FormatUint(acct.EVMNonce, 10),
			strconv.FormatUint(acct.Sequence, 10),
			strconv.FormatBool(acct.IsAssociated),
			bucket,
		}

		if err := writer.Write(row); err != nil {
			ctx.Logger().Error("failed to write account CSV row", "account", account.GetAddress().String(), "error", err)
			iterateErr = err
			return true
		}

		return false
	})

	return iterateErr
}

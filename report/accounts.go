package report

import (
	"github.com/cosmos/cosmos-sdk/crypto/types/multisig"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
)

type Account struct {
	Account       string `json:"account"`
	EVMAddress    string `json:"evmAddress"`
	EVMNonce      uint64 `json:"evmNonce"`
	Sequence      uint64 `json:"sequence"`
	IsAssociated  bool   `json:"associated"`
	IsEVMContract bool   `json:"isEvmContract"`
	IsCWContract  bool   `json:"isCWContract"`
	IsMultisig    bool   `json:"isMultisig"`
}

func (s *service) exportAccounts(ctx sdk.Context) error {
	file := s.openFile("accounts.txt")
	defer file.Close()
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

		if _, err := file.WriteString(jsonRow(acct)); err != nil {
			ctx.Logger().Error("failed to write account", "account", account.GetAddress().String(), "error", err)
			iterateErr = err
			return true
		}

		return false
	})
	return iterateErr
}

package keeper_test

import (
	"crypto/ecdsa"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/holiman/uint256"
	"github.com/sei-protocol/sei-chain/x/evm/types"
	"github.com/stretchr/testify/require"
)

var delegateTarget = common.HexToAddress("0x000000000000000000000000000000000000c0de")

// setCodeTx builds a SetCode tx carrying one authorization signed by authority.
func (e *behaviorEnv) setCodeTx(authority *ecdsa.PrivateKey, authNonce, txNonce uint64, target common.Address) ethtypes.TxData {
	auth, err := ethtypes.SignSetCode(authority, ethtypes.SetCodeAuthorization{
		ChainID: *uint256.MustFromBig(e.k.ChainID(e.ctx)),
		Address: target,
		Nonce:   authNonce,
	})
	require.NoError(e.t, err)
	to := common.HexToAddress("0x00000000000000000000000000000000000000aa")
	return &ethtypes.SetCodeTx{
		ChainID:   uint256.MustFromBig(e.k.ChainID(e.ctx)),
		Nonce:     txNonce,
		GasTipCap: uint256.MustFromBig(bigGwei(1)),
		GasFeeCap: uint256.MustFromBig(bigGwei(1)),
		Gas:       200_000,
		To:        to,
		Value:     uint256.NewInt(0),
		AuthList:  []ethtypes.SetCodeAuthorization{auth},
	}
}

func (e *behaviorEnv) hasCodeKey(addr common.Address) bool {
	return e.k.PrefixStore(e.ctx, types.CodeKeyPrefix).Has(addr[:])
}

// TestSetCodeAuthorizationBehavior pins which EIP-7702 authorizations write code.
func TestSetCodeAuthorizationBehavior(t *testing.T) {
	sponsorKey := mustKey(t, anvilKey0Hex)
	authorityKey := mustKey(t, anvilKey1Hex)

	t.Run("clear_without_delegation_writes_no_code", func(t *testing.T) {
		e := newBehaviorEnv(t, false)
		e.associateAndFund(sponsorKey, 1_000_000)
		authSei, authority := keyAddrs(authorityKey)
		e.k.SetAddressMapping(e.ctx, authSei, authority)

		r := e.runTx(sponsorKey, e.setCodeTx(authorityKey, 0, 0, common.Address{}), 0)
		require.Empty(t, r.res.VmError)
		require.Equal(t, uint64(46_000), r.res.GasUsed)
		require.Equal(t, uint64(1), e.k.GetNonce(e.ctx, authority))
		require.False(t, e.hasCodeKey(authority))
		require.Equal(t, ethtypes.EmptyCodeHash, e.k.GetCodeHash(e.ctx, authority))
		gotSei, ok := e.k.GetSeiAddress(e.ctx, authority)
		require.True(t, ok)
		require.Equal(t, authSei, gotSei)
	})

	t.Run("clear_after_delegation_writes_empty_code", func(t *testing.T) {
		e := newBehaviorEnv(t, false)
		e.associateAndFund(sponsorKey, 1_000_000)
		authSei, authority := keyAddrs(authorityKey)
		e.k.SetAddressMapping(e.ctx, authSei, authority)

		e.runTx(sponsorKey, e.setCodeTx(authorityKey, 0, 0, delegateTarget), 0)
		require.Equal(t, ethtypes.AddressToDelegation(delegateTarget), e.k.GetCode(e.ctx, authority))

		r := e.runTx(sponsorKey, e.setCodeTx(authorityKey, 1, 1, common.Address{}), 1)
		require.Empty(t, r.res.VmError)
		require.Equal(t, uint64(36_800), r.res.GasUsed)
		require.True(t, e.hasCodeKey(authority))
		require.Nil(t, e.k.GetCode(e.ctx, authority))
		require.Equal(t, 0, e.k.GetCodeSize(e.ctx, authority))
		require.Equal(t, ethtypes.EmptyCodeHash, e.k.GetCodeHash(e.ctx, authority))
	})

	t.Run("same_target_redelegation_skips_code_write", func(t *testing.T) {
		e := newBehaviorEnv(t, false)
		e.associateAndFund(sponsorKey, 1_000_000)
		authSei, authority := keyAddrs(authorityKey)
		e.k.SetAddressMapping(e.ctx, authSei, authority)

		e.runTx(sponsorKey, e.setCodeTx(authorityKey, 0, 0, delegateTarget), 0)
		// Probe: a code write would restore the removed size key.
		e.k.PrefixStore(e.ctx, types.CodeSizeKeyPrefix).Delete(authority[:])

		r := e.runTx(sponsorKey, e.setCodeTx(authorityKey, 1, 1, delegateTarget), 1)
		require.Empty(t, r.res.VmError)
		require.Equal(t, uint64(36_800), r.res.GasUsed)
		require.Equal(t, uint64(2), e.k.GetNonce(e.ctx, authority))
		require.False(t, e.k.PrefixStore(e.ctx, types.CodeSizeKeyPrefix).Has(authority[:]))
		require.Equal(t, ethtypes.AddressToDelegation(delegateTarget), e.k.GetCode(e.ctx, authority))
	})
}

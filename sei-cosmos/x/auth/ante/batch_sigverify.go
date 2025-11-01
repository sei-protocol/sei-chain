package ante

import (
	"errors"
	"fmt"

	"github.com/cosmos/cosmos-sdk/crypto/keys/sr25519"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/cosmos/cosmos-sdk/types/tx/signing"
	authsigning "github.com/cosmos/cosmos-sdk/x/auth/signing"
	tmsr25519 "github.com/tendermint/tendermint/crypto/sr25519"
)

type SR25519BatchVerifier struct {
	verifier        *tmsr25519.BatchVerifier
	errors          []error
	ak              AccountKeeper
	signModeHandler authsigning.SignModeHandler
}

func NewSR25519BatchVerifier(ak AccountKeeper, signModeHandler authsigning.SignModeHandler) *SR25519BatchVerifier {
	verifier := tmsr25519.NewBatchVerifier().(*tmsr25519.BatchVerifier)
	return &SR25519BatchVerifier{
		verifier:        verifier,
		errors:          []error{},
		ak:              ak,
		signModeHandler: signModeHandler,
	}
}

func (v *SR25519BatchVerifier) VerifyTxs(ctx sdk.Context, txs []sdk.Tx) {
	if ctx.BlockHeight() == 0 || ctx.IsReCheckTx() {
		return
	}
	v.errors = make([]error, len(txs))
	sigTxs := make([]authsigning.SigVerifiableTx, len(txs))
	sigsList := make([][]signing.SignatureV2, len(txs))
	signerAddressesList := make([][]sdk.AccAddress, len(txs))
	for i, tx := range txs {
		if tx == nil {
			v.errors[i] = sdkerrors.Wrap(sdkerrors.ErrTxDecode, "nil transaction")
			continue
		}
		sigTx, ok := tx.(authsigning.SigVerifiableTx)
		if !ok {
			v.errors[i] = sdkerrors.Wrap(sdkerrors.ErrTxDecode, "invalid transaction type")
			continue
		}
		sigs, err := sigTx.GetSignaturesV2()
		if err != nil {
			v.errors[i] = err
			continue
		}
		signerAddrs := sigTx.GetSigners()
		if len(sigs) != len(signerAddrs) {
			v.errors[i] = sdkerrors.Wrapf(sdkerrors.ErrUnauthorized, "invalid number of signer;  expected: %d, got %d", len(signerAddrs), len(sigs))
			continue
		}

		pubkeys, err := sigTx.GetPubKeys()
		if err != nil {
			v.errors[i] = err
			continue
		}
		if len(pubkeys) != len(signerAddrs) {
			v.errors[i] = sdkerrors.Wrapf(sdkerrors.ErrUnauthorized, "invalid number of pubkeys;  expected: %d, got %d", len(signerAddrs), len(pubkeys))
			continue
		}
		for j, pk := range pubkeys {
			acc, err := GetSignerAcc(ctx, v.ak, signerAddrs[j])
			if err != nil {
				v.errors[i] = err
				break
			}
			// account already has pubkey set,no need to reset
			if acc.GetPubKey() != nil {
				continue
			}
			err = acc.SetPubKey(pk)
			if err != nil {
				v.errors[i] = sdkerrors.Wrap(sdkerrors.ErrInvalidPubKey, err.Error())
				break
			}
			v.ak.SetAccount(ctx, acc)
		}
		if v.errors[i] != nil {
			continue
		}

		sigTxs[i] = sigTx
		sigsList[i] = sigs
		signerAddressesList[i] = signerAddrs
	}
	sigTxIndices := []int{}
	sequenceNumberCache := map[uint64]uint64{}
	for i := range txs {
		if v.errors[i] != nil {
			continue
		}
		for j := range sigsList[i] {
			acc, err := GetSignerAcc(ctx, v.ak, signerAddressesList[i][j])
			if err != nil {
				v.errors[i] = err
				continue
			}

			pubKey := acc.GetPubKey()
			if pubKey == nil {
				v.errors[i] = sdkerrors.Wrap(sdkerrors.ErrInvalidPubKey, "pubkey on account is not set")
				continue
			}
			typedPubKey, ok := pubKey.(*sr25519.PubKey)
			if !ok {
				v.errors[i] = sdkerrors.Wrapf(
					sdkerrors.ErrNotSupported,
					"only sr25519 supported at the moment",
				)
				continue
			}

			accNum := acc.GetAccountNumber()

			var seqNum uint64
			if cachedSeq, ok := sequenceNumberCache[accNum]; ok {
				seqNum = cachedSeq + 1
				sequenceNumberCache[accNum] = seqNum
			} else {
				sequenceNumberCache[accNum] = acc.GetSequence()
				seqNum = sequenceNumberCache[accNum]
			}

			sig := sigsList[i][j]
			if sig.Sequence != seqNum {
				params := v.ak.GetParams(ctx)
				if !params.GetDisableSeqnoCheck() {
					v.errors[i] = sdkerrors.Wrapf(
						sdkerrors.ErrWrongSequence,
						"account sequence mismatch, expected %d, got %d", acc.GetSequence(), sig.Sequence,
					)
					continue
				}
			}

			switch data := sig.Data.(type) {
			case *signing.SingleSignatureData:
				chainID := ctx.ChainID()
				signerData := authsigning.SignerData{
					ChainID:       chainID,
					AccountNumber: accNum,
					Sequence:      acc.GetSequence(),
				}
				signBytes, err := v.signModeHandler.GetSignBytes(data.SignMode, signerData, txs[i])
				if err != nil {
					v.errors[i] = err
					continue
				}
				err = v.verifier.Add(typedPubKey.Key, signBytes, data.Signature)
				if err != nil {
					v.errors[i] = err
					continue
				}
				sigTxIndices = append(sigTxIndices, i)
			case *signing.MultiSignatureData:
				v.errors[i] = sdkerrors.Wrapf(
					sdkerrors.ErrNotSupported,
					"multisig not supported at the moment",
				)
				continue
			default:
				v.errors[i] = fmt.Errorf("unexpected SignatureData %T", sig.Data)
				continue
			}
		}
	}
	overall, individiauls := v.verifier.Verify()
	if !overall {
		for i, individual := range individiauls {
			if !individual {
				v.errors[i] = sdkerrors.Wrap(
					sdkerrors.ErrUnauthorized,
					"signature verification failed; please verify account number and chain-id",
				)
			}
		}
	}
}

type ContextKeyTxIndexKeyType string

const ContextKeyTxIndexKey ContextKeyTxIndexKeyType = ContextKeyTxIndexKeyType("tx-index")

type BatchSigVerificationDecorator struct {
	verifier           *SR25519BatchVerifier
	sigVerifyDecorator SigVerificationDecorator
}

func NewBatchSigVerificationDecorator(verifier *SR25519BatchVerifier, sigVerifyDecorator SigVerificationDecorator) BatchSigVerificationDecorator {
	return BatchSigVerificationDecorator{
		verifier:           verifier,
		sigVerifyDecorator: sigVerifyDecorator,
	}
}

func (svd BatchSigVerificationDecorator) AnteHandle(ctx sdk.Context, tx sdk.Tx, simulate bool, next sdk.AnteHandler) (newCtx sdk.Context, err error) {
	var txIdx int
	if val := ctx.Context().Value(ContextKeyTxIndexKey); val != nil {
		idx, ok := val.(int)
		if !ok {
			return ctx, errors.New("invalid tx index data type")
		}
		txIdx = idx
	} else if ctx.BlockHeight() == 0 || ctx.IsCheckTx() || ctx.IsReCheckTx() {
		ctx.Logger().Debug("fall back to sequential verification during genesis or CheckTx")
		return svd.sigVerifyDecorator.AnteHandle(ctx, tx, simulate, next)
	} else {
		return ctx, errors.New("no tx index set when using batch sig verification")
	}

	if err := svd.verifier.errors[txIdx]; err != nil {
		return ctx, err
	}

	return next(ctx, tx, simulate)
}

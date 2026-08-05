package tx

import (
	"fmt"

	"github.com/gogo/protobuf/proto"
	"google.golang.org/protobuf/encoding/protowire"

	"github.com/sei-protocol/sei-chain/sei-cosmos/codec"
	"github.com/sei-protocol/sei-chain/sei-cosmos/codec/unknownproto"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	sdkerrors "github.com/sei-protocol/sei-chain/sei-cosmos/types/errors"
	"github.com/sei-protocol/sei-chain/sei-cosmos/types/tx"
)

// DefaultTxDecoder returns a default protobuf TxDecoder using the provided Marshaler.
func DefaultTxDecoder(cdc codec.ProtoCodecMarshaler) sdk.TxDecoder {
	return defaultTxDecoder(cdc, true, true)
}

// DefaultTxDecoderWithoutBodyBloatRejection returns a protobuf TxDecoder that
// preserves pre-v6.5 decode behavior for historical tooling: it does not reject
// non-canonical TxBody or AuthInfo wire encodings. Do not use this for
// mempool, CheckTx, or DeliverTx paths.
func DefaultTxDecoderWithoutBodyBloatRejection(cdc codec.ProtoCodecMarshaler) sdk.TxDecoder {
	return defaultTxDecoder(cdc, false, false)
}

// DefaultTxDecoderWithoutAuthInfoBloatRejection returns a protobuf TxDecoder that
// rejects non-canonical TxBody encodings (v6.5+) but not non-canonical AuthInfo
// encodings. Used for historical tooling that replays the v6.5-to-v6.7 window.
// Do not use this for mempool, CheckTx, or DeliverTx paths.
func DefaultTxDecoderWithoutAuthInfoBloatRejection(cdc codec.ProtoCodecMarshaler) sdk.TxDecoder {
	return defaultTxDecoder(cdc, true, false)
}

func defaultTxDecoder(cdc codec.ProtoCodecMarshaler, rejectBodyBloat, rejectAuthInfoBloat bool) sdk.TxDecoder {
	return func(txBytes []byte) (sdk.Tx, error) {
		// Make sure txBytes follow ADR-027.
		err := rejectNonADR027TxRaw(txBytes)
		if err != nil {
			return nil, sdkerrors.Wrap(sdkerrors.ErrTxDecode, err.Error())
		}

		var raw tx.TxRaw

		// reject all unknown proto fields in the root TxRaw
		err = unknownproto.RejectUnknownFieldsStrict(txBytes, &raw, cdc.InterfaceRegistry())
		if err != nil {
			return nil, sdkerrors.Wrap(sdkerrors.ErrTxDecode, err.Error())
		}

		err = cdc.Unmarshal(txBytes, &raw)
		if err != nil {
			return nil, err
		}

		// Reject non-canonical TxRaw envelopes (e.g. explicit default encodings).
		// Duplicate singular fields are also rejected earlier by rejectNonADR027TxRaw.
		if rejectBodyBloat || rejectAuthInfoBloat {
			if err := rejectBloatedProto(txBytes, &raw, "tx raw"); err != nil {
				return nil, sdkerrors.Wrap(sdkerrors.ErrTxDecode, err.Error())
			}
		}

		var body tx.TxBody

		// allow non-critical unknown fields in TxBody
		txBodyHasUnknownNonCriticals, err := unknownproto.RejectUnknownFields(raw.BodyBytes, &body, true, cdc.InterfaceRegistry())
		if err != nil {
			return nil, sdkerrors.Wrap(sdkerrors.ErrTxDecode, err.Error())
		}

		err = cdc.Unmarshal(raw.BodyBytes, &body)
		if err != nil {
			return nil, sdkerrors.Wrap(sdkerrors.ErrTxDecode, err.Error())
		}

		if rejectBodyBloat {
			if err := rejectBloatedProto(raw.BodyBytes, &body, "tx body"); err != nil {
				return nil, sdkerrors.Wrap(sdkerrors.ErrTxDecode, err.Error())
			}
		}

		var authInfo tx.AuthInfo

		// reject all unknown proto fields in AuthInfo
		err = unknownproto.RejectUnknownFieldsStrict(raw.AuthInfoBytes, &authInfo, cdc.InterfaceRegistry())
		if err != nil {
			return nil, sdkerrors.Wrap(sdkerrors.ErrTxDecode, err.Error())
		}

		err = cdc.Unmarshal(raw.AuthInfoBytes, &authInfo)
		if err != nil {
			return nil, sdkerrors.Wrap(sdkerrors.ErrTxDecode, err.Error())
		}

		if rejectAuthInfoBloat {
			if err := rejectBloatedProto(raw.AuthInfoBytes, &authInfo, "tx auth info"); err != nil {
				return nil, sdkerrors.Wrap(sdkerrors.ErrTxDecode, err.Error())
			}
		}

		theTx := &tx.Tx{
			Body:       &body,
			AuthInfo:   &authInfo,
			Signatures: raw.Signatures,
		}

		return &wrapper{
			tx:                           theTx,
			bodyBz:                       raw.BodyBytes,
			authInfoBz:                   raw.AuthInfoBytes,
			txBodyHasUnknownNonCriticals: txBodyHasUnknownNonCriticals,
		}, nil
	}
}

// DefaultJSONTxDecoder returns a default protobuf JSON TxDecoder using the provided Marshaler.
func DefaultJSONTxDecoder(cdc codec.ProtoCodecMarshaler) sdk.TxDecoder {
	return func(txBytes []byte) (sdk.Tx, error) {
		var theTx tx.Tx
		err := cdc.UnmarshalAsJSON(txBytes, &theTx)
		if err != nil {
			return nil, sdkerrors.Wrap(sdkerrors.ErrTxDecode, err.Error())
		}

		return &wrapper{
			tx: &theTx,
		}, nil
	}
}

// rejectNonADR027TxRaw rejects txBytes that do not follow ADR-027. This is NOT
// a generic ADR-027 checker, it only applies decoding TxRaw. Specifically, it
// only checks that:
//   - field numbers are in ascending order (1, 2, and potentially multiple 3s),
//   - singular fields 1 (body_bytes) and 2 (auth_info_bytes) appear at most once
//     (field 3 / signatures is repeated and may appear multiple times),
//   - and varints are as short as possible.
//
// All other ADR-027 edge cases (e.g. default values) are not applicable with
// TxRaw.
func rejectNonADR027TxRaw(txBytes []byte) error {
	// Make sure all fields are ordered in ascending order with this variable.
	prevTagNum := protowire.Number(0)

	for len(txBytes) > 0 {
		tagNum, wireType, m := protowire.ConsumeTag(txBytes)
		if m < 0 {
			return fmt.Errorf("invalid length; %w", protowire.ParseError(m))
		}
		// TxRaw only has bytes fields.
		if wireType != protowire.BytesType {
			return fmt.Errorf("expected %d wire type, got %d", protowire.BytesType, wireType)
		}
		// Make sure fields are ordered in ascending order.
		if tagNum < prevTagNum {
			return fmt.Errorf("txRaw must follow ADR-027, got tagNum %d after tagNum %d", tagNum, prevTagNum)
		}
		// body_bytes and auth_info_bytes are singular; only signatures may repeat.
		if tagNum == prevTagNum && tagNum != 3 {
			return fmt.Errorf("txRaw must follow ADR-027, field %d appears more than once", tagNum)
		}
		prevTagNum = tagNum

		// All 3 fields of TxRaw have wireType == 2, so their next component
		// is a varint, so we can safely call ConsumeVarint here.
		// Byte structure: <varint of bytes length><bytes sequence>
		// Inner  fields are verified in `DefaultTxDecoder`
		lengthPrefix, m := protowire.ConsumeVarint(txBytes[m:])
		if m < 0 {
			return fmt.Errorf("invalid length; %w", protowire.ParseError(m))
		}
		// We make sure that this varint is as short as possible.
		n := varintMinLength(lengthPrefix)
		if n != m {
			return fmt.Errorf("length prefix varint for tagNum %d is not as short as possible, read %d, only need %d", tagNum, m, n)
		}

		// Skip over the bytes that store fieldNumber and wireType bytes.
		_, _, m = protowire.ConsumeField(txBytes)
		if m < 0 {
			return fmt.Errorf("invalid length; %w", protowire.ParseError(m))
		}
		txBytes = txBytes[m:]
	}

	return nil
}

// rejectBloatedProto rejects messages where the raw wire encoding is larger
// than the canonical re-marshal of the decoded struct. This catches protobuf-level
// bloat (e.g. padded sdk.Int fields, oversized Any.Value, non-canonical encodings)
// that Unmarshal would otherwise silently canonicalize away before validation runs.
func rejectBloatedProto(rawBytes []byte, msg proto.Message, name string) error {
	canonicalBytes, err := proto.Marshal(msg)
	if err != nil {
		return fmt.Errorf("failed to re-marshal %s: %w", name, err)
	}
	if len(rawBytes) != len(canonicalBytes) {
		return fmt.Errorf("%s wire size (%d) does not match canonical size (%d)", name, len(rawBytes), len(canonicalBytes))
	}
	return nil
}

// varintMinLength returns the minimum number of bytes necessary to encode an
// uint using varint encoding.
func varintMinLength(n uint64) int {
	switch {
	// Note: 1<<N == 2**N.
	case n < 1<<(7):
		return 1
	case n < 1<<(7*2):
		return 2
	case n < 1<<(7*3):
		return 3
	case n < 1<<(7*4):
		return 4
	case n < 1<<(7*5):
		return 5
	case n < 1<<(7*6):
		return 6
	case n < 1<<(7*7):
		return 7
	case n < 1<<(7*8):
		return 8
	case n < 1<<(7*9):
		return 9
	default:
		return 10
	}
}

// nolint
package mock

import (
	"bytes"
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
)

// An seitypes.Tx which is its own seitypes.Msg.
type kvstoreTx struct {
	key   []byte
	value []byte
	bytes []byte
}

// dummy implementation of proto.Message
func (msg kvstoreTx) Reset()         {}
func (msg kvstoreTx) String() string { return "TODO" }
func (msg kvstoreTx) ProtoMessage()  {}

var _ seitypes.Tx = kvstoreTx{}
var _ seitypes.Msg = kvstoreTx{}

func NewTx(key, value string) kvstoreTx {
	bytes := fmt.Sprintf("%s=%s", key, value)
	return kvstoreTx{
		key:   []byte(key),
		value: []byte(value),
		bytes: []byte(bytes),
	}
}

func (tx kvstoreTx) Route() string {
	return "kvstore"
}

func (tx kvstoreTx) Type() string {
	return "kvstore_tx"
}

func (tx kvstoreTx) GetMsgs() []seitypes.Msg {
	return []seitypes.Msg{tx}
}

func (tx kvstoreTx) GetMemo() string {
	return ""
}

func (tx kvstoreTx) GetSignBytes() []byte {
	return tx.bytes
}

// Should the app be calling this? Or only handlers?
func (tx kvstoreTx) ValidateBasic() error {
	return nil
}

func (tx kvstoreTx) GetSigners() []seitypes.AccAddress {
	return nil
}

func (tx kvstoreTx) GetGasEstimate() uint64 {
	return 0
}

// takes raw transaction bytes and decodes them into an seitypes.Tx. An seitypes.Tx has
// all the signatures and can be used to authenticate.
func decodeTx(txBytes []byte) (seitypes.Tx, error) {
	var tx seitypes.Tx

	split := bytes.Split(txBytes, []byte("="))
	if len(split) == 1 {
		k := split[0]
		tx = kvstoreTx{k, k, txBytes}
	} else if len(split) == 2 {
		k, v := split[0], split[1]
		tx = kvstoreTx{k, v, txBytes}
	} else {
		return nil, sdkerrors.Wrap(sdkerrors.ErrTxDecode, "too many '='")
	}

	return tx, nil
}

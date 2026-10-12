package flatkv

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-db/common/keys"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/view"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	gigatypes "github.com/sei-protocol/sei-chain/sei-db/state_db/giga/types"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/ktype"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/sview"
)

type recordingView struct {
	keys []string
}

func (v *recordingView) Name() string {
	return "recording"
}

func (v *recordingView) Get(key []byte, _ bool) ([]byte, bool, error) {
	v.keys = append(v.keys, string(key))
	return nil, false, nil
}

func (v *recordingView) BatchGet([][]byte) (map[string][]byte, error) {
	panic("unexpected call")
}

func (v *recordingView) ForEachDiff(func(key string, value []byte) error) error {
	panic("unexpected call")
}

func (v *recordingView) Reserve() error {
	panic("unexpected call")
}

func (v *recordingView) Release() error {
	panic("unexpected call")
}

func (v *recordingView) Abandon() {
	panic("unexpected call")
}

func (v *recordingView) Finalize([]*proto.KVPair) error {
	panic("unexpected call")
}

func (v *recordingView) AwaitFlush(context.Context) error {
	panic("unexpected call")
}

var _ view.View = (*recordingView)(nil)

func TestStateViewReadsBuildExactPhysicalKeys(t *testing.T) {
	acct := &recordingView{}
	code := &recordingView{}
	storage := &recordingView{}
	misc := &recordingView{}
	sv, err := sview.NewStoreView(1, acct, code, storage, misc)
	require.NoError(t, err)
	v := &flatKVStateView{blockView: sv}

	addr := gigatypes.Address{
		0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17, 0x18, 0x19, 0x1a,
		0x1b, 0x1c, 0x1d, 0x1e, 0x1f, 0x20, 0x21, 0x22, 0x23, 0x24,
	}
	slot := gigatypes.Hash{
		0x22, 0x23, 0x24, 0x25, 0x26, 0x27, 0x28, 0x29, 0x2a, 0x2b, 0x2c, 0x2d, 0x2e, 0x2f, 0x30, 0x31,
		0x32, 0x33, 0x34, 0x35, 0x36, 0x37, 0x38, 0x39, 0x3a, 0x3b, 0x3c, 0x3d, 0x3e, 0x3f, 0x40, 0x41,
	}
	storageKey := append(append([]byte(nil), addr[:]...), slot[:]...)
	expectedStorage := string(ktype.EVMPhysicalKey(keys.EVMKeyStorage, storageKey))

	v.GetStorage(addr, slot)
	require.Equal(t, []string{expectedStorage}, storage.keys)

	v.GetBalance(addr)
	require.Equal(t, string(ktype.EVMPhysicalKey(ktype.EVMKeyAccount, addr[:])), acct.keys[0])

	v.GetCode(addr)
	require.Equal(t, string(ktype.EVMPhysicalKey(keys.EVMKeyCode, addr[:])), code.keys[0])

	v.Get(keys.EVMStoreKey, keys.BuildEVMKey(keys.EVMKeyStorage, storageKey))
	require.Equal(t, expectedStorage, storage.keys[1])

	slot2 := gigatypes.Hash{
		0x42, 0x43, 0x44, 0x45, 0x46, 0x47, 0x48, 0x49, 0x4a, 0x4b, 0x4c, 0x4d, 0x4e, 0x4f, 0x50, 0x51,
		0x52, 0x53, 0x54, 0x55, 0x56, 0x57, 0x58, 0x59, 0x5a, 0x5b, 0x5c, 0x5d, 0x5e, 0x5f, 0x60, 0x61,
	}
	storageKey2 := append(append([]byte(nil), addr[:]...), slot2[:]...)
	expectedStorage2 := string(ktype.EVMPhysicalKey(keys.EVMKeyStorage, storageKey2))
	v.GetStorage(addr, slot2)
	require.Equal(t, []string{expectedStorage, expectedStorage, expectedStorage2}, storage.keys)
}

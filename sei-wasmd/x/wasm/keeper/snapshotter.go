package keeper

import (
	"encoding/hex"
	"errors"
	"fmt"
	"io"

	snapshot "github.com/cosmos/cosmos-sdk/snapshots/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	protoio "github.com/gogo/protobuf/io"
	"github.com/tendermint/tendermint/libs/log"
	tmproto "github.com/tendermint/tendermint/proto/tendermint/types"

	"github.com/CosmWasm/wasmd/x/wasm/ioutils"
	"github.com/CosmWasm/wasmd/x/wasm/types"
)

var _ snapshot.ExtensionSnapshotter = &WasmSnapshotter{}

// SnapshotFormat format 1 is just gzipped wasm byte code for each item payload. No protobuf envelope, no metadata.
const SnapshotFormat = 1

type WasmSnapshotter struct {
	wasm *Keeper
	cms  sdk.MultiStore
}

func NewWasmSnapshotter(cms sdk.MultiStore, wasm *Keeper) *WasmSnapshotter {
	return &WasmSnapshotter{
		wasm: wasm,
		cms:  cms,
	}
}

func (ws *WasmSnapshotter) SnapshotName() string {
	return types.ModuleName
}

func (ws *WasmSnapshotter) SnapshotFormat() uint32 {
	return SnapshotFormat
}

func (ws *WasmSnapshotter) SupportedFormats() []uint32 {
	// If we support older formats, add them here and handle them in Restore
	return []uint32{SnapshotFormat}
}

func (ws *WasmSnapshotter) Snapshot(height uint64, protoWriter protoio.Writer) error {
	cacheMS, err := ws.cms.CacheMultiStoreForExport(int64(height))
	if err != nil {
		return err
	}
	defer cacheMS.Close()

	ctx := sdk.NewContext(cacheMS, tmproto.Header{}, false, log.NewNopLogger())
	seenBefore := make(map[string]bool)
	var rerr error

	ws.wasm.IterateCodeInfos(ctx, func(id uint64, info types.CodeInfo) bool {
		// Many code ids may point to the same code hash... only sync it once
		hexHash := hex.EncodeToString(info.CodeHash)
		// if seenBefore, just skip this one and move to the next
		if seenBefore[hexHash] {
			return false
		}
		seenBefore[hexHash] = true

		// load code and abort on error
		wasmBytes, err := ws.wasm.GetByteCode(ctx, id)
		if err != nil {
			rerr = err
			return true
		}

		compressedWasm, err := ioutils.GzipIt(wasmBytes)
		if err != nil {
			rerr = err
			return true
		}

		err = snapshot.WriteExtensionItem(protoWriter, compressedWasm)
		if err != nil {
			rerr = err
			return true
		}

		return false
	})
	return rerr
}

func (ws *WasmSnapshotter) Restore(
	height uint64, format uint32, protoReader protoio.Reader,
) (snapshot.SnapshotItem, error) {
	if format == SnapshotFormat {
		return ws.processAllItems(height, protoReader, restoreV1, finalizeV1)
	}
	return snapshot.SnapshotItem{}, snapshot.ErrUnknownFormat
}

func restoreV1(ctx sdk.Context, k *Keeper, compressedCode []byte, num int) error {
	wasmCode, err := ioutils.Uncompress(compressedCode, uint64(types.MaxWasmSize))
	if err != nil {
		return sdkerrors.Wrap(types.ErrCreateFailed, err.Error())
	}

	// FIXME: check which codeIDs the checksum matches??
	checkSum, err := k.wasmVM.Create(wasmCode)
	if err != nil {
		return sdkerrors.Wrap(types.ErrCreateFailed, err.Error())
	}
	ctx.Logger().Info(fmt.Sprintf("Restored %d WASM code with checksum %X", num, checkSum))
	return nil
}

func finalizeV1(ctx sdk.Context, k *Keeper) error {
	var errCheckingExistence error
	k.IterateCodeInfos(ctx, func(id uint64, info types.CodeInfo) bool {
		_, err := k.GetByteCode(ctx, id)
		if err != nil {
			e := fmt.Sprintf("Could not find byte code for ID %d hash %X: %s", id, info.CodeHash, err)
			ctx.Logger().Error(e)
			errCheckingExistence = errors.New(e)
		}

		return false
	})
	if errCheckingExistence != nil {
		return errCheckingExistence
	}
	return k.InitializePinnedCodes(ctx)
}

func (ws *WasmSnapshotter) processAllItems(
	height uint64,
	protoReader protoio.Reader,
	cb func(sdk.Context, *Keeper, []byte, int) error,
	finalize func(sdk.Context, *Keeper) error,
) (snapshot.SnapshotItem, error) {
	ctx := sdk.NewContext(ws.cms, tmproto.Header{Height: int64(height)}, false, log.NewNopLogger())

	// keep the last item here... if we break, it will either be empty (if we hit io.EOF)
	// or contain the last item (if we hit payload == nil)
	var item snapshot.SnapshotItem
	itemNum := 0
	for {
		itemNum++
		item = snapshot.SnapshotItem{}
		err := protoReader.ReadMsg(&item)
		if err == io.EOF {
			break
		} else if err != nil {
			return snapshot.SnapshotItem{}, sdkerrors.Wrap(err, "invalid protobuf message")
		}

		// if it is not another ExtensionPayload message, then it is not for us.
		// we should return it an let the manager handle this one
		payload := item.GetExtensionPayload()
		if payload == nil {
			break
		}

		if err := cb(ctx, ws.wasm, payload.Payload, itemNum); err != nil {
			return snapshot.SnapshotItem{}, sdkerrors.Wrap(err, "processing snapshot item")
		}
	}

	return item, finalize(ctx, ws.wasm)
}

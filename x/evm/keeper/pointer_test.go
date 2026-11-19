package keeper_test

import (
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/sei-protocol/sei-chain/sei-cosmos/types"
	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/app"
	testkeeper "github.com/sei-protocol/sei-chain/testutil/keeper"
	"github.com/sei-protocol/sei-chain/x/evm/artifacts/cw1155"
	"github.com/sei-protocol/sei-chain/x/evm/artifacts/cw20"
	"github.com/sei-protocol/sei-chain/x/evm/artifacts/cw721"
	"github.com/sei-protocol/sei-chain/x/evm/artifacts/native"
	evmkeeper "github.com/sei-protocol/sei-chain/x/evm/keeper"
)

// allows us to permutate different pointer combinations
type handlers struct {
	evmSetter func(ctx types.Context, token string, addr common.Address) error
	evmGetter func(ctx types.Context, token string) (addr common.Address, version uint16, exists bool)
	cwSetter  func(ctx types.Context, erc20Address common.Address, addr string) error
	cwGetter  func(ctx types.Context, erc20Address common.Address) (addr types.AccAddress, version uint16, exists bool)
}

type seiPointerTest struct {
	name        string
	getHandlers func(k *evmkeeper.Keeper) *handlers
}

func TestEVMtoCWPointers(t *testing.T) {
	tests := []seiPointerTest{
		{
			name: "ERC20NativePointer prevents pointer to cw20 pointer",
			getHandlers: func(k *evmkeeper.Keeper) *handlers {
				return &handlers{
					evmSetter: k.SetERC20NativePointer,
					evmGetter: k.GetERC20NativePointer,
					cwSetter:  k.SetCW20ERC20Pointer,
					cwGetter:  k.GetCW20ERC20Pointer,
				}
			},
		},
		{
			name: "ERC20NativePointer prevents pointer to cw721 pointer",
			getHandlers: func(k *evmkeeper.Keeper) *handlers {
				return &handlers{
					evmSetter: k.SetERC20NativePointer,
					evmGetter: k.GetERC20NativePointer,
					cwSetter:  k.SetCW721ERC721Pointer,
					cwGetter:  k.GetCW721ERC721Pointer,
				}
			},
		},
		{
			name: "ERC20NativePointer prevents pointer to cw1155 pointer",
			getHandlers: func(k *evmkeeper.Keeper) *handlers {
				return &handlers{
					evmSetter: k.SetERC20NativePointer,
					evmGetter: k.GetERC20NativePointer,
					cwSetter:  k.SetCW1155ERC1155Pointer,
					cwGetter:  k.GetCW1155ERC1155Pointer,
				}
			},
		},
		{
			name: "ERC20CW20Pointer prevents pointer to cw721 pointer",
			getHandlers: func(k *evmkeeper.Keeper) *handlers {
				return &handlers{
					evmSetter: k.SetERC20CW20Pointer,
					evmGetter: k.GetERC20CW20Pointer,
					cwSetter:  k.SetCW721ERC721Pointer,
					cwGetter:  k.GetCW721ERC721Pointer,
				}
			},
		},
		{
			name: "ERC20CW20Pointer prevents pointer to cw1155 pointer",
			getHandlers: func(k *evmkeeper.Keeper) *handlers {
				return &handlers{
					evmSetter: k.SetERC20CW20Pointer,
					evmGetter: k.GetERC20CW20Pointer,
					cwSetter:  k.SetCW1155ERC1155Pointer,
					cwGetter:  k.GetCW1155ERC1155Pointer,
				}
			},
		},
		{
			name: "ERC20CW20Pointer prevents pointer to cw20 pointer",
			getHandlers: func(k *evmkeeper.Keeper) *handlers {
				return &handlers{
					evmSetter: k.SetERC20CW20Pointer,
					evmGetter: k.GetERC20CW20Pointer,
					cwSetter:  k.SetCW20ERC20Pointer,
					cwGetter:  k.GetCW20ERC20Pointer,
				}
			},
		},
		{
			name: "ERC721CW721Pointer prevents pointer to cw721 pointer",
			getHandlers: func(k *evmkeeper.Keeper) *handlers {
				return &handlers{
					evmSetter: k.SetERC721CW721Pointer,
					evmGetter: k.GetERC721CW721Pointer,
					cwSetter:  k.SetCW721ERC721Pointer,
					cwGetter:  k.GetCW721ERC721Pointer,
				}
			},
		},
		{
			name: "ERC721CW721Pointer prevents pointer to cw1155 pointer",
			getHandlers: func(k *evmkeeper.Keeper) *handlers {
				return &handlers{
					evmSetter: k.SetERC721CW721Pointer,
					evmGetter: k.GetERC721CW721Pointer,
					cwSetter:  k.SetCW1155ERC1155Pointer,
					cwGetter:  k.GetCW1155ERC1155Pointer,
				}
			},
		},
		{
			name: "ERC721CW721Pointer prevents pointer to cw20 pointer",
			getHandlers: func(k *evmkeeper.Keeper) *handlers {
				return &handlers{
					evmSetter: k.SetERC721CW721Pointer,
					evmGetter: k.GetERC721CW721Pointer,
					cwSetter:  k.SetCW20ERC20Pointer,
					cwGetter:  k.GetCW20ERC20Pointer,
				}
			},
		},
		{
			name: "ERC1155CW1155Pointer prevents pointer to cw721 pointer",
			getHandlers: func(k *evmkeeper.Keeper) *handlers {
				return &handlers{
					evmSetter: k.SetERC1155CW1155Pointer,
					evmGetter: k.GetERC1155CW1155Pointer,
					cwSetter:  k.SetCW721ERC721Pointer,
					cwGetter:  k.GetCW721ERC721Pointer,
				}
			},
		},
		{
			name: "ERC1155CW1155Pointer prevents pointer to cw1155 pointer",
			getHandlers: func(k *evmkeeper.Keeper) *handlers {
				return &handlers{
					evmSetter: k.SetERC1155CW1155Pointer,
					evmGetter: k.GetERC1155CW1155Pointer,
					cwSetter:  k.SetCW1155ERC1155Pointer,
					cwGetter:  k.GetCW1155ERC1155Pointer,
				}
			},
		},
		{
			name: "ERC1155CW1155Pointer prevents pointer to cw20 pointer",
			getHandlers: func(k *evmkeeper.Keeper) *handlers {
				return &handlers{
					evmSetter: k.SetERC1155CW1155Pointer,
					evmGetter: k.GetERC1155CW1155Pointer,
					cwSetter:  k.SetCW20ERC20Pointer,
					cwGetter:  k.GetCW20ERC20Pointer,
				}
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			k := &testkeeper.EVMTestApp.EvmKeeper
			ctx := testkeeper.EVMTestApp.GetContextForDeliverTx([]byte{}).WithBlockTime(time.Now())
			handlers := test.getHandlers(k)
			cwAddress, evmAddress := testkeeper.MockAddressPair()

			// create a pointer in a discarded branch, leaving cwAddress and evmAddress unregistered
			setCtx, _ := ctx.CacheContext()
			require.Nil(t, handlers.evmSetter(setCtx, cwAddress.String(), evmAddress))

			// should exist
			addr, _, exists := handlers.evmGetter(setCtx, cwAddress.String())
			require.Equal(t, evmAddress, addr)
			require.True(t, exists)

			// setup target as pointer
			require.Nil(t, handlers.cwSetter(ctx, evmAddress, cwAddress.String()))
			_, _, exists = handlers.cwGetter(ctx, evmAddress)
			require.True(t, exists)

			// should not allow pointer to pointer
			require.Error(t, handlers.evmSetter(ctx, cwAddress.String(), evmAddress), evmkeeper.ErrorPointerToPointerNotAllowed)

		})
	}
}

func TestCWtoEVMPointers(t *testing.T) {
	tests := []seiPointerTest{
		{
			name: "CW20ERC20Pointer prevents pointer to native pointer",
			getHandlers: func(k *evmkeeper.Keeper) *handlers {
				return &handlers{
					cwSetter:  k.SetCW20ERC20Pointer,
					cwGetter:  k.GetCW20ERC20Pointer,
					evmSetter: k.SetERC20NativePointer,
					evmGetter: k.GetERC20NativePointer,
				}
			},
		},
		{
			name: "CW20ERC20Pointer prevents pointer to erc20 pointer",
			getHandlers: func(k *evmkeeper.Keeper) *handlers {
				return &handlers{
					cwSetter:  k.SetCW20ERC20Pointer,
					cwGetter:  k.GetCW20ERC20Pointer,
					evmSetter: k.SetERC20CW20Pointer,
					evmGetter: k.GetERC20CW20Pointer,
				}
			},
		},
		{
			name: "CW20ERC20Pointer prevents pointer to erc721 pointer",
			getHandlers: func(k *evmkeeper.Keeper) *handlers {
				return &handlers{
					cwSetter:  k.SetCW20ERC20Pointer,
					cwGetter:  k.GetCW20ERC20Pointer,
					evmSetter: k.SetERC721CW721Pointer,
					evmGetter: k.GetERC721CW721Pointer,
				}
			},
		},
		{
			name: "CW20ERC20Pointer prevents pointer to erc1155 pointer",
			getHandlers: func(k *evmkeeper.Keeper) *handlers {
				return &handlers{
					cwSetter:  k.SetCW20ERC20Pointer,
					cwGetter:  k.GetCW20ERC20Pointer,
					evmSetter: k.SetERC1155CW1155Pointer,
					evmGetter: k.GetERC1155CW1155Pointer,
				}
			},
		},
		{
			name: "CW721ERC721Pointer prevents pointer to native pointer",
			getHandlers: func(k *evmkeeper.Keeper) *handlers {
				return &handlers{
					cwSetter:  k.SetCW721ERC721Pointer,
					cwGetter:  k.GetCW721ERC721Pointer,
					evmSetter: k.SetERC20NativePointer,
					evmGetter: k.GetERC20NativePointer,
				}
			},
		},
		{
			name: "CW721ERC721Pointer prevents pointer to erc721 pointer",
			getHandlers: func(k *evmkeeper.Keeper) *handlers {
				return &handlers{
					cwSetter:  k.SetCW721ERC721Pointer,
					cwGetter:  k.GetCW721ERC721Pointer,
					evmSetter: k.SetERC721CW721Pointer,
					evmGetter: k.GetERC721CW721Pointer,
				}
			},
		},
		{
			name: "CW721ERC721Pointer prevents pointer to erc20 pointer",
			getHandlers: func(k *evmkeeper.Keeper) *handlers {
				return &handlers{
					cwSetter:  k.SetCW721ERC721Pointer,
					cwGetter:  k.GetCW721ERC721Pointer,
					evmSetter: k.SetERC20CW20Pointer,
					evmGetter: k.GetERC20CW20Pointer,
				}
			},
		},
		{
			name: "CW721ERC721Pointer prevents pointer to erc1155 pointer",
			getHandlers: func(k *evmkeeper.Keeper) *handlers {
				return &handlers{
					cwSetter:  k.SetCW721ERC721Pointer,
					cwGetter:  k.GetCW721ERC721Pointer,
					evmSetter: k.SetERC1155CW1155Pointer,
					evmGetter: k.GetERC1155CW1155Pointer,
				}
			},
		},
		{
			name: "CW1155ERC1155Pointer prevents pointer to native pointer",
			getHandlers: func(k *evmkeeper.Keeper) *handlers {
				return &handlers{
					cwSetter:  k.SetCW1155ERC1155Pointer,
					cwGetter:  k.GetCW1155ERC1155Pointer,
					evmSetter: k.SetERC20NativePointer,
					evmGetter: k.GetERC20NativePointer,
				}
			},
		},
		{
			name: "CW1155ERC1155Pointer prevents pointer to erc721 pointer",
			getHandlers: func(k *evmkeeper.Keeper) *handlers {
				return &handlers{
					cwSetter:  k.SetCW1155ERC1155Pointer,
					cwGetter:  k.GetCW1155ERC1155Pointer,
					evmSetter: k.SetERC721CW721Pointer,
					evmGetter: k.GetERC721CW721Pointer,
				}
			},
		},
		{
			name: "CW1155ERC1155Pointer prevents pointer to erc20 pointer",
			getHandlers: func(k *evmkeeper.Keeper) *handlers {
				return &handlers{
					cwSetter:  k.SetCW1155ERC1155Pointer,
					cwGetter:  k.GetCW1155ERC1155Pointer,
					evmSetter: k.SetERC20CW20Pointer,
					evmGetter: k.GetERC20CW20Pointer,
				}
			},
		},
		{
			name: "CW1155ERC1155Pointer prevents pointer to erc1155 pointer",
			getHandlers: func(k *evmkeeper.Keeper) *handlers {
				return &handlers{
					cwSetter:  k.SetCW1155ERC1155Pointer,
					cwGetter:  k.GetCW1155ERC1155Pointer,
					evmSetter: k.SetERC1155CW1155Pointer,
					evmGetter: k.GetERC1155CW1155Pointer,
				}
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			k := &testkeeper.EVMTestApp.EvmKeeper
			ctx := testkeeper.EVMTestApp.GetContextForDeliverTx([]byte{}).WithBlockTime(time.Now())
			handlers := test.getHandlers(k)
			cwAddress, evmAddress := testkeeper.MockAddressPair()

			// create a pointer
			require.Nil(t, handlers.cwSetter(ctx, evmAddress, cwAddress.String()))

			// should exist
			addr, _, exists := handlers.cwGetter(ctx, evmAddress)
			require.Equal(t, cwAddress, addr)
			require.True(t, exists)

			// create new address to test prevention logic
			cwAddress2, evmAddress2 := testkeeper.MockAddressPair()

			// setup target as pointer (has to be evm so that the target exists)
			require.Nil(t, handlers.evmSetter(ctx, cwAddress2.String(), evmAddress2))
			_, _, exists = handlers.evmGetter(ctx, cwAddress2.String())
			require.True(t, exists)

			// should not allow pointer to pointer
			require.Error(t, handlers.cwSetter(ctx, evmAddress2, cwAddress2.String()), evmkeeper.ErrorPointerToPointerNotAllowed)
		})
	}
}

func TestGetAnyPointeeInfo(t *testing.T) {
	k := &testkeeper.EVMTestApp.EvmKeeper
	ctx := testkeeper.EVMTestApp.GetContextForDeliverTx([]byte{}).WithBlockTime(time.Now())
	cwAddress, evmAddress := testkeeper.MockAddressPair()

	// Test case 1: No pointers exist
	addr, version, exists := k.GetAnyPointeeInfo(ctx, cwAddress.String())
	require.Equal(t, common.Address{}, addr)
	require.Equal(t, uint16(0), version)
	require.False(t, exists)

	// EVMTestApp is shared across tests, so each case writes to a discarded branch of its state.

	// Test case 2: ERC20CW20 pointer exists
	caseCtx, _ := ctx.CacheContext()
	require.Nil(t, k.SetERC20CW20Pointer(caseCtx, cwAddress.String(), evmAddress))
	addr, version, exists = k.GetAnyPointeeInfo(caseCtx, cwAddress.String())
	require.Equal(t, evmAddress, addr)
	require.Equal(t, cw20.CurrentVersion, version)
	require.True(t, exists)

	// Test case 3: ERC721CW721 pointer exists
	caseCtx, _ = ctx.CacheContext()
	require.Nil(t, k.SetERC721CW721Pointer(caseCtx, cwAddress.String(), evmAddress))
	addr, version, exists = k.GetAnyPointeeInfo(caseCtx, cwAddress.String())
	require.Equal(t, evmAddress, addr)
	require.Equal(t, cw721.CurrentVersion, version)
	require.True(t, exists)

	// Test case 4: ERC1155CW1155 pointer exists
	caseCtx, _ = ctx.CacheContext()
	require.Nil(t, k.SetERC1155CW1155Pointer(caseCtx, cwAddress.String(), evmAddress))
	addr, version, exists = k.GetAnyPointeeInfo(caseCtx, cwAddress.String())
	require.Equal(t, evmAddress, addr)
	require.Equal(t, cw1155.CurrentVersion, version)
	require.True(t, exists)

	// Test case 5: Multiple pointers exist - should return first match (ERC20CW20)
	caseCtx, _ = ctx.CacheContext()
	require.Nil(t, k.SetERC20CW20Pointer(caseCtx, cwAddress.String(), evmAddress))
	require.Nil(t, k.SetERC721CW721Pointer(caseCtx, cwAddress.String(), evmAddress))
	require.Nil(t, k.SetERC1155CW1155Pointer(caseCtx, cwAddress.String(), evmAddress))
	addr, version, exists = k.GetAnyPointeeInfo(caseCtx, cwAddress.String())
	require.Equal(t, evmAddress, addr)
	require.Equal(t, cw20.CurrentVersion, version)
	require.True(t, exists)
}

func TestPointerLookupsIgnoreUpgradeName(t *testing.T) {
	blockCtx, _ := testkeeper.EVMTestApp.GetContextForDeliverTx([]byte{}).CacheContext()
	k := &testkeeper.EVMTestApp.EvmKeeper
	pamp := common.HexToAddress("0x00000000000000000000000000000000000000c1")
	pampVest := common.HexToAddress("0x00000000000000000000000000000000000000c2")
	ufoo := common.HexToAddress("0x00000000000000000000000000000000000000c3")
	require.NoError(t, k.SetERC20NativePointer(blockCtx, "factory/creator/pamp", pamp))
	require.NoError(t, k.SetERC20NativePointer(blockCtx, "factory/creator/pamp/vest", pampVest))
	require.NoError(t, k.SetERC20NativePointerWithVersion(blockCtx, "ufoo", ufoo, 1000))
	rpcCtx := blockCtx.WithClosestUpgradeName(app.LatestUpgrade)

	pointer := func(denom string) func(types.Context) []any {
		return func(ctx types.Context) []any {
			addr, version, exists := k.GetERC20NativePointer(ctx, denom)
			return []any{addr, version, exists}
		}
	}
	pointee := func(addr common.Address) func(types.Context) []any {
		return func(ctx types.Context) []any {
			denom, version, exists := k.GetNativePointee(ctx, addr.Hex())
			return []any{denom, version, exists}
		}
	}
	for _, tc := range []struct {
		name   string
		lookup func(types.Context) []any
	}{
		{"pointer of a denom", pointer("factory/creator/pamp/vest")},
		{"pointer of a denom without one", pointer("ubar")},
		{"pointer of a denom another denom extends", pointer("factory/creator/pamp")},
		{"pointer stored above the current version", pointer("ufoo")},
		{"pointee of a pointer", pointee(pampVest)},
		{"pointee of an address that is not a pointer", pointee(common.HexToAddress("0x00000000000000000000000000000000000000c4"))},
	} {
		t.Run(tc.name, func(t *testing.T) {
			blockResult, blockGas := lookupWithGas(blockCtx, tc.lookup)
			rpcResult, rpcGas := lookupWithGas(rpcCtx, tc.lookup)
			require.Equal(t, blockResult, rpcResult)
			require.Equal(t, blockGas, rpcGas)
		})
	}
}

func lookupWithGas(ctx types.Context, lookup func(types.Context) []any) ([]any, uint64) {
	ctx = ctx.WithGasMeter(types.NewInfiniteGasMeter(1, 1))
	result := lookup(ctx)
	return result, ctx.GasMeter().GasConsumed()
}

func TestGetPointerInfoMatchesExactPointee(t *testing.T) {
	ctx, _ := testkeeper.EVMTestApp.GetContextForDeliverTx([]byte{}).CacheContext()
	k := &testkeeper.EVMTestApp.EvmKeeper
	short := common.HexToAddress("0x00000000000000000000000000000000000000a1")
	long := common.HexToAddress("0x00000000000000000000000000000000000000a2")
	require.NoError(t, k.SetERC20NativePointer(ctx, "factory/creator/pamp", short))
	require.NoError(t, k.SetERC20NativePointer(ctx, "factory/creator/pamp/vest", long))

	addr, version, exists := k.GetERC20NativePointer(ctx, "factory/creator/pamp")
	require.True(t, exists)
	require.Equal(t, short, addr)
	require.Equal(t, native.CurrentVersion, version)

	addr, _, exists = k.GetERC20NativePointer(ctx, "factory/creator/pamp/vest")
	require.True(t, exists)
	require.Equal(t, long, addr)

	_, _, exists = k.GetERC20NativePointer(ctx, "factory/creator/pam")
	require.False(t, exists)
}

func TestGetPointerInfoReturnsHighestStoredVersion(t *testing.T) {
	ctx, _ := testkeeper.EVMTestApp.GetContextForDeliverTx([]byte{}).CacheContext()
	k := &testkeeper.EVMTestApp.EvmKeeper
	current := common.HexToAddress("0x00000000000000000000000000000000000000b1")
	higher := common.HexToAddress("0x00000000000000000000000000000000000000b2")
	require.NoError(t, k.SetERC20NativePointerWithVersion(ctx, "ufoo", current, native.CurrentVersion))
	require.NoError(t, k.SetERC20NativePointerWithVersion(ctx, "ufoo", higher, 1000))

	addr, version, exists := k.GetERC20NativePointer(ctx, "ufoo")
	require.True(t, exists)
	require.Equal(t, higher, addr)
	require.Equal(t, uint16(1000), version)
}

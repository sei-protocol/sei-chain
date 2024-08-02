package keeper_test

import (
	"testing"

	"github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"

	testkeeper "github.com/sei-protocol/sei-chain/testutil/keeper"
	"github.com/sei-protocol/sei-chain/x/evm/artifacts/cw1155"
	"github.com/sei-protocol/sei-chain/x/evm/artifacts/cw20"
	"github.com/sei-protocol/sei-chain/x/evm/artifacts/cw721"
	"github.com/sei-protocol/sei-chain/x/evm/artifacts/native"
	evmkeeper "github.com/sei-protocol/sei-chain/x/evm/keeper"
)

// allows us to permutate different pointer combinations
type handlers struct {
	evmSetter  func(ctx types.Context, token string, addr common.Address) error
	evmGetter  func(ctx types.Context, token string) (addr common.Address, version uint16, exists bool)
	evmDeleter func(ctx types.Context, token string, version uint16)
	cwSetter   func(ctx types.Context, erc20Address common.Address, addr string) error
	cwGetter   func(ctx types.Context, erc20Address common.Address) (addr types.AccAddress, version uint16, exists bool)
	cwDeleter  func(ctx types.Context, erc20Address common.Address, version uint16)
}

type seiPointerTest struct {
	name        string
	getHandlers func(k *evmkeeper.Keeper) *handlers
	version     uint16
}

func TestEVMtoCWPointers(t *testing.T) {
	_, ctx := testkeeper.MockEVMKeeper()

	tests := []seiPointerTest{
		{
			name: "ERC20NativePointer prevents pointer to cw20 pointer",
			getHandlers: func(k *evmkeeper.Keeper) *handlers {
				return &handlers{
					evmSetter:  k.SetERC20NativePointer,
					evmGetter:  k.GetERC20NativePointer,
					evmDeleter: k.DeleteERC20NativePointer,
					cwSetter:   k.SetCW20ERC20Pointer,
					cwGetter:   k.GetCW20ERC20Pointer,
				}
			},
			version: native.CurrentVersion,
		},
		{
			name: "ERC20NativePointer prevents pointer to cw721 pointer",
			getHandlers: func(k *evmkeeper.Keeper) *handlers {
				return &handlers{
					evmSetter:  k.SetERC20NativePointer,
					evmGetter:  k.GetERC20NativePointer,
					evmDeleter: k.DeleteERC20NativePointer,
					cwSetter:   k.SetCW721ERC721Pointer,
					cwGetter:   k.GetCW721ERC721Pointer,
				}
			},
			version: native.CurrentVersion,
		},
		{
			name: "ERC20NativePointer prevents pointer to cw1155 pointer",
			getHandlers: func(k *evmkeeper.Keeper) *handlers {
				return &handlers{
					evmSetter:  k.SetERC20NativePointer,
					evmGetter:  k.GetERC20NativePointer,
					evmDeleter: k.DeleteERC20NativePointer,
					cwSetter:   k.SetCW1155ERC1155Pointer,
					cwGetter:   k.GetCW1155ERC1155Pointer,
				}
			},
			version: native.CurrentVersion,
		},
		{
			name: "ERC20CW20Pointer prevents pointer to cw721 pointer",
			getHandlers: func(k *evmkeeper.Keeper) *handlers {
				return &handlers{
					evmSetter:  k.SetERC20CW20Pointer,
					evmGetter:  k.GetERC20CW20Pointer,
					evmDeleter: k.DeleteERC20CW20Pointer,
					cwSetter:   k.SetCW721ERC721Pointer,
					cwGetter:   k.GetCW721ERC721Pointer,
				}
			},
			version: cw20.CurrentVersion(ctx),
		},
		{
			name: "ERC20CW20Pointer prevents pointer to cw1155 pointer",
			getHandlers: func(k *evmkeeper.Keeper) *handlers {
				return &handlers{
					evmSetter:  k.SetERC20CW20Pointer,
					evmGetter:  k.GetERC20CW20Pointer,
					evmDeleter: k.DeleteERC20CW20Pointer,
					cwSetter:   k.SetCW1155ERC1155Pointer,
					cwGetter:   k.GetCW1155ERC1155Pointer,
				}
			},
			version: cw20.CurrentVersion(ctx),
		},
		{
			name: "ERC20CW20Pointer prevents pointer to cw20 pointer",
			getHandlers: func(k *evmkeeper.Keeper) *handlers {
				return &handlers{
					evmSetter:  k.SetERC20CW20Pointer,
					evmGetter:  k.GetERC20CW20Pointer,
					evmDeleter: k.DeleteERC20CW20Pointer,
					cwSetter:   k.SetCW20ERC20Pointer,
					cwGetter:   k.GetCW20ERC20Pointer,
				}
			},
			version: cw20.CurrentVersion(ctx),
		},
		{
			name: "ERC721CW721Pointer prevents pointer to cw721 pointer",
			getHandlers: func(k *evmkeeper.Keeper) *handlers {
				return &handlers{
					evmSetter:  k.SetERC721CW721Pointer,
					evmGetter:  k.GetERC721CW721Pointer,
					evmDeleter: k.DeleteERC721CW721Pointer,
					cwSetter:   k.SetCW721ERC721Pointer,
					cwGetter:   k.GetCW721ERC721Pointer,
				}
			},
			version: cw721.CurrentVersion,
		},
		{
			name: "ERC721CW721Pointer prevents pointer to cw1155 pointer",
			getHandlers: func(k *evmkeeper.Keeper) *handlers {
				return &handlers{
					evmSetter:  k.SetERC721CW721Pointer,
					evmGetter:  k.GetERC721CW721Pointer,
					evmDeleter: k.DeleteERC721CW721Pointer,
					cwSetter:   k.SetCW1155ERC1155Pointer,
					cwGetter:   k.GetCW1155ERC1155Pointer,
				}
			},
			version: cw721.CurrentVersion,
		},
		{
			name: "ERC721CW721Pointer prevents pointer to cw20 pointer",
			getHandlers: func(k *evmkeeper.Keeper) *handlers {
				return &handlers{
					evmSetter:  k.SetERC721CW721Pointer,
					evmGetter:  k.GetERC721CW721Pointer,
					evmDeleter: k.DeleteERC721CW721Pointer,
					cwSetter:   k.SetCW20ERC20Pointer,
					cwGetter:   k.GetCW20ERC20Pointer,
				}
			},
			version: cw721.CurrentVersion,
		},
		{
			name: "ERC1155CW1155Pointer prevents pointer to cw721 pointer",
			getHandlers: func(k *evmkeeper.Keeper) *handlers {
				return &handlers{
					evmSetter:  k.SetERC1155CW1155Pointer,
					evmGetter:  k.GetERC1155CW1155Pointer,
					evmDeleter: k.DeleteERC1155CW1155Pointer,
					cwSetter:   k.SetCW721ERC721Pointer,
					cwGetter:   k.GetCW721ERC721Pointer,
				}
			},
			version: cw1155.CurrentVersion,
		},
		{
			name: "ERC1155CW1155Pointer prevents pointer to cw1155 pointer",
			getHandlers: func(k *evmkeeper.Keeper) *handlers {
				return &handlers{
					evmSetter:  k.SetERC1155CW1155Pointer,
					evmGetter:  k.GetERC1155CW1155Pointer,
					evmDeleter: k.DeleteERC1155CW1155Pointer,
					cwSetter:   k.SetCW1155ERC1155Pointer,
					cwGetter:   k.GetCW1155ERC1155Pointer,
				}
			},
			version: cw1155.CurrentVersion,
		},
		{
			name: "ERC1155CW1155Pointer prevents pointer to cw20 pointer",
			getHandlers: func(k *evmkeeper.Keeper) *handlers {
				return &handlers{
					evmSetter:  k.SetERC1155CW1155Pointer,
					evmGetter:  k.GetERC1155CW1155Pointer,
					evmDeleter: k.DeleteERC1155CW1155Pointer,
					cwSetter:   k.SetCW20ERC20Pointer,
					cwGetter:   k.GetCW20ERC20Pointer,
				}
			},
			version: cw1155.CurrentVersion,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			k, ctx := testkeeper.MockEVMKeeper()
			handlers := test.getHandlers(k)
			cwAddress, evmAddress := testkeeper.MockAddressPair()

			// create a pointer
			require.Nil(t, handlers.evmSetter(ctx, cwAddress.String(), evmAddress))

			// should exist
			addr, _, exists := handlers.evmGetter(ctx, cwAddress.String())
			require.Equal(t, evmAddress, addr)
			require.True(t, exists)

			// should delete
			var version uint16 = 1
			if test.version != 0 {
				version = test.version
			}
			handlers.evmDeleter(ctx, cwAddress.String(), version)
			_, _, exists = handlers.evmGetter(ctx, cwAddress.String())
			require.False(t, exists)

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
			k, ctx := testkeeper.MockEVMKeeper()
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

package artifacts

import (
	"fmt"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/sei-protocol/sei-chain/x/evm/artifacts/cw1155"
	"github.com/sei-protocol/sei-chain/x/evm/artifacts/cw20"
	"github.com/sei-protocol/sei-chain/x/evm/artifacts/cw721"
	"github.com/sei-protocol/sei-chain/x/evm/artifacts/native"
)

func GetParsedABI(typ string) *abi.ABI {
	switch typ {
	case "native":
		return native.GetParsedABI()
	case "cw20":
		return cw20.GetParsedABI()
	case "cw721":
		return cw721.GetParsedABI()
	case "cw1155":
		return cw1155.GetParsedABI()
	default:
		panic(fmt.Sprintf("unknown artifact type %s", typ))
	}
}

func GetBin(typ string) []byte {
	switch typ {
	case "native":
		return native.GetBin()
	case "cw20":
		return cw20.GetBin()
	case "cw721":
		return cw721.GetBin()
	case "cw1155":
		return cw1155.GetBin()
	default:
		panic(fmt.Sprintf("unknown artifact type %s", typ))
	}
}

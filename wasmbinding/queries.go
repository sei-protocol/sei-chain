package wasmbinding

import (
	"encoding/json"
	"errors"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/sei-protocol/sei-chain/utils/metrics"
	epochwasm "github.com/sei-protocol/sei-chain/x/epoch/client/wasm"
	epochbindings "github.com/sei-protocol/sei-chain/x/epoch/client/wasm/bindings"
	epochtypes "github.com/sei-protocol/sei-chain/x/epoch/types"
	evmwasm "github.com/sei-protocol/sei-chain/x/evm/client/wasm"
	evmbindings "github.com/sei-protocol/sei-chain/x/evm/client/wasm/bindings"
	oraclewasm "github.com/sei-protocol/sei-chain/x/oracle/client/wasm"
	oraclebindings "github.com/sei-protocol/sei-chain/x/oracle/client/wasm/bindings"
	oracletypes "github.com/sei-protocol/sei-chain/x/oracle/types"
	tokenfactorywasm "github.com/sei-protocol/sei-chain/x/tokenfactory/client/wasm"
	tokenfactorybindings "github.com/sei-protocol/sei-chain/x/tokenfactory/client/wasm/bindings"
	tokenfactorytypes "github.com/sei-protocol/sei-chain/x/tokenfactory/types"
)

type QueryPlugin struct {
	oracleHandler       oraclewasm.OracleWasmQueryHandler
	epochHandler        epochwasm.EpochWasmQueryHandler
	tokenfactoryHandler tokenfactorywasm.TokenFactoryWasmQueryHandler
	evmHandler          evmwasm.EVMQueryHandler
}

// NewQueryPlugin returns a reference to a new QueryPlugin.
func NewQueryPlugin(oh *oraclewasm.OracleWasmQueryHandler, eh *epochwasm.EpochWasmQueryHandler, th *tokenfactorywasm.TokenFactoryWasmQueryHandler, evmh *evmwasm.EVMQueryHandler) *QueryPlugin {
	return &QueryPlugin{
		oracleHandler:       *oh,
		epochHandler:        *eh,
		tokenfactoryHandler: *th,
		evmHandler:          *evmh,
	}
}

func (qp QueryPlugin) HandleOracleQuery(ctx sdk.Context, queryData json.RawMessage) ([]byte, error) {
	var parsedQuery oraclebindings.SeiOracleQuery
	if err := json.Unmarshal(queryData, &parsedQuery); err != nil {
		return nil, oracletypes.ErrParsingOracleQuery
	}
	switch {
	case parsedQuery.ExchangeRates != nil:
		res, err := qp.oracleHandler.GetExchangeRates(ctx)
		if err != nil {
			return nil, err
		}
		bz, err := json.Marshal(res)
		if err != nil {
			return nil, oracletypes.ErrEncodingExchangeRates
		}

		return bz, nil
	case parsedQuery.OracleTwaps != nil:
		res, err := qp.oracleHandler.GetOracleTwaps(ctx, parsedQuery.OracleTwaps)
		if err != nil {
			return nil, err
		}
		bz, err := json.Marshal(res)
		if err != nil {
			return nil, oracletypes.ErrEncodingOracleTwaps
		}

		return bz, nil
	default:
		return nil, oracletypes.ErrUnknownSeiOracleQuery
	}
}

func (qp QueryPlugin) HandleEpochQuery(ctx sdk.Context, queryData json.RawMessage) ([]byte, error) {
	var parsedQuery epochbindings.SeiEpochQuery
	if err := json.Unmarshal(queryData, &parsedQuery); err != nil {
		return nil, epochtypes.ErrParsingSeiEpochQuery
	}
	switch {
	case parsedQuery.Epoch != nil:
		res, err := qp.epochHandler.GetEpoch(ctx, parsedQuery.Epoch)
		if err != nil {
			return nil, err
		}
		bz, err := json.Marshal(res)
		if err != nil {
			return nil, epochtypes.ErrEncodingEpoch
		}

		return bz, nil
	default:
		return nil, epochtypes.ErrUnknownSeiEpochQuery
	}
}

func (qp QueryPlugin) HandleTokenFactoryQuery(ctx sdk.Context, queryData json.RawMessage) ([]byte, error) {
	var parsedQuery tokenfactorybindings.SeiTokenFactoryQuery
	if err := json.Unmarshal(queryData, &parsedQuery); err != nil {
		return nil, tokenfactorytypes.ErrParsingSeiTokenFactoryQuery
	}
	switch {
	case parsedQuery.DenomAuthorityMetadata != nil:
		res, err := qp.tokenfactoryHandler.GetDenomAuthorityMetadata(ctx, parsedQuery.DenomAuthorityMetadata)
		if err != nil {
			return nil, err
		}
		bz, err := json.Marshal(res)
		if err != nil {
			return nil, tokenfactorytypes.ErrEncodingDenomAuthorityMetadata
		}

		return bz, nil
	case parsedQuery.DenomsFromCreator != nil:
		res, err := qp.tokenfactoryHandler.GetDenomsFromCreator(ctx, parsedQuery.DenomsFromCreator)
		if err != nil {
			return nil, err
		}
		bz, err := json.Marshal(res)
		if err != nil {
			return nil, tokenfactorytypes.ErrEncodingDenomsFromCreator
		}

		return bz, nil
	default:
		return nil, tokenfactorytypes.ErrUnknownSeiTokenFactoryQuery
	}
}

func (qp QueryPlugin) HandleEVMQuery(ctx sdk.Context, queryData json.RawMessage) (res []byte, err error) {
	var queryType evmbindings.EVMQueryType
	var parsedQuery evmbindings.SeiEVMQuery
	if err := json.Unmarshal(queryData, &parsedQuery); err != nil {
		return nil, errors.New("invalid EVM query")
	}
	queryType = parsedQuery.GetQueryType()

	defer func() {
		metrics.IncrementErrorMetrics(string(queryType), err)
	}()

	switch queryType {
	case evmbindings.StaticCallType:
		c := parsedQuery.StaticCall
		return qp.evmHandler.HandleStaticCall(ctx, c.From, c.To, c.Data)
	case evmbindings.ERC20TransferType:
		c := parsedQuery.ERC20TransferPayload
		return qp.evmHandler.HandleERC20TransferPayload(ctx, c.Recipient, c.Amount)
	case evmbindings.ERC20TransferFromType:
		c := parsedQuery.ERC20TransferFromPayload
		return qp.evmHandler.HandleERC20TransferFromPayload(ctx, c.Owner, c.Recipient, c.Amount)
	case evmbindings.ERC20ApproveType:
		c := parsedQuery.ERC20ApprovePayload
		return qp.evmHandler.HandleERC20ApprovePayload(ctx, c.Spender, c.Amount)
	case evmbindings.ERC20AllowanceType:
		c := parsedQuery.ERC20Allowance
		return qp.evmHandler.HandleERC20Allowance(ctx, c.ContractAddress, c.Owner, c.Spender)
	case evmbindings.ERC20TokenInfoType:
		c := parsedQuery.ERC20TokenInfo
		return qp.evmHandler.HandleERC20TokenInfo(ctx, c.ContractAddress, c.Caller)
	case evmbindings.ERC20BalanceType:
		c := parsedQuery.ERC20Balance
		return qp.evmHandler.HandleERC20Balance(ctx, c.ContractAddress, c.Account)
	case evmbindings.ERC721OwnerType:
		c := parsedQuery.ERC721Owner
		return qp.evmHandler.HandleERC721Owner(ctx, c.Caller, c.ContractAddress, c.TokenID)
	case evmbindings.ERC721TransferType:
		c := parsedQuery.ERC721TransferPayload
		return qp.evmHandler.HandleERC721TransferPayload(ctx, c.From, c.Recipient, c.TokenID)
	case evmbindings.ERC721ApproveType:
		c := parsedQuery.ERC721ApprovePayload
		return qp.evmHandler.HandleERC721ApprovePayload(ctx, c.Spender, c.TokenID)
	case evmbindings.ERC721SetApprovalAllType:
		c := parsedQuery.ERC721SetApprovalAllPayload
		return qp.evmHandler.HandleERC721SetApprovalAllPayload(ctx, c.To, c.Approved)
	case evmbindings.ERC721ApprovedType:
		c := parsedQuery.ERC721Approved
		return qp.evmHandler.HandleERC721Approved(ctx, c.Caller, c.ContractAddress, c.TokenID)
	case evmbindings.ERC721IsApprovedForAllType:
		c := parsedQuery.ERC721IsApprovedForAll
		return qp.evmHandler.HandleERC721IsApprovedForAll(ctx, c.Caller, c.ContractAddress, c.Owner, c.Operator)
	case evmbindings.ERC721TotalSupplyType:
		c := parsedQuery.ERC721TotalSupply
		return qp.evmHandler.HandleERC721TotalSupply(ctx, c.Caller, c.ContractAddress)
	case evmbindings.ERC721NameSymbolType:
		c := parsedQuery.ERC721NameSymbol
		return qp.evmHandler.HandleERC721NameSymbol(ctx, c.Caller, c.ContractAddress)
	case evmbindings.ERC721UriType:
		c := parsedQuery.ERC721Uri
		return qp.evmHandler.HandleERC721Uri(ctx, c.Caller, c.ContractAddress, c.TokenID)
	case evmbindings.ERC721RoyaltyInfoType:
		c := parsedQuery.ERC721RoyaltyInfo
		return qp.evmHandler.HandleERC721RoyaltyInfo(ctx, c.Caller, c.ContractAddress, c.TokenID, c.SalePrice)
	case evmbindings.ERC1155TransferType:
		c := parsedQuery.ERC1155TransferPayload
		return qp.evmHandler.HandleERC1155TransferPayload(ctx, c.From, c.Recipient, c.TokenID, c.Amount)
	case evmbindings.ERC1155BatchTransferType:
		c := parsedQuery.ERC1155BatchTransferPayload
		return qp.evmHandler.HandleERC1155BatchTransferPayload(ctx, c.From, c.Recipient, c.TokenIDs, c.Amounts)
	case evmbindings.ERC1155SetApprovalAllType:
		c := parsedQuery.ERC1155SetApprovalAllPayload
		return qp.evmHandler.HandleERC1155SetApprovalAllPayload(ctx, c.To, c.Approved)
	case evmbindings.ERC1155IsApprovedForAllType:
		c := parsedQuery.ERC1155IsApprovedForAll
		return qp.evmHandler.HandleERC1155IsApprovedForAll(ctx, c.Caller, c.ContractAddress, c.Owner, c.Operator)
	case evmbindings.ERC1155BalanceOfType:
		c := parsedQuery.ERC1155BalanceOf
		return qp.evmHandler.HandleERC1155BalanceOf(ctx, c.Caller, c.ContractAddress, c.Account, c.TokenID)
	case evmbindings.ERC1155BalanceOfBatchType:
		c := parsedQuery.ERC1155BalanceOfBatch
		return qp.evmHandler.HandleERC1155BalanceOfBatch(ctx, c.Caller, c.ContractAddress, c.Accounts, c.TokenIDs)
	case evmbindings.ERC1155UriType:
		c := parsedQuery.ERC1155Uri
		return qp.evmHandler.HandleERC1155Uri(ctx, c.Caller, c.ContractAddress, c.TokenID)
	case evmbindings.ERC1155TotalSupplyType:
		c := parsedQuery.ERC1155TotalSupply
		return qp.evmHandler.HandleERC1155TotalSupply(ctx, c.Caller, c.ContractAddress)
	case evmbindings.ERC1155TotalSupplyForTokenType:
		c := parsedQuery.ERC1155TotalSupplyForToken
		return qp.evmHandler.HandleERC1155TotalSupplyForToken(ctx, c.Caller, c.ContractAddress, c.TokenID)
	case evmbindings.ERC1155TokenExistsType:
		c := parsedQuery.ERC1155TokenExists
		return qp.evmHandler.HandleERC1155TokenExists(ctx, c.Caller, c.ContractAddress, c.TokenID)
	case evmbindings.ERC1155NameSymbolType:
		c := parsedQuery.ERC1155NameSymbol
		return qp.evmHandler.HandleERC1155NameSymbol(ctx, c.Caller, c.ContractAddress)
	case evmbindings.ERC1155RoyaltyInfoType:
		c := parsedQuery.ERC1155RoyaltyInfo
		return qp.evmHandler.HandleERC1155RoyaltyInfo(ctx, c.Caller, c.ContractAddress, c.TokenID, c.SalePrice)
	case evmbindings.GetEvmAddressType:
		c := parsedQuery.GetEvmAddress
		return qp.evmHandler.HandleGetEvmAddress(ctx, c.SeiAddress)
	case evmbindings.GetSeiAddressType:
		c := parsedQuery.GetSeiAddress
		return qp.evmHandler.HandleGetSeiAddress(ctx, c.EvmAddress)
	case evmbindings.SupportsInterfaceType:
		c := parsedQuery.SupportsInterface
		return qp.evmHandler.HandleSupportsInterface(ctx, c.Caller, c.InterfaceID, c.ContractAddress)
	default:
		return nil, errors.New("unknown EVM query")
	}
}

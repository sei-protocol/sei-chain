package acltokenfactorymapping

import (
	"encoding/hex"
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkacltypes "github.com/cosmos/cosmos-sdk/types/accesscontrol"
	aclkeeper "github.com/cosmos/cosmos-sdk/x/accesscontrol/keeper"
	acltypes "github.com/cosmos/cosmos-sdk/x/accesscontrol/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	tfktypes "github.com/sei-protocol/sei-chain/x/tokenfactory/types"
)

var ErrInvalidMessageType = fmt.Errorf("invalid message received for TokenFactory Module")

func GetTokenFactoryDependencyGenerators() aclkeeper.DependencyGeneratorMap {
	dependencyGeneratorMap := make(aclkeeper.DependencyGeneratorMap)
	MintMsgKey := acltypes.GenerateMessageKey(&tfktypes.MsgMint{})
	dependencyGeneratorMap[MintMsgKey] = TokenFactoryMintDependencyGenerator

	BurnMsgKey := acltypes.GenerateMessageKey(&tfktypes.MsgBurn{})
	dependencyGeneratorMap[BurnMsgKey] = TokenFactoryBurnDependencyGenerator

	return dependencyGeneratorMap
}

func TokenFactoryMintDependencyGenerator(keeper aclkeeper.Keeper, _ sdk.Context, msg sdk.Msg) ([]sdkacltypes.AccessOperation, error) {
	mintMsg, ok := msg.(*tfktypes.MsgMint)
	if !ok {
		return []sdkacltypes.AccessOperation{}, ErrInvalidMessageType
	}
	moduleAdr := keeper.AccountKeeper.GetModuleAddress(tfktypes.ModuleName)
	denom := mintMsg.GetAmount().Denom

	denomMetaDataKey := append([]byte(tfktypes.DenomAuthorityMetadataKey), []byte(denom)...)
	tokenfactoryDenomKey := tfktypes.GetDenomPrefixStore(denom)
	bankDenomMetaDataKey := banktypes.DenomMetadataKey(denom)
	supplyKey := hex.EncodeToString(append(banktypes.SupplyKey, []byte(denom)...))

	return []sdkacltypes.AccessOperation{
		// Reads denom data From BankKeeper
		{
			AccessType:         sdkacltypes.AccessType_READ,
			ResourceType:       sdkacltypes.ResourceType_KV_BANK_DENOM,
			IdentifierTemplate: hex.EncodeToString(bankDenomMetaDataKey),
		},

		// Gets Authoritity data related to the denom
		{
			AccessType:         sdkacltypes.AccessType_READ,
			ResourceType:       sdkacltypes.ResourceType_KV_TOKENFACTORY_METADATA,
			IdentifierTemplate: hex.EncodeToString(denomMetaDataKey),
		},

		// Gets Authoritity data related to the denom
		{
			AccessType:         sdkacltypes.AccessType_READ,
			ResourceType:       sdkacltypes.ResourceType_KV_TOKENFACTORY_DENOM,
			IdentifierTemplate: hex.EncodeToString(tokenfactoryDenomKey),
		},

		// Gets Module Account information
		{
			AccessType:         sdkacltypes.AccessType_READ,
			ResourceType:       sdkacltypes.ResourceType_KV_AUTH_ADDRESS_STORE,
			IdentifierTemplate: hex.EncodeToString(authtypes.AddressStoreKey(moduleAdr)),
		},
		{
			AccessType:         sdkacltypes.AccessType_READ,
			ResourceType:       sdkacltypes.ResourceType_KV_BANK_BALANCES,
			IdentifierTemplate: hex.EncodeToString(banktypes.CreateAccountBalancesPrefix(moduleAdr)),
		},
		{
			AccessType:         sdkacltypes.AccessType_WRITE,
			ResourceType:       sdkacltypes.ResourceType_KV_BANK_BALANCES,
			IdentifierTemplate: hex.EncodeToString(banktypes.CreateAccountBalancesPrefix(moduleAdr)),
		},

		// Deposit into Sender's Bank Balance
		{
			AccessType:         sdkacltypes.AccessType_READ,
			ResourceType:       sdkacltypes.ResourceType_KV_BANK_BALANCES,
			IdentifierTemplate: hex.EncodeToString(banktypes.CreateAccountBalancesPrefixFromBech32(mintMsg.GetSender())),
		},
		{
			AccessType:         sdkacltypes.AccessType_WRITE,
			ResourceType:       sdkacltypes.ResourceType_KV_BANK_BALANCES,
			IdentifierTemplate: hex.EncodeToString(banktypes.CreateAccountBalancesPrefixFromBech32(mintMsg.GetSender())),
		},

		// Read and update supply after burn
		{
			AccessType:         sdkacltypes.AccessType_READ,
			ResourceType:       sdkacltypes.ResourceType_KV_BANK_SUPPLY,
			IdentifierTemplate: supplyKey,
		},
		{
			AccessType:         sdkacltypes.AccessType_WRITE,
			ResourceType:       sdkacltypes.ResourceType_KV_BANK_SUPPLY,
			IdentifierTemplate: supplyKey,
		},

		{
			AccessType:         sdkacltypes.AccessType_READ,
			ResourceType:       sdkacltypes.ResourceType_KV_AUTH_ADDRESS_STORE,
			IdentifierTemplate: hex.EncodeToString(authtypes.CreateAddressStoreKeyFromBech32(mintMsg.GetSender())),
		},
		{
			AccessType:         sdkacltypes.AccessType_WRITE,
			ResourceType:       sdkacltypes.ResourceType_KV_AUTH_ADDRESS_STORE,
			IdentifierTemplate: hex.EncodeToString(authtypes.CreateAddressStoreKeyFromBech32(mintMsg.GetSender())),
		},
		// Last Operation should always be a commit
		*acltypes.CommitAccessOp(),
	}, nil
}

func TokenFactoryBurnDependencyGenerator(keeper aclkeeper.Keeper, _ sdk.Context, msg sdk.Msg) ([]sdkacltypes.AccessOperation, error) {
	burnMsg, ok := msg.(*tfktypes.MsgBurn)
	if !ok {
		return []sdkacltypes.AccessOperation{}, ErrInvalidMessageType
	}

	moduleAdr := keeper.AccountKeeper.GetModuleAddress(tfktypes.ModuleName)
	denom := burnMsg.GetAmount().Denom

	denomMetaDataKey := append([]byte(tfktypes.DenomAuthorityMetadataKey), []byte(denom)...)
	tokenfactoryDenomKey := tfktypes.GetDenomPrefixStore(denom)
	bankDenomMetaDataKey := banktypes.DenomMetadataKey(denom)
	supplyKey := hex.EncodeToString(append(banktypes.SupplyKey, []byte(denom)...))
	return []sdkacltypes.AccessOperation{
		// Reads denom data From BankKeeper
		{
			AccessType:         sdkacltypes.AccessType_READ,
			ResourceType:       sdkacltypes.ResourceType_KV_BANK_DENOM,
			IdentifierTemplate: hex.EncodeToString(bankDenomMetaDataKey),
		},

		// Gets Authoritity data related to the denom
		{
			AccessType:         sdkacltypes.AccessType_READ,
			ResourceType:       sdkacltypes.ResourceType_KV_TOKENFACTORY_METADATA,
			IdentifierTemplate: hex.EncodeToString(denomMetaDataKey),
		},

		// Gets Authoritity data related to the denom
		{
			AccessType:         sdkacltypes.AccessType_READ,
			ResourceType:       sdkacltypes.ResourceType_KV_TOKENFACTORY_DENOM,
			IdentifierTemplate: hex.EncodeToString(tokenfactoryDenomKey),
		},

		// Gets Module Account Balance
		{
			AccessType:         sdkacltypes.AccessType_READ,
			ResourceType:       sdkacltypes.ResourceType_KV_AUTH_ADDRESS_STORE,
			IdentifierTemplate: hex.EncodeToString(authtypes.AddressStoreKey(moduleAdr)),
		},
		{
			AccessType:         sdkacltypes.AccessType_READ,
			ResourceType:       sdkacltypes.ResourceType_KV_BANK_BALANCES,
			IdentifierTemplate: hex.EncodeToString(banktypes.CreateAccountBalancesPrefix(moduleAdr)),
		},
		{
			AccessType:         sdkacltypes.AccessType_WRITE,
			ResourceType:       sdkacltypes.ResourceType_KV_BANK_BALANCES,
			IdentifierTemplate: hex.EncodeToString(banktypes.CreateAccountBalancesPrefix(moduleAdr)),
		},

		// Checks balance for receiver
		{
			AccessType:         sdkacltypes.AccessType_READ,
			ResourceType:       sdkacltypes.ResourceType_KV_BANK_BALANCES,
			IdentifierTemplate: hex.EncodeToString(banktypes.CreateAccountBalancesPrefixFromBech32(burnMsg.GetSender())),
		},
		{
			AccessType:         sdkacltypes.AccessType_WRITE,
			ResourceType:       sdkacltypes.ResourceType_KV_BANK_BALANCES,
			IdentifierTemplate: hex.EncodeToString(banktypes.CreateAccountBalancesPrefixFromBech32(burnMsg.GetSender())),
		},

		// Read and update supply after burn
		{
			AccessType:         sdkacltypes.AccessType_READ,
			ResourceType:       sdkacltypes.ResourceType_KV_BANK_SUPPLY,
			IdentifierTemplate: supplyKey,
		},
		{
			AccessType:         sdkacltypes.AccessType_WRITE,
			ResourceType:       sdkacltypes.ResourceType_KV_BANK_SUPPLY,
			IdentifierTemplate: supplyKey,
		},

		{
			AccessType:         sdkacltypes.AccessType_READ,
			ResourceType:       sdkacltypes.ResourceType_KV_AUTH_ADDRESS_STORE,
			IdentifierTemplate: hex.EncodeToString(authtypes.CreateAddressStoreKeyFromBech32(burnMsg.GetSender())),
		},
		{
			AccessType:         sdkacltypes.AccessType_WRITE,
			ResourceType:       sdkacltypes.ResourceType_KV_AUTH_ADDRESS_STORE,
			IdentifierTemplate: hex.EncodeToString(authtypes.CreateAddressStoreKeyFromBech32(burnMsg.GetSender())),
		},

		// Last Operation should always be a commit
		*acltypes.CommitAccessOp(),
	}, nil
}

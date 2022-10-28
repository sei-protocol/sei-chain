package aclTokenFactorymapping

import (
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkacltypes "github.com/cosmos/cosmos-sdk/types/accesscontrol"
	aclkeeper "github.com/cosmos/cosmos-sdk/x/accesscontrol/keeper"
	acltypes "github.com/cosmos/cosmos-sdk/x/accesscontrol/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	utils "github.com/sei-protocol/sei-chain/aclmapping/utils"
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

func TokenFactoryMintDependencyGenerator(keeper aclkeeper.Keeper, ctx sdk.Context, msg sdk.Msg) ([]sdkacltypes.AccessOperation, error) {
	mintMsg, ok := msg.(*tfktypes.MsgMint)
	if !ok {
		return []sdkacltypes.AccessOperation{}, ErrInvalidMessageType
	}
	moduleAdr := keeper.AccountKeeper.GetModuleAddress(tfktypes.ModuleName)
	denom := mintMsg.GetAmount().Denom

	denomMetaDataKey := append([]byte(tfktypes.DenomAuthorityMetadataKey), []byte(denom)...)
	tokenfactoryDenomKey := tfktypes.GetDenomPrefixStore(denom)
	bankDenomMetaDataKey := banktypes.DenomMetadataKey(denom)
	supplyKey := string(append(banktypes.SupplyKey, []byte(denom)...))

	return []sdkacltypes.AccessOperation{
		// Reads denom data From BankKeeper
		{
			AccessType:         sdkacltypes.AccessType_READ,
			ResourceType:       sdkacltypes.ResourceType_KV_BANK_DENOM,
			IdentifierTemplate:  string(bankDenomMetaDataKey),
		},

		// Gets Authoritity data related to the denom
		{
			AccessType:         sdkacltypes.AccessType_READ,
			ResourceType:       sdkacltypes.ResourceType_KV_TOKENFACTORY_METADATA,
			IdentifierTemplate: string(denomMetaDataKey),
		},

		// Gets Authoritity data related to the denom
		{
			AccessType:         sdkacltypes.AccessType_READ,
			ResourceType:       sdkacltypes.ResourceType_KV_TOKENFACTORY_DENOM,
			IdentifierTemplate: string(tokenfactoryDenomKey),
		},

		// Gets Module Account information
		{
			AccessType:         sdkacltypes.AccessType_READ,
			ResourceType:       sdkacltypes.ResourceType_KV_AUTH_ADDRESS_STORE,
			IdentifierTemplate:  string(authtypes.AddressStoreKey(moduleAdr)),
		},
		{
			AccessType:         sdkacltypes.AccessType_READ,
			ResourceType:       sdkacltypes.ResourceType_KV_BANK_BALANCES,
			IdentifierTemplate:  string(banktypes.CreateAccountBalancesPrefix(moduleAdr)),
		},

		// Deposit into Sender's Bank Balance
		{
			AccessType:         sdkacltypes.AccessType_READ,
			ResourceType:       sdkacltypes.ResourceType_KV_BANK_BALANCES,
			IdentifierTemplate: string(banktypes.CreateAccountBalancesPrefixFromBech32(mintMsg.GetSender())),
		},
		{
			AccessType:         sdkacltypes.AccessType_WRITE,
			ResourceType:       sdkacltypes.ResourceType_KV_BANK_BALANCES,
			IdentifierTemplate: string(banktypes.CreateAccountBalancesPrefixFromBech32(mintMsg.GetSender())),
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
			IdentifierTemplate: string(authtypes.CreateAddressStoreKeyFromBech32(mintMsg.GetSender())),
		},
		{
			AccessType:         sdkacltypes.AccessType_WRITE,
			ResourceType:       sdkacltypes.ResourceType_KV_AUTH_ADDRESS_STORE,
			IdentifierTemplate: string(authtypes.CreateAddressStoreKeyFromBech32(mintMsg.GetSender())),
		},

		// Coins removed from Module account (Deferred)

		// Last Operation should always be a commit
		{
			ResourceType:       sdkacltypes.ResourceType_ANY,
			AccessType:         sdkacltypes.AccessType_COMMIT,
			IdentifierTemplate: utils.DefaultIDTemplate,
		},
	}, nil
}

func TokenFactoryBurnDependencyGenerator(keeper aclkeeper.Keeper, ctx sdk.Context, msg sdk.Msg) ([]sdkacltypes.AccessOperation, error) {
	burnMsg, ok := msg.(*tfktypes.MsgBurn)
	if !ok {
		return []sdkacltypes.AccessOperation{}, ErrInvalidMessageType
	}

	moduleAdr := keeper.AccountKeeper.GetModuleAddress(tfktypes.ModuleName)
	denom := burnMsg.GetAmount().Denom

	denomMetaDataKey := append([]byte(tfktypes.DenomAuthorityMetadataKey), []byte(denom)...)
	tokenfactoryDenomKey := tfktypes.GetDenomPrefixStore(denom)
	bankDenomMetaDataKey := banktypes.DenomMetadataKey(denom)
	supplyKey := string(append(banktypes.SupplyKey, []byte(denom)...))
	return []sdkacltypes.AccessOperation{
		// Reads denom data From BankKeeper
		{
			AccessType:         sdkacltypes.AccessType_READ,
			ResourceType:       sdkacltypes.ResourceType_KV_BANK_DENOM,
			IdentifierTemplate:  string(bankDenomMetaDataKey),
		},

		// Gets Authoritity data related to the denom
		{
			AccessType:         sdkacltypes.AccessType_READ,
			ResourceType:       sdkacltypes.ResourceType_KV_TOKENFACTORY_METADATA,
			IdentifierTemplate: string(denomMetaDataKey),
		},

		// Gets Authoritity data related to the denom
		{
			AccessType:         sdkacltypes.AccessType_READ,
			ResourceType:       sdkacltypes.ResourceType_KV_TOKENFACTORY_DENOM,
			IdentifierTemplate: string(tokenfactoryDenomKey),
		},

		// Gets Module Account Balance
		{
			AccessType:         sdkacltypes.AccessType_READ,
			ResourceType:       sdkacltypes.ResourceType_KV_AUTH,
			IdentifierTemplate:  string(authtypes.AddressStoreKey(moduleAdr)),
		},

		// Sends from Sender to Module account (deferred deposit)
		// Checks balance for receiver
		{
			AccessType:         sdkacltypes.AccessType_READ,
			ResourceType:       sdkacltypes.ResourceType_KV_BANK_BALANCES,
			IdentifierTemplate: string(banktypes.CreateAccountBalancesPrefixFromBech32(burnMsg.GetSender())),
		},
		{
			AccessType:         sdkacltypes.AccessType_WRITE,
			ResourceType:       sdkacltypes.ResourceType_KV_BANK_BALANCES,
			IdentifierTemplate: string(banktypes.CreateAccountBalancesPrefixFromBech32(burnMsg.GetSender())),
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
			IdentifierTemplate: string(authtypes.CreateAddressStoreKeyFromBech32(burnMsg.GetSender())),
		},
		{
			AccessType:         sdkacltypes.AccessType_WRITE,
			ResourceType:       sdkacltypes.ResourceType_KV_AUTH_ADDRESS_STORE,
			IdentifierTemplate: string(authtypes.CreateAddressStoreKeyFromBech32(burnMsg.GetSender())),
		},

		// Coins removed from Module account (Deferred)

		// Last Operation should always be a commit
		{
			ResourceType:       sdkacltypes.ResourceType_ANY,
			AccessType:         sdkacltypes.AccessType_COMMIT,
			IdentifierTemplate: utils.DefaultIDTemplate,
		},
	}, nil
}

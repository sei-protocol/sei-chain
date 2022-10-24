package main

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"os/exec"
	"strconv"
	"strings"
	"time"

	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	tokenfactorytypes "github.com/sei-protocol/sei-chain/x/tokenfactory/types"

	cryptotypes "github.com/cosmos/cosmos-sdk/crypto/types"
	"github.com/cosmos/cosmos-sdk/std"

	"github.com/cosmos/cosmos-sdk/x/auth/tx"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"

	"github.com/cosmos/cosmos-sdk/codec"
	"github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/sei-protocol/sei-chain/app"
	dextypes "github.com/sei-protocol/sei-chain/x/dex/types"
)

var (
	TestConfig         EncodingConfig
	StakingQueryClient stakingtypes.QueryClient
	//// Staking specific variables
	Validators []stakingtypes.Validator
	// DelegationMap is a map of delegator -> validator -> delegated amount
	DelegationMap map[string]map[string]int
	//// Tokenfactory specific variables
	TokenFactoryDenomOwner map[string]string
)

const (
	VortexData = "{\"position_effect\":\"Open\",\"leverage\":\"1\"}"
)

var FromMili = sdk.NewDec(1000000)

func init() {
	cdc := codec.NewLegacyAmino()
	interfaceRegistry := types.NewInterfaceRegistry()
	marshaler := codec.NewProtoCodec(interfaceRegistry)

	TestConfig = EncodingConfig{
		InterfaceRegistry: interfaceRegistry,
		Marshaler:         marshaler,
		TxConfig:          tx.NewTxConfig(marshaler, tx.DefaultSignModes),
		Amino:             cdc,
	}
	std.RegisterLegacyAminoCodec(TestConfig.Amino)
	std.RegisterInterfaces(TestConfig.InterfaceRegistry)
	app.ModuleBasics.RegisterLegacyAminoCodec(TestConfig.Amino)
	app.ModuleBasics.RegisterInterfaces(TestConfig.InterfaceRegistry)
}

func run() {
	client := NewLoadTestClient()
	client.SetValidators()
	config := client.LoadTestConfig

	defer client.Close()

	if config.TxsPerBlock < config.MsgsPerTx {
		panic("Must have more TxsPerBlock than MsgsPerTx")
	}

	configString, _ := json.Marshal(config)
	fmt.Printf("Running with \n %s \n", string(configString))

	fmt.Printf("%s - Starting block prepare\n", time.Now().Format("2006-01-02T15:04:05"))
	workgroups, sendersList := client.BuildTxs()

	client.SendTxs(workgroups, sendersList)

	// Records the resulting TxHash to file
	client.WriteTxHashToFile()
	fmt.Printf("%s - Finished\n", time.Now().Format("2006-01-02T15:04:05"))
}

func generateMessage(config Config, key cryptotypes.PrivKey, msgPerTx uint64) (sdk.Msg, bool) {
	var msg sdk.Msg
	messageTypes := strings.Split(config.MessageType, ",")
	rand.Seed(time.Now().UnixNano())
	messageType := messageTypes[rand.Intn(len(messageTypes))]
	fmt.Printf("Message type: %s\n", messageType)
	switch messageType {
	case Bank:
		msg = &banktypes.MsgSend{
			FromAddress: sdk.AccAddress(key.PubKey().Address()).String(),
			ToAddress:   sdk.AccAddress(key.PubKey().Address()).String(),
			Amount: sdk.NewCoins(sdk.Coin{
				Denom:  "usei",
				Amount: sdk.NewInt(1),
			}),
		}
	case Dex:
		price := config.PriceDistr.Sample()
		quantity := config.QuantityDistr.Sample()
		contract := config.ContractDistr.Sample()
		orderPlacements := generateDexOrderPlacements(config, key, msgPerTx, price, quantity)
		amount, err := sdk.ParseCoinsNormalized(fmt.Sprintf("%d%s", price.Mul(quantity).Ceil().RoundInt64(), "usei"))
		if err != nil {
			panic(err)
		}
		msg = &dextypes.MsgPlaceOrders{
			Creator:      sdk.AccAddress(key.PubKey().Address()).String(),
			Orders:       orderPlacements,
			ContractAddr: contract,
			Funds:        amount,
		}
	case Staking:
		delegatorAddr := sdk.AccAddress(key.PubKey().Address()).String()
		chosenValidator := Validators[rand.Intn(len(Validators))].OperatorAddress
		// Randomly pick someone to redelegate / unbond from
		srcAddr := ""
		for k := range DelegationMap[delegatorAddr] {
			if k == chosenValidator {
				continue
			}
			srcAddr = k
			break
		}
		msg = generateStakingMsg(delegatorAddr, chosenValidator, srcAddr)
	case Tokenfactory:
		denomCreatorAddr := sdk.AccAddress(key.PubKey().Address()).String()
		// No denoms, let's mint
		randNum := rand.Float64()
		denom, ok := TokenFactoryDenomOwner[denomCreatorAddr]
		switch {
		case !ok || randNum <= 0.33:
			subDenom := fmt.Sprintf("tokenfactory-created-denom-%d", time.Now().UnixMilli())
			denom = fmt.Sprintf("factory/%s/%s", denomCreatorAddr, subDenom)
			msg = &tokenfactorytypes.MsgCreateDenom{
				Sender:   denomCreatorAddr,
				Subdenom: subDenom,
			}
			TokenFactoryDenomOwner[denomCreatorAddr] = denom
		case randNum <= 0.66:
			msg = &tokenfactorytypes.MsgMint{
				Sender: denomCreatorAddr,
				Amount: sdk.Coin{Denom: denom, Amount: sdk.NewInt(1000000)},
			}
		default:
			msg = &tokenfactorytypes.MsgBurn{
				Sender: denomCreatorAddr,
				Amount: sdk.Coin{Denom: denom, Amount: sdk.NewInt(1)},
			}
		}
	case FailureBankMalformed:
		var denom string
		if rand.Float64() < 0.5 {
			denom = "unknown"
		} else {
			denom = "other"
		}
		msg = &banktypes.MsgSend{
			FromAddress: sdk.AccAddress(key.PubKey().Address()).String(),
			ToAddress:   sdk.AccAddress(key.PubKey().Address()).String(),
			Amount: sdk.NewCoins(sdk.Coin{
				Denom:  denom,
				Amount: sdk.NewInt(1),
			}),
		}
	case FailureBankInvalid:
		var amountUsei int64
		amountUsei = 1000000000000000000
		msg = &banktypes.MsgSend{
			FromAddress: sdk.AccAddress(key.PubKey().Address()).String(),
			ToAddress:   sdk.AccAddress(key.PubKey().Address()).String(),
			Amount: sdk.NewCoins(sdk.Coin{
				Denom:  "usei",
				Amount: sdk.NewInt(amountUsei),
			}),
		}
	case FailureDexMalformed:
		price := config.PriceDistr.InvalidSample()
		quantity := config.QuantityDistr.InvalidSample()
		contract := config.ContractDistr.Sample()
		orderPlacements := generateDexOrderPlacements(config, key, msgPerTx, price, quantity)
		amount, err := sdk.ParseCoinsNormalized(fmt.Sprintf("%d%s", price.Mul(quantity).Ceil().RoundInt64(), "usei"))
		if err != nil {
			panic(err)
		}
		msg = &dextypes.MsgPlaceOrders{
			Creator:      sdk.AccAddress(key.PubKey().Address()).String(),
			Orders:       orderPlacements,
			ContractAddr: contract,
			Funds:        amount,
		}
	case FailureDexInvalid:
		price := config.PriceDistr.Sample()
		quantity := config.QuantityDistr.Sample()
		contract := config.ContractDistr.Sample()
		orderPlacements := generateDexOrderPlacements(config, key, msgPerTx, price, quantity)
		var amountUsei int64
		if rand.Float64() < 0.5 {
			amountUsei = 10000 * price.Mul(quantity).Ceil().RoundInt64()
		} else {
			amountUsei = 0
		}
		amount, err := sdk.ParseCoinsNormalized(fmt.Sprintf("%d%s", amountUsei, "usei"))
		if err != nil {
			panic(err)
		}
		msg = &dextypes.MsgPlaceOrders{
			Creator:      sdk.AccAddress(key.PubKey().Address()).String(),
			Orders:       orderPlacements,
			ContractAddr: contract,
			Funds:        amount,
		}

	default:
		fmt.Printf("Unrecognized message type %s", config.MessageType)
	}

	if strings.Contains(config.MessageType, "failure") {
		return msg, true
	}
	return msg, false
}

func generateDexOrderPlacements(config Config, key cryptotypes.PrivKey, batchSize uint64, price sdk.Dec, quantity sdk.Dec) []*dextypes.Order {
	orderPlacements := []*dextypes.Order{}
	var orderType dextypes.OrderType
	if config.MessageType == "failure_bank_malformed" {
		orderType = -1
	} else {
		dexMsgType := config.MsgTypeDistr.Sample()
		switch dexMsgType {
		case Limit:
			orderType = dextypes.OrderType_LIMIT
		case Market:
			orderType = dextypes.OrderType_MARKET
		default:
			panic(fmt.Sprintf("Unknown message type %s\n", dexMsgType))
		}
	}
	var direction dextypes.PositionDirection
	if rand.Float64() < 0.5 {
		direction = dextypes.PositionDirection_LONG
	} else {
		direction = dextypes.PositionDirection_SHORT
	}

	contract := config.ContractDistr.Sample()
	for j := 0; j < int(batchSize); j++ {
		orderPlacements = append(orderPlacements, &dextypes.Order{
			Account:           sdk.AccAddress(key.PubKey().Address()).String(),
			ContractAddr:      contract,
			PositionDirection: direction,
			Price:             price.Quo(FromMili),
			Quantity:          quantity.Quo(FromMili),
			PriceDenom:        "SEI",
			AssetDenom:        "ATOM",
			OrderType:         orderType,
			Data:              VortexData,
		})
	}
	return orderPlacements
}

func generateStakingMsg(delegatorAddr string, chosenValidator string, srcAddr string) sdk.Msg {
	// Randomly unbond, redelegate or delegate
	// However, if there are no delegations, do so first
	var msg sdk.Msg
	randNum := rand.Float64()
	if _, ok := DelegationMap[delegatorAddr]; !ok || randNum <= 0.33 || srcAddr == "" {
		msg = &stakingtypes.MsgDelegate{
			DelegatorAddress: delegatorAddr,
			ValidatorAddress: chosenValidator,
			Amount:           sdk.Coin{Denom: "usei", Amount: sdk.NewInt(1)},
		}
		DelegationMap[delegatorAddr] = map[string]int{}
		DelegationMap[delegatorAddr][chosenValidator] = 1
	} else {

		if randNum <= 0.66 {
			msg = &stakingtypes.MsgBeginRedelegate{
				DelegatorAddress:    delegatorAddr,
				ValidatorSrcAddress: srcAddr,
				ValidatorDstAddress: chosenValidator,
				Amount:              sdk.Coin{Denom: "usei", Amount: sdk.NewInt(1)},
			}
			DelegationMap[delegatorAddr][chosenValidator]++
		} else {
			msg = &stakingtypes.MsgUndelegate{
				DelegatorAddress: delegatorAddr,
				ValidatorAddress: srcAddr,
				Amount:           sdk.Coin{Denom: "usei", Amount: sdk.NewInt(1)},
			}
		}
		// Update delegation map
		DelegationMap[delegatorAddr][srcAddr]--
		if DelegationMap[delegatorAddr][srcAddr] == 0 {
			delete(DelegationMap, delegatorAddr)
		}
	}
	return msg
}

func getLastHeight() int {
	out, err := exec.Command("curl", "http://localhost:26657/blockchain").Output()
	if err != nil {
		panic(err)
	}
	var dat map[string]interface{}
	if err := json.Unmarshal(out, &dat); err != nil {
		panic(err)
	}
	height, err := strconv.Atoi(dat["last_height"].(string))
	if err != nil {
		panic(err)
	}
	return height
}

func main() {
	run()
}

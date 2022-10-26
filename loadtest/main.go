package main

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"os/exec"
	"strconv"
	"time"

	cryptotypes "github.com/cosmos/cosmos-sdk/crypto/types"
	"github.com/cosmos/cosmos-sdk/std"

	"github.com/cosmos/cosmos-sdk/x/auth/tx"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"

	"github.com/cosmos/cosmos-sdk/codec"
	"github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	"github.com/sei-protocol/sei-chain/app"
	dextypes "github.com/sei-protocol/sei-chain/x/dex/types"
	oracletypes "github.com/sei-protocol/sei-chain/x/oracle/types"
)

var TestConfig EncodingConfig

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

func generateMessage(c *LoadTestClient, key cryptotypes.PrivKey, msgPerTx uint64, validators []Validator) sdk.Msg {
	var msg sdk.Msg
	switch c.LoadTestConfig.MessageType {
	case "basic":
		msg = &banktypes.MsgSend{
			FromAddress: sdk.AccAddress(key.PubKey().Address()).String(),
			ToAddress:   sdk.AccAddress(key.PubKey().Address()).String(),
			Amount: sdk.NewCoins(sdk.Coin{
				Denom:  "usei",
				Amount: sdk.NewInt(1),
			}),
		}
	case "staking":
		msgType := c.LoadTestConfig.MsgTypeDistr.SampleStakingMsgs()

		switch msgType {
		case "delegate":
			msg = &stakingtypes.MsgDelegate{
				DelegatorAddress: sdk.AccAddress(key.PubKey().Address()).String(),
				ValidatorAddress: validators[rand.Intn(len(validators))].OpperatorAddr,
				Amount:           sdk.Coin{Denom: "usei", Amount: sdk.NewInt(5)},
			}
		case "undelegate":
			msg = &stakingtypes.MsgUndelegate{
				DelegatorAddress: sdk.AccAddress(key.PubKey().Address()).String(),
				ValidatorAddress: validators[rand.Intn(len(validators))].OpperatorAddr,
				Amount:           sdk.Coin{Denom: "usei", Amount: sdk.NewInt(1)},
			}
		case "begin_redelegate":
			msg = &stakingtypes.MsgBeginRedelegate{
				DelegatorAddress:    sdk.AccAddress(key.PubKey().Address()).String(),
				ValidatorSrcAddress: validators[rand.Intn(len(validators))].OpperatorAddr,
				ValidatorDstAddress: validators[rand.Intn(len(validators))].OpperatorAddr,
				Amount:              sdk.Coin{Denom: "usei", Amount: sdk.NewInt(1)},
			}
		default:
			panic("Unknown message type")
		}
	case "dex":
		msgType := c.LoadTestConfig.MsgTypeDistr.SampleDexMsgs()
		switch msgType {
		case "place_order":
			orderPlacements := []*dextypes.Order{}
			var direction dextypes.PositionDirection
			if rand.Float64() < 0.5 {
				direction = dextypes.PositionDirection_LONG
			} else {
				direction = dextypes.PositionDirection_SHORT
			}
			orderType := c.LoadTestConfig.OrderTypeDistr.SampleOrderType()
			price := c.LoadTestConfig.PriceDistr.Sample()
			quantity := c.LoadTestConfig.QuantityDistr.Sample()
			contract := c.LoadTestConfig.ContractDistr.Sample()
			for j := 0; j < int(msgPerTx); j++ {
				order := &dextypes.Order{
					Account:           sdk.AccAddress(key.PubKey().Address()).String(),
					ContractAddr:      contract,
					PositionDirection: direction,
					Price:             price.Quo(FromMili),
					Quantity:          quantity.Quo(FromMili),
					PriceDenom:        "SEI",
					AssetDenom:        "ATOM",
					OrderType:         orderType,
					Data:              VortexData,
				}
				orderPlacements = append(orderPlacements, order)
			}
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
		case "cancel_order":
			var contract string
			var outstandingOrders []*dextypes.Order
			for _, contractConfig := range c.LoadTestConfig.ContractDistr {

				if resp, err := c.DexQueryClient.GetOrders(context.Background(), &dextypes.QueryGetOrdersRequest{
					ContractAddr: contractConfig.ContractAddr,
					Account:      sdk.AccAddress(key.PubKey().Address()).String(),
				}); err != nil {
					panic(err)
				} else if len(resp.Orders) > 0 {
					contract = contractConfig.ContractAddr
					outstandingOrders = resp.Orders
					break
				}
			}

			cancelPlacements := []*dextypes.Cancellation{}
			for j := 0; j < int(msgPerTx); j++ {
				if len(outstandingOrders) > 0 {
					order := outstandingOrders[len(outstandingOrders)-1]
					outstandingOrders = outstandingOrders[:len(outstandingOrders)-1]
					cancelPlacements = append(cancelPlacements, &dextypes.Cancellation{
						Id:                order.Id,
						Initiator:         dextypes.CancellationInitiator_USER,
						Creator:           order.Account,
						ContractAddr:      order.ContractAddr,
						PriceDenom:        order.PriceDenom,
						AssetDenom:        order.AssetDenom,
						PositionDirection: order.PositionDirection,
						Price:             order.Price,
					})
				}
			}

			msg = &dextypes.MsgCancelOrders{
				Creator:       sdk.AccAddress(key.PubKey().Address()).String(),
				Cancellations: cancelPlacements,
				ContractAddr:  contract,
			}
		default:
			panic("Unknown message type")
		}

	default:
		fmt.Printf("Unrecognized message type %s", c.LoadTestConfig.MessageType)
	}
	return msg
}

func generateOracleMessage(key cryptotypes.PrivKey) sdk.Msg {
	valAddr := sdk.ValAddress(key.PubKey().Address()).String()
	addr := sdk.AccAddress(key.PubKey().Address()).String()
	msg := &oracletypes.MsgAggregateExchangeRateVote{
		ExchangeRates: "1usei,2uatom",
		Feeder:        addr,
		Validator:     valAddr,
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

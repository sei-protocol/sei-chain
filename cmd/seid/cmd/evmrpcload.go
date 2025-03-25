package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/sei-protocol/sei-chain/utils"
	"github.com/spf13/cobra"

	"github.com/cosmos/cosmos-sdk/server"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	ethrpc "github.com/ethereum/go-ethereum/rpc"

	"net/http"
)

const flagEvmRPCLoadConfig = "config"

//nolint:gosec
func EvmRPCLoadTestCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "evm-rpc-load",
		Short: "loadtest EVM RPC endpoints",
		Long:  "loadtest EVM RPC endpoints",
		Args:  cobra.ExactArgs(0),
		RunE: func(cmd *cobra.Command, _ []string) error {
			serverCtx := server.GetServerContextFromCmd(cmd)
			if err := serverCtx.Viper.BindPFlags(cmd.Flags()); err != nil {
				return err
			}

			config := LoadConfig(serverCtx.Viper.GetString(flagEvmRPCLoadConfig))
			config.SetMaxIdleConnsPerHost()
			rpcclient, err := ethrpc.Dial(config.Endpoint)
			if err != nil {
				return err
			}
			defer rpcclient.Close()
			ctx, cancel := context.WithTimeout(context.Background(), config.Duration)
			defer cancel()
			callCounts := make([]atomic.Int64, len(config.LoadList))
			latencies := make([]atomic.Int64, len(config.LoadList))
			wg := sync.WaitGroup{}
			for i, load := range config.LoadList {
				i := i
				load := load
				typedParams := load.GetTypedParams()
				for j := 0; j < load.Concurrency; j++ {
					wg.Add(1)
					go func() {
						defer wg.Done()
						for {
							select {
							case <-ctx.Done():
								return
							default:
								var res interface{}
								now := time.Now()
								if err := rpcclient.CallContext(ctx, &res, load.Method, typedParams...); err != nil {
									if !strings.Contains(err.Error(), "context deadline exceeded") {
										fmt.Printf("calling %s encountered error %s\n", load.Method, err)
									}
									return
								}
								callCounts[i].Add(1)
								latencies[i].Add(time.Since(now).Microseconds())
								if !load.CheckRes(res) {
									return
								}
							}
						}
					}()
				}
			}
			wg.Wait()
			for i := range config.LoadList {
				callCount := callCounts[i].Load()
				fmt.Printf("made %d requests for load %d with an average latency of %d micro seconds\n", callCount, i, latencies[i].Load()/callCount)
			}
			return nil
		},
	}

	cmd.Flags().String(flagEvmRPCLoadConfig, "config/rpc_load.json", "Path to config file")

	return cmd
}

type EvmRPCLoadConfig struct {
	Endpoint string        `json:"endpoint"`
	Duration time.Duration `json:"duration"`
	LoadList []EvmRPCLoad  `json:"load_list"`
}

func LoadConfig(path string) EvmRPCLoadConfig {
	configFile, err := os.Open(path)
	if err != nil {
		panic(err)
	}
	configBz, err := io.ReadAll(configFile)
	if err != nil {
		panic(err)
	}
	var config EvmRPCLoadConfig
	if err := json.Unmarshal(configBz, &config); err != nil {
		panic(err)
	}
	return config
}

func (config EvmRPCLoadConfig) SetMaxIdleConnsPerHost() {
	totalConns := 0
	for _, load := range config.LoadList {
		totalConns += load.Concurrency
	}
	http.DefaultTransport.(*http.Transport).MaxIdleConnsPerHost = totalConns
}

type EvmRPCLoad struct {
	Concurrency    int      `json:"concurrency"`
	Method         string   `json:"method"`
	Params         []string `json:"params"`
	ExpectedResult string   `json:"expected_result"`
}

func (load EvmRPCLoad) GetTypedParams() []interface{} {
	switch load.Method {
	case "eth_getBalance":
		return []interface{}{
			common.HexToAddress(load.Params[0]),
			load.Params[1],
		}
	case "eth_getBlockByHash":
		return []interface{}{
			common.HexToHash(load.Params[0]),
			true,
		}
	case "eth_getBlockReceipts":
		return []interface{}{load.Params[0]}
	case "eth_getBlockByNumber":
		return []interface{}{
			sdk.MustNewDecFromStr(load.Params[0]).TruncateInt().BigInt(),
			true,
		}
	case "eth_getTransactionByHash", "eth_getTransactionReceipt":
		return []interface{}{common.HexToHash(load.Params[0])}
	case "eth_estimateGas":
		return []interface{}{DecodeCallArgsStr(load.Params[0])}
	case "eth_call":
		return []interface{}{
			DecodeCallArgsStr(load.Params[0]),
			load.Params[1],
		}
	case "eth_getLogs":
		return []interface{}{
			DecodeFilterArgsStr(load.Params[0]),
		}
	}
	panic(fmt.Sprintf("unknown load method %s", load.Method))
}

func (load EvmRPCLoad) CheckRes(res interface{}) bool {
	serializedRes, _ := json.Marshal(res)
	if load.ExpectedResult != "" && string(serializedRes) != load.ExpectedResult {
		fmt.Printf("calling %s expected %s but got %s\n", load.Method, load.ExpectedResult, string(serializedRes))
		return false
	}
	return true
}

func DecodeCallArgsStr(str string) map[string]interface{} {
	splitted := strings.Split(str, ",")
	res := map[string]interface{}{}
	res["from"] = common.HexToAddress(splitted[0])
	res["to"] = HexToAddressPtr(splitted[1])
	if splitted[2] != "" {
		res["input"] = hexutil.Bytes(common.Hex2Bytes(splitted[2]))
	}
	if splitted[3] != "" {
		res["value"] = (*hexutil.Big)(sdk.MustNewDecFromStr(splitted[3]).TruncateInt().BigInt())
	}
	if splitted[4] != "" {
		gas, _ := strconv.ParseUint(splitted[4], 10, 64)
		res["gas"] = hexutil.Uint64(gas)
	}
	if splitted[5] != "" {
		res["gasPrice"] = (*hexutil.Big)(sdk.MustNewDecFromStr(splitted[5]).TruncateInt().BigInt())
	}
	if splitted[6] != "" {
		res["maxFeePerGas"] = (*hexutil.Big)(sdk.MustNewDecFromStr(splitted[6]).TruncateInt().BigInt())
	}
	if splitted[7] != "" {
		res["maxPriorityFeePerGas"] = (*hexutil.Big)(sdk.MustNewDecFromStr(splitted[7]).TruncateInt().BigInt())
	}
	return res
}

func DecodeFilterArgsStr(str string) map[string]interface{} {
	splitted := strings.Split(str, ",")
	res := map[string]interface{}{}
	addresses := strings.Split(splitted[0], "-")
	res["address"] = utils.Map(addresses, common.HexToAddress)
	topicsList := [][]common.Hash{}
	for _, topics := range strings.Split(splitted[1], "-") {
		topicsList = append(topicsList, utils.Map(
			strings.Split(topics, "|"),
			common.HexToHash,
		))
	}
	res["topics"] = topicsList
	if splitted[2] != "" {
		res["blockHash"] = common.HexToHash(splitted[2])
		return res
	}
	res["fromBlock"] = splitted[3]
	res["toBlock"] = splitted[4]
	return res
}

func HexToAddressPtr(str string) *common.Address {
	if str == "" {
		return nil
	}
	a := common.HexToAddress(str)
	return &a
}

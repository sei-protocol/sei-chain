package evmrpc

import (
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFeeHistory(t *testing.T) {
	bodyByNumber := "{\"jsonrpc\": \"2.0\",\"method\": \"eth_feeHistory\",\"params\":[\"0x1\",\"0x8\",[0.5]],\"id\":\"test\"}"
	bodyByLatest := "{\"jsonrpc\": \"2.0\",\"method\": \"eth_feeHistory\",\"params\":[\"0x1\",\"latest\",[0.5]],\"id\":\"test\"}"
	bodyByEarliest := "{\"jsonrpc\": \"2.0\",\"method\": \"eth_feeHistory\",\"params\":[\"0x1\",\"earliest\",[0.5]],\"id\":\"test\"}"
	bodyOld := "{\"jsonrpc\": \"2.0\",\"method\": \"eth_feeHistory\",\"params\":[\"0x1\",\"0x1\",[0.5]],\"id\":\"test\"}"
	bodyFuture := "{\"jsonrpc\": \"2.0\",\"method\": \"eth_feeHistory\",\"params\":[\"0x1\",\"0x9\",[0.5]],\"id\":\"test\"}"
	for body, expectedOldest := range map[string]string{
		bodyByNumber: "0x8", bodyByLatest: "0x8", bodyByEarliest: "0x1", bodyOld: "0x1", bodyFuture: "0x8",
	} {
		req, err := http.NewRequest(http.MethodGet, fmt.Sprintf("http://%s:%d", TestAddr, TestPort), strings.NewReader(body))
		require.Nil(t, err)
		req.Header.Set("Content-Type", "application/json")
		res, err := http.DefaultClient.Do(req)
		require.Nil(t, err)
		resBody, err := io.ReadAll(res.Body)
		require.Nil(t, err)
		resObj := map[string]interface{}{}
		require.Nil(t, json.Unmarshal(resBody, &resObj))
		resObj = resObj["result"].(map[string]interface{})
		require.Equal(t, expectedOldest, resObj["oldestBlock"].(string))
		rewards := resObj["reward"].([]interface{})
		require.Equal(t, 1, len(rewards))
		reward := rewards[0].([]interface{})
		require.Equal(t, 1, len(reward))
		require.Equal(t, "0xa", reward[0].(string))
		baseFeePerGas := resObj["baseFeePerGas"].([]interface{})
		require.Equal(t, 1, len(baseFeePerGas))
		require.Equal(t, "0x0", baseFeePerGas[0].(string))
		gasUsedRatio := resObj["gasUsedRatio"].([]interface{})
		require.Equal(t, 1, len(gasUsedRatio))
		require.Equal(t, 0.5, gasUsedRatio[0].(float64))
	}

	// bad percentile
	outOfRangeBody1 := "{\"jsonrpc\": \"2.0\",\"method\": \"eth_feeHistory\",\"params\":[\"0x1\",\"0x8\",[-1]],\"id\":\"test\"}"
	outOfRangeBody2 := "{\"jsonrpc\": \"2.0\",\"method\": \"eth_feeHistory\",\"params\":[\"0x1\",\"0x8\",[101]],\"id\":\"test\"}"
	outOfOrderBody := "{\"jsonrpc\": \"2.0\",\"method\": \"eth_feeHistory\",\"params\":[\"0x1\",\"0x8\",[99, 1]],\"id\":\"test\"}"
	for _, body := range []string{outOfRangeBody1, outOfRangeBody2, outOfOrderBody} {
		req, err := http.NewRequest(http.MethodGet, fmt.Sprintf("http://%s:%d", TestAddr, TestPort), strings.NewReader(body))
		require.Nil(t, err)
		req.Header.Set("Content-Type", "application/json")
		res, err := http.DefaultClient.Do(req)
		require.Nil(t, err)
		resBody, err := io.ReadAll(res.Body)
		require.Nil(t, err)
		resObj := map[string]interface{}{}
		require.Nil(t, json.Unmarshal(resBody, &resObj))
		errMap := resObj["error"].(map[string]interface{})
		require.Equal(t, "invalid reward percentiles: must be ascending and between 0 and 100", errMap["message"].(string))
	}
}

func TestCalculatePercentiles(t *testing.T) {
	// all empty
	result := calculatePercentiles([]float64{}, []gasAndReward{}, 0)
	require.Equal(t, 0, len(result))

	// empty gasAndRewards
	result = calculatePercentiles([]float64{1}, []gasAndReward{}, 0)
	require.Equal(t, 0, len(result))

	// empty percentiles
	result = calculatePercentiles([]float64{}, []gasAndReward{{reward: 10, gasUsed: 1}}, 1)
	require.Equal(t, 0, len(result))

	// 0 percentile
	result = calculatePercentiles([]float64{0}, []gasAndReward{{reward: 10, gasUsed: 1}}, 1)
	require.Equal(t, 1, len(result))
	// see comments above calculatePercentiles to understand why it should return 10 here
	require.Equal(t, big.NewInt(10), result[0].ToInt())

	// 100 percentile
	result = calculatePercentiles([]float64{100}, []gasAndReward{{reward: 10, gasUsed: 1}}, 1)
	require.Equal(t, 1, len(result))
	require.Equal(t, big.NewInt(10), result[0].ToInt())

	// 0 percentile and 100 percentile with just one transaction
	result = calculatePercentiles([]float64{0, 100}, []gasAndReward{{reward: 10, gasUsed: 1}}, 1)
	require.Equal(t, 2, len(result))
	require.Equal(t, big.NewInt(10), result[0].ToInt())
	require.Equal(t, big.NewInt(10), result[1].ToInt())

	// more transactions than percentiles
	result = calculatePercentiles([]float64{0, 50, 100}, []gasAndReward{{reward: 10, gasUsed: 1}, {reward: 5, gasUsed: 2}, {reward: 3, gasUsed: 3}}, 6)
	require.Equal(t, 3, len(result))
	require.Equal(t, big.NewInt(3), result[0].ToInt())
	require.Equal(t, big.NewInt(3), result[1].ToInt())
	require.Equal(t, big.NewInt(10), result[2].ToInt())
}

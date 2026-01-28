package testing

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/ethereum/go-ethereum/common"
	seiutils "github.com/sei-protocol/sei-chain/utils"
)

func GetTransactionReads(url string, txHash common.Hash) map[common.Address]map[common.Hash]common.Hash {
	res := sendRequestWithNamespace(url, "debug", "traceTransaction", txHash, map[string]interface{}{})
	if res["result"] == nil {
		panic(fmt.Sprintf("failed to get transaction reads for tx %s", txHash.Hex()))
	}
	logs := res["result"].(map[string]interface{})["structLogs"].([]interface{})
	allReads := map[common.Address]map[common.Hash]common.Hash{}
	receipt := GetTransactionReceipt(url, txHash)
	entryContract := common.HexToAddress(receipt["to"].(string))
	if code := GetCode(url, entryContract); code == "" {
		// to is EOA
		return allReads
	}
	contractStack := []common.Address{entryContract}
	lastDepth := 1
	for _, log := range logs {
		logMap := log.(map[string]interface{})
		for range lastDepth - int(logMap["depth"].(float64)) {
			contractStack = contractStack[:len(contractStack)-1]
		}
		stack := logMap["stack"].([]interface{})
		switch logMap["op"].(string) {
		case "CALL", "STATICCALL":
			contractStack = append(contractStack, common.HexToAddress(stack[len(stack)-2].(string)))
		case "DELEGATECALL", "CALLCODE":
			contractStack = append(contractStack, contractStack[len(contractStack)-1])
		case "SLOAD":
			read := common.HexToHash(stack[len(stack)-1].(string))
			reads, ok := allReads[contractStack[len(contractStack)-1]]
			if !ok {
				reads = map[common.Hash]common.Hash{}
			}
			reads[read] = common.Hash{}
			allReads[contractStack[len(contractStack)-1]] = reads
		}
		lastDepth = int(logMap["depth"].(float64))
	}
	height := receipt["blockNumber"].(string)
	for address, reads := range allReads {
		for read := range reads {
			reads[read] = GetState(url, address, read, height)
		}
	}
	return allReads
}

func GetTransactionReceipt(url string, txHash common.Hash) map[string]interface{} {
	res := sendRequestWithNamespace(url, "eth", "getTransactionReceipt", txHash)
	if res["result"] == nil {
		panic(fmt.Sprintf("failed to get transaction receipt for tx %s", txHash.Hex()))
	}
	return res["result"].(map[string]interface{})
}

func GetCode(url string, address common.Address) string {
	res := sendRequestWithNamespace(url, "eth", "getCode", address, "latest")
	if res["result"] == nil {
		panic(fmt.Sprintf("failed to get code for address %s", address.Hex()))
	}
	return res["result"].(string)
}

func GetState(url string, address common.Address, key common.Hash, height string) common.Hash {
	res := sendRequestWithNamespace(url, "eth", "getStorageAt", address, key.Hex(), height)
	if res["result"] == nil {
		panic(fmt.Sprintf("failed to get state for address %s and key %s at height %s", address.Hex(), key.Hex(), height))
	}
	return common.HexToHash(res["result"].(string))
}

func sendRequestWithNamespace(url string, namespace string, method string, params ...interface{}) map[string]interface{} {
	paramsFormatted := ""
	if len(params) > 0 {
		paramsFormatted = strings.Join(seiutils.Map(params, formatParam), ",")
	}
	body := fmt.Sprintf("{\"jsonrpc\": \"2.0\",\"method\": \"%s_%s\",\"params\":[%s],\"id\":\"test\"}", namespace, method, paramsFormatted)
	req, _ := http.NewRequest(http.MethodPost, url, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		panic(err)
	}
	defer func() { _ = res.Body.Close() }()
	resBody, _ := io.ReadAll(res.Body)
	resObj := map[string]interface{}{}
	_ = json.Unmarshal(resBody, &resObj)
	return resObj
}

func formatParam(p interface{}) string {
	if p == nil {
		return "null"
	}
	switch v := p.(type) {
	case bool:
		if v {
			return "true"
		}
		return "false"
	case int:
		return fmt.Sprintf("%d", v)
	case float64:
		return fmt.Sprintf("%f", v)
	case string:
		return fmt.Sprintf("\"%s\"", v)
	case common.Address:
		return fmt.Sprintf("\"%s\"", v)
	case []common.Address:
		wrapper := func(i common.Address) string {
			return formatParam(i)
		}
		return fmt.Sprintf("[%s]", strings.Join(seiutils.Map(v, wrapper), ","))
	case common.Hash:
		return fmt.Sprintf("\"%s\"", v)
	case []common.Hash:
		wrapper := func(i common.Hash) string {
			return formatParam(i)
		}
		return fmt.Sprintf("[%s]", strings.Join(seiutils.Map(v, wrapper), ","))
	case [][]common.Hash:
		wrapper := func(i []common.Hash) string {
			return formatParam(i)
		}
		return fmt.Sprintf("[%s]", strings.Join(seiutils.Map(v, wrapper), ","))
	case []string:
		return fmt.Sprintf("[%s]", strings.Join(v, ","))
	case []float64:
		return fmt.Sprintf("[%s]", strings.Join(seiutils.Map(v, func(s float64) string { return fmt.Sprintf("%f", s) }), ","))
	case []interface{}:
		return fmt.Sprintf("[%s]", strings.Join(seiutils.Map(v, formatParam), ","))
	case map[string]interface{}:
		kvs := []string{}
		for k, v := range v {
			kvs = append(kvs, fmt.Sprintf("\"%s\":%s", k, formatParam(v)))
		}
		return fmt.Sprintf("{%s}", strings.Join(kvs, ","))
	case map[string]map[string]interface{}:
		kvs := []string{}
		for k, v := range v {
			kvs = append(kvs, fmt.Sprintf("\"%s\":%s", k, formatParam(v)))
		}
		return fmt.Sprintf("{%s}", strings.Join(kvs, ","))
	default:
		return fmt.Sprintf("%s", p)
	}
}

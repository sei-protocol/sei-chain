package types

import (
	"context"
	"encoding/json"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"

	"github.com/sei-protocol/sei-chain/loadtest_v2/config"
)

// LoadTx is a wrapper that has pre-encoded json rpc payload and eth transaction.
type LoadTx struct {
	EthTx          *ethtypes.Transaction
	JSONRPCPayload []byte
	Payload        []byte
	Scenario       *TxScenario
}

// JSONRPCRequest represents json rpc request.
type JSONRPCRequest struct {
	Version string          `json:"jsonrpc,omitempty"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
}

func toJSONRequestBytes(rawTx []byte) ([]byte, error) {
	req := &JSONRPCRequest{
		Version: "2.0",
		Method:  "eth_sendRawTransaction",
		Params:  json.RawMessage(fmt.Sprintf(`["0x%x"]`, rawTx)),
		ID:      json.RawMessage("0"),
	}
	b, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	return b, nil
}

// ShardID returns the shard id for the given number of shards.
func (tx *LoadTx) ShardID(n int) int {
	addressBigInt := new(big.Int).SetBytes(tx.Scenario.Sender.Address.Bytes())
	mod := new(big.Int).Mod(addressBigInt, big.NewInt(int64(n)))
	return int(mod.Int64())
}

// TxScenario captures the scenario of this test transaction.
type TxScenario struct {
	Name     string
	Nonce    uint64
	Sender   *Account
	Receiver common.Address
}

// TxGenerator is an interface for generating transactions.
type TxGenerator interface {
	Deploy(ctx context.Context, config *config.LoadConfig, deployer *Account) common.Address
	Generate(scenario *TxScenario) *LoadTx
}

func CreateTxFromEthTx(tx *ethtypes.Transaction, scenario *TxScenario) *LoadTx {
	// Convert to raw transaction bytes for JSON-RPC payload
	rawTx, err := tx.MarshalBinary()
	if err != nil {
		panic("Failed to marshal transaction: " + err.Error())
	}

	// Create JSON-RPC payload
	jsonRPCPayload, err := toJSONRequestBytes(rawTx)
	if err != nil {
		panic("Failed to create JSON-RPC payload: " + err.Error())
	}

	// Return the complete LoadTx object
	return &LoadTx{
		EthTx:          tx,
		JSONRPCPayload: jsonRPCPayload,
		Payload:        rawTx,
		Scenario:       scenario,
	}
}

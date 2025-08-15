package report

import (
	"regexp"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

// Service interface for report generation
type Service interface {
	Start(ctx sdk.Context) error
	Name() string
	Status() string
}

// PostgreSQL configuration
type PostgreSQLConfig struct {
	Host     string
	Port     int
	Database string
	User     string
	Password string
	SSLMode  string
}

// Worker communication data structures
type AccountWork struct {
	Account sdk.AccAddress
}

type AccountData struct {
	Account    string
	EVMAddress string
	EVMNonce   uint64
	Sequence   uint64
	Associated bool
	Bucket     string
}

type AssetData struct {
	Name       string
	Type       string
	Label      string
	CodeID     uint64
	Creator    string
	Admin      string
	HasAdmin   bool
	HasPointer bool
	Pointer    string
}

type BalanceData struct {
	AccountID string
	AssetID   string
	Balance   string
	TokenID   string
}

// Legacy types for backward compatibility
type Account struct {
	Account       string `json:"account"`
	EVMAddress    string `json:"evmAddress"`
	EVMNonce      uint64 `json:"evmNonce"`
	Sequence      uint64 `json:"sequence"`
	IsAssociated  bool   `json:"associated"`
	IsEVMContract bool   `json:"isEvmContract"`
	IsCWContract  bool   `json:"isCWContract"`
	IsMultisig    bool   `json:"isMultisig"`
}

type Coin struct {
	Denom      string `json:"denom"`
	HasPointer bool   `json:"hasPointer"`
	Pointer    string `json:"pointer,omitempty"`
}

type CoinBalance struct {
	Account string `json:"account"`
	Denom   string `json:"denom"`
	Amount  string `json:"amount"`
}

// Token-related types
type AccountsResponse struct {
	Accounts []string `json:"accounts"`
}

type TokensResponse struct {
	Tokens []string `json:"tokens"`
}

type OwnerResponse struct {
	Owner string `json:"owner"`
}

type BalanceResponse struct {
	Balance string `json:"balance"`
}

// Global regex patterns for token type detection
var (
	CW20ErrorRegex = regexp.MustCompile(`Generic error: Querying contract: query wasm contract failed: invalid request: (.*?): execute wasm contract failed`)
	CW721ErrorRegex = regexp.MustCompile(`Generic error: Querying contract: query wasm contract failed: invalid request: (.*?): execute wasm contract failed`)
)

// Token type normalization mapping
var TypeNormalizationMap = map[string]string{
	"cw20-base":         "cw20",
	"cw721-base":        "cw721",
	"cw721-metadata-onchain": "cw721",
	// Add more mappings as needed
}

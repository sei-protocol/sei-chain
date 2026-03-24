package evmrpc

import (
	"sort"
	"strings"

	"github.com/ethereum/go-ethereum/rpc"
)

// SeiLegacyDeprecationHTTPHeader is set on HTTP responses that successfully forwarded an allowlisted
// gated sei_* / sei2_* JSON-RPC call (body is unchanged; clients should not rely on JSON result mutation).
const (
	SeiLegacyDeprecationHTTPHeader = "Sei-Legacy-RPC-Deprecation"
	SeiLegacyDeprecationMessage    = "All sei_* and sei2_* JSON-RPC methods are deprecated and scheduled for removal; migrate to eth_* and supported APIs."
)

// errSeiLegacyNotEnabled is returned when a gated sei_* / sei2_* method is not listed in enabled_legacy_sei_apis.
// It follows github.com/ethereum/go-ethereum/rpc error encoding (jsonrpcMessage.error via rpc.Error / rpc.DataError).
type errSeiLegacyNotEnabled struct {
	method string
}

func (e *errSeiLegacyNotEnabled) Error() string {
	return seiLegacyMethodDisabledMessage(e.method)
}

func (e *errSeiLegacyNotEnabled) ErrorCode() int {
	return -32601
}

func (e *errSeiLegacyNotEnabled) ErrorData() interface{} {
	return "legacy_sei_deprecated"
}

var (
	_ rpc.Error     = (*errSeiLegacyNotEnabled)(nil)
	_ rpc.DataError = (*errSeiLegacyNotEnabled)(nil)
)

// seiLegacyGatedMethods is the full set of JSON-RPC methods on the sei and sei2 namespaces that
// are subject to [evm] enabled_legacy_sei_apis in app.toml (same allowlist for both prefixes).
var seiLegacyGatedMethods = map[string]struct{}{
	"sei_associate":                             {},
	"sei_getBlockByHash":                        {},
	"sei_getBlockByNumber":                      {},
	"sei_getBlockReceipts":                      {},
	"sei_getBlockTransactionCountByHash":        {},
	"sei_getBlockTransactionCountByNumber":      {},
	"sei_getBlockByHashExcludeTraceFail":        {},
	"sei_getBlockByNumberExcludeTraceFail":      {},
	"sei_getCosmosTx":                           {},
	"sei_getEVMAddress":                         {},
	"sei_getEvmTx":                              {},
	"sei_getFilterChanges":                      {},
	"sei_getFilterLogs":                         {},
	"sei_getLogs":                               {},
	"sei_getSeiAddress":                         {},
	"sei_getTransactionByBlockHashAndIndex":     {},
	"sei_getTransactionByBlockNumberAndIndex":   {},
	"sei_getTransactionByHash":                  {},
	"sei_getTransactionCount":                   {},
	"sei_getTransactionErrorByHash":             {},
	"sei_getTransactionReceipt":                 {},
	"sei_getTransactionReceiptExcludeTraceFail": {},
	"sei_getVMError":                            {},
	"sei_newBlockFilter":                        {},
	"sei_newFilter":                             {},
	"sei_sign":                                  {},
	"sei_traceBlockByHashExcludeTraceFail":      {},
	"sei_traceBlockByNumberExcludeTraceFail":    {},
	"sei_uninstallFilter":                       {},
	// sei2_* block namespace (HTTP only; bank transfers in blocks). Gated via the same allowlist.
	"sei2_getBlockByHash":                   {},
	"sei2_getBlockByHashExcludeTraceFail":   {},
	"sei2_getBlockByNumber":                 {},
	"sei2_getBlockByNumberExcludeTraceFail": {},
	"sei2_getBlockReceipts":                 {},
	"sei2_getBlockTransactionCountByHash":   {},
	"sei2_getBlockTransactionCountByNumber": {},
}

// SeiLegacyAllExtraMethodNames returns gated sei_* methods other than the usual default trio
// (sei_getSeiAddress, sei_getEVMAddress, sei_getCosmosTx). Used to compose full test configs.
func SeiLegacyAllExtraMethodNames() []string {
	out := make([]string, 0, len(seiLegacyGatedMethods))
	for m := range seiLegacyGatedMethods {
		switch strings.ToLower(m) {
		case "sei_getseiaddress", "sei_getevmaddress", "sei_getcosmostx":
			continue
		default:
			out = append(out, m)
		}
	}
	sort.Strings(out)
	return out
}

// SeiLegacyAllGatedMethodNames returns every gated sei_* and sei2_* method (sorted). Use when tests need full parity.
func SeiLegacyAllGatedMethodNames() []string {
	out := make([]string, 0, len(seiLegacyGatedMethods))
	for m := range seiLegacyGatedMethods {
		out = append(out, m)
	}
	sort.Strings(out)
	return out
}

// BuildSeiLegacyEnabledSet returns the set of allowed gated sei_* / sei2_* JSON-RPC methods from
// config only ([evm].enabled_legacy_sei_apis). Names are matched case-insensitively to canonical RPC names.
func BuildSeiLegacyEnabledSet(enabledLegacySeiApis []string) map[string]struct{} {
	enabled := make(map[string]struct{}, len(enabledLegacySeiApis))
	for _, raw := range enabledLegacySeiApis {
		name := strings.TrimSpace(raw)
		if name == "" {
			continue
		}
		canonical := canonicalizeSeiLegacyMethodName(name)
		if canonical == "" {
			continue
		}
		if _, ok := seiLegacyGatedMethods[canonical]; ok {
			enabled[canonical] = struct{}{}
		}
	}
	return enabled
}

func canonicalizeSeiLegacyMethodName(name string) string {
	lower := strings.ToLower(strings.TrimSpace(name))
	for m := range seiLegacyGatedMethods {
		if strings.ToLower(m) == lower {
			return m
		}
	}
	return ""
}

func seiLegacyMethodDisabledMessage(method string) string {
	return method + " is not enabled on this node. The sei_* and sei2_* JSON-RPC surfaces are deprecated, scheduled for removal, and should not be used for new integrations - " +
		"prefer standard eth_* (and debug_*) methods and official migration guidance. " +
		"To allow this legacy method, add it to enabled_legacy_sei_apis under [evm] in app.toml."
}

func seiLegacyIsGatedNamespaceMethod(method string) bool {
	return strings.HasPrefix(method, "sei2_") || strings.HasPrefix(method, "sei_")
}

// seiLegacyGateError enforces [evm].enabled_legacy_sei_apis when allowlist is non-nil.
// allowlist nil means ungated (HTTP middleware disabled, or non-enforcing paths).
func seiLegacyGateError(method string, allowlist map[string]struct{}) error {
	if allowlist == nil {
		return nil
	}
	if !seiLegacyIsGatedNamespaceMethod(method) {
		return nil
	}
	canon := canonicalizeSeiLegacyMethodName(method)
	if canon == "" {
		// Fail closed: sei_* / sei2_* names not in seiLegacyGatedMethods must not bypass the allowlist
		// (e.g. future handlers or typos would otherwise reach the inner server).
		return &errSeiLegacyNotEnabled{method: strings.TrimSpace(method)}
	}
	if _, ok := allowlist[canon]; ok {
		return nil
	}
	return &errSeiLegacyNotEnabled{method: canon}
}

// seiLegacyForwardedGatedMethod is true when the request method is a gated sei_* / sei2_* name listed
// in the allowlist (the call was forwarded to the inner JSON-RPC server). Used only for optional HTTP metadata.
func seiLegacyForwardedGatedMethod(method string, allowlist map[string]struct{}) bool {
	if allowlist == nil {
		return false
	}
	if !seiLegacyIsGatedNamespaceMethod(method) {
		return false
	}
	canon := canonicalizeSeiLegacyMethodName(method)
	if canon == "" {
		return false
	}
	_, ok := allowlist[canon]
	return ok
}

package rest

import (
	"encoding/json"
	"net/http"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/tx"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/rest"
	govrest "github.com/cosmos/cosmos-sdk/x/gov/client/rest"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"

	"github.com/CosmWasm/wasmd/x/wasm/types"
)

type StoreCodeProposalJSONReq struct {
	BaseReq rest.BaseReq `json:"base_req" yaml:"base_req"`

	Title       string    `json:"title" yaml:"title"`
	Description string    `json:"description" yaml:"description"`
	Proposer    string    `json:"proposer" yaml:"proposer"`
	Deposit     sdk.Coins `json:"deposit" yaml:"deposit"`

	RunAs string `json:"run_as" yaml:"run_as"`
	// WASMByteCode can be raw or gzip compressed
	WASMByteCode []byte `json:"wasm_byte_code" yaml:"wasm_byte_code"`
	// InstantiatePermission to apply on contract creation, optional
	InstantiatePermission *types.AccessConfig `json:"instantiate_permission" yaml:"instantiate_permission"`
}

func (s StoreCodeProposalJSONReq) Content() govtypes.Content {
	return &types.StoreCodeProposal{
		Title:                 s.Title,
		Description:           s.Description,
		RunAs:                 s.RunAs,
		WASMByteCode:          s.WASMByteCode,
		InstantiatePermission: s.InstantiatePermission,
	}
}

func (s StoreCodeProposalJSONReq) GetProposer() string {
	return s.Proposer
}

func (s StoreCodeProposalJSONReq) GetDeposit() sdk.Coins {
	return s.Deposit
}

func (s StoreCodeProposalJSONReq) GetBaseReq() rest.BaseReq {
	return s.BaseReq
}

func StoreCodeProposalHandler(cliCtx client.Context) govrest.ProposalRESTHandler {
	return govrest.ProposalRESTHandler{
		SubRoute: "wasm_store_code",
		Handler: func(w http.ResponseWriter, r *http.Request) {
			var req StoreCodeProposalJSONReq
			if !rest.ReadRESTReq(w, r, cliCtx.LegacyAmino, &req) {
				return
			}
			toStdTxResponse(cliCtx, w, req)
		},
	}
}

type InstantiateProposalJSONReq struct {
	BaseReq rest.BaseReq `json:"base_req" yaml:"base_req"`

	Title       string `json:"title" yaml:"title"`
	Description string `json:"description" yaml:"description"`

	Proposer string    `json:"proposer" yaml:"proposer"`
	Deposit  sdk.Coins `json:"deposit" yaml:"deposit"`

	RunAs string `json:"run_as" yaml:"run_as"`
	// Admin is an optional address that can execute migrations
	Admin string          `json:"admin,omitempty" yaml:"admin"`
	Code  uint64          `json:"code_id" yaml:"code_id"`
	Label string          `json:"label" yaml:"label"`
	Msg   json.RawMessage `json:"msg" yaml:"msg"`
	Funds sdk.Coins       `json:"funds" yaml:"funds"`
}

func (s InstantiateProposalJSONReq) Content() govtypes.Content {
	return &types.InstantiateContractProposal{
		Title:       s.Title,
		Description: s.Description,
		RunAs:       s.RunAs,
		Admin:       s.Admin,
		CodeID:      s.Code,
		Label:       s.Label,
		Msg:         types.RawContractMessage(s.Msg),
		Funds:       s.Funds,
	}
}

func (s InstantiateProposalJSONReq) GetProposer() string {
	return s.Proposer
}

func (s InstantiateProposalJSONReq) GetDeposit() sdk.Coins {
	return s.Deposit
}

func (s InstantiateProposalJSONReq) GetBaseReq() rest.BaseReq {
	return s.BaseReq
}

func InstantiateProposalHandler(cliCtx client.Context) govrest.ProposalRESTHandler {
	return govrest.ProposalRESTHandler{
		SubRoute: "wasm_instantiate",
		Handler: func(w http.ResponseWriter, r *http.Request) {
			var req InstantiateProposalJSONReq
			if !rest.ReadRESTReq(w, r, cliCtx.LegacyAmino, &req) {
				return
			}
			toStdTxResponse(cliCtx, w, req)
		},
	}
}

type MigrateProposalJSONReq struct {
	BaseReq rest.BaseReq `json:"base_req" yaml:"base_req"`

	Title       string `json:"title" yaml:"title"`
	Description string `json:"description" yaml:"description"`

	Proposer string    `json:"proposer" yaml:"proposer"`
	Deposit  sdk.Coins `json:"deposit" yaml:"deposit"`

	Contract string          `json:"contract" yaml:"contract"`
	Code     uint64          `json:"code_id" yaml:"code_id"`
	Msg      json.RawMessage `json:"msg" yaml:"msg"`
}

func (s MigrateProposalJSONReq) Content() govtypes.Content {
	return &types.MigrateContractProposal{
		Title:       s.Title,
		Description: s.Description,
		Contract:    s.Contract,
		CodeID:      s.Code,
		Msg:         types.RawContractMessage(s.Msg),
	}
}

func (s MigrateProposalJSONReq) GetProposer() string {
	return s.Proposer
}

func (s MigrateProposalJSONReq) GetDeposit() sdk.Coins {
	return s.Deposit
}

func (s MigrateProposalJSONReq) GetBaseReq() rest.BaseReq {
	return s.BaseReq
}

func MigrateProposalHandler(cliCtx client.Context) govrest.ProposalRESTHandler {
	return govrest.ProposalRESTHandler{
		SubRoute: "wasm_migrate",
		Handler: func(w http.ResponseWriter, r *http.Request) {
			var req MigrateProposalJSONReq
			if !rest.ReadRESTReq(w, r, cliCtx.LegacyAmino, &req) {
				return
			}
			toStdTxResponse(cliCtx, w, req)
		},
	}
}

type ExecuteProposalJSONReq struct {
	BaseReq rest.BaseReq `json:"base_req" yaml:"base_req"`

	Title       string `json:"title" yaml:"title"`
	Description string `json:"description" yaml:"description"`

	Proposer string    `json:"proposer" yaml:"proposer"`
	Deposit  sdk.Coins `json:"deposit" yaml:"deposit"`

	Contract string          `json:"contract" yaml:"contract"`
	Msg      json.RawMessage `json:"msg" yaml:"msg"`
	// RunAs is the role that is passed to the contract's environment
	RunAs string    `json:"run_as" yaml:"run_as"`
	Funds sdk.Coins `json:"funds" yaml:"funds"`
}

func (s ExecuteProposalJSONReq) Content() govtypes.Content {
	return &types.ExecuteContractProposal{
		Title:       s.Title,
		Description: s.Description,
		Contract:    s.Contract,
		Msg:         types.RawContractMessage(s.Msg),
		RunAs:       s.RunAs,
		Funds:       s.Funds,
	}
}

func (s ExecuteProposalJSONReq) GetProposer() string {
	return s.Proposer
}

func (s ExecuteProposalJSONReq) GetDeposit() sdk.Coins {
	return s.Deposit
}

func (s ExecuteProposalJSONReq) GetBaseReq() rest.BaseReq {
	return s.BaseReq
}

func ExecuteProposalHandler(cliCtx client.Context) govrest.ProposalRESTHandler {
	return govrest.ProposalRESTHandler{
		SubRoute: "wasm_execute",
		Handler: func(w http.ResponseWriter, r *http.Request) {
			var req ExecuteProposalJSONReq
			if !rest.ReadRESTReq(w, r, cliCtx.LegacyAmino, &req) {
				return
			}
			toStdTxResponse(cliCtx, w, req)
		},
	}
}

type SudoProposalJSONReq struct {
	BaseReq rest.BaseReq `json:"base_req" yaml:"base_req"`

	Title       string `json:"title" yaml:"title"`
	Description string `json:"description" yaml:"description"`

	Proposer string    `json:"proposer" yaml:"proposer"`
	Deposit  sdk.Coins `json:"deposit" yaml:"deposit"`

	Contract string          `json:"contract" yaml:"contract"`
	Msg      json.RawMessage `json:"msg" yaml:"msg"`
}

func (s SudoProposalJSONReq) Content() govtypes.Content {
	return &types.SudoContractProposal{
		Title:       s.Title,
		Description: s.Description,
		Contract:    s.Contract,
		Msg:         types.RawContractMessage(s.Msg),
	}
}

func (s SudoProposalJSONReq) GetProposer() string {
	return s.Proposer
}

func (s SudoProposalJSONReq) GetDeposit() sdk.Coins {
	return s.Deposit
}

func (s SudoProposalJSONReq) GetBaseReq() rest.BaseReq {
	return s.BaseReq
}

func SudoProposalHandler(cliCtx client.Context) govrest.ProposalRESTHandler {
	return govrest.ProposalRESTHandler{
		SubRoute: "wasm_sudo",
		Handler: func(w http.ResponseWriter, r *http.Request) {
			var req SudoProposalJSONReq
			if !rest.ReadRESTReq(w, r, cliCtx.LegacyAmino, &req) {
				return
			}
			toStdTxResponse(cliCtx, w, req)
		},
	}
}

type UpdateAdminJSONReq struct {
	BaseReq rest.BaseReq `json:"base_req" yaml:"base_req"`

	Title       string `json:"title" yaml:"title"`
	Description string `json:"description" yaml:"description"`

	Proposer string    `json:"proposer" yaml:"proposer"`
	Deposit  sdk.Coins `json:"deposit" yaml:"deposit"`

	NewAdmin string `json:"new_admin" yaml:"new_admin"`
	Contract string `json:"contract" yaml:"contract"`
}

func (s UpdateAdminJSONReq) Content() govtypes.Content {
	return &types.UpdateAdminProposal{
		Title:       s.Title,
		Description: s.Description,
		Contract:    s.Contract,
		NewAdmin:    s.NewAdmin,
	}
}

func (s UpdateAdminJSONReq) GetProposer() string {
	return s.Proposer
}

func (s UpdateAdminJSONReq) GetDeposit() sdk.Coins {
	return s.Deposit
}

func (s UpdateAdminJSONReq) GetBaseReq() rest.BaseReq {
	return s.BaseReq
}

func UpdateContractAdminProposalHandler(cliCtx client.Context) govrest.ProposalRESTHandler {
	return govrest.ProposalRESTHandler{
		SubRoute: "wasm_update_admin",
		Handler: func(w http.ResponseWriter, r *http.Request) {
			var req UpdateAdminJSONReq
			if !rest.ReadRESTReq(w, r, cliCtx.LegacyAmino, &req) {
				return
			}
			toStdTxResponse(cliCtx, w, req)
		},
	}
}

type ClearAdminJSONReq struct {
	BaseReq rest.BaseReq `json:"base_req" yaml:"base_req"`

	Title       string `json:"title" yaml:"title"`
	Description string `json:"description" yaml:"description"`

	Proposer string    `json:"proposer" yaml:"proposer"`
	Deposit  sdk.Coins `json:"deposit" yaml:"deposit"`

	Contract string `json:"contract" yaml:"contract"`
}

func (s ClearAdminJSONReq) Content() govtypes.Content {
	return &types.ClearAdminProposal{
		Title:       s.Title,
		Description: s.Description,
		Contract:    s.Contract,
	}
}

func (s ClearAdminJSONReq) GetProposer() string {
	return s.Proposer
}

func (s ClearAdminJSONReq) GetDeposit() sdk.Coins {
	return s.Deposit
}

func (s ClearAdminJSONReq) GetBaseReq() rest.BaseReq {
	return s.BaseReq
}

func ClearContractAdminProposalHandler(cliCtx client.Context) govrest.ProposalRESTHandler {
	return govrest.ProposalRESTHandler{
		SubRoute: "wasm_clear_admin",
		Handler: func(w http.ResponseWriter, r *http.Request) {
			var req ClearAdminJSONReq
			if !rest.ReadRESTReq(w, r, cliCtx.LegacyAmino, &req) {
				return
			}
			toStdTxResponse(cliCtx, w, req)
		},
	}
}

type PinCodeJSONReq struct {
	BaseReq rest.BaseReq `json:"base_req" yaml:"base_req"`

	Title       string `json:"title" yaml:"title"`
	Description string `json:"description" yaml:"description"`

	Proposer string    `json:"proposer" yaml:"proposer"`
	Deposit  sdk.Coins `json:"deposit" yaml:"deposit"`

	CodeIDs []uint64 `json:"code_ids" yaml:"code_ids"`
}

func (s PinCodeJSONReq) Content() govtypes.Content {
	return &types.PinCodesProposal{
		Title:       s.Title,
		Description: s.Description,
		CodeIDs:     s.CodeIDs,
	}
}

func (s PinCodeJSONReq) GetProposer() string {
	return s.Proposer
}

func (s PinCodeJSONReq) GetDeposit() sdk.Coins {
	return s.Deposit
}

func (s PinCodeJSONReq) GetBaseReq() rest.BaseReq {
	return s.BaseReq
}

func PinCodeProposalHandler(cliCtx client.Context) govrest.ProposalRESTHandler {
	return govrest.ProposalRESTHandler{
		SubRoute: "pin_code",
		Handler: func(w http.ResponseWriter, r *http.Request) {
			var req PinCodeJSONReq
			if !rest.ReadRESTReq(w, r, cliCtx.LegacyAmino, &req) {
				return
			}
			toStdTxResponse(cliCtx, w, req)
		},
	}
}

type UnpinCodeJSONReq struct {
	BaseReq rest.BaseReq `json:"base_req" yaml:"base_req"`

	Title       string `json:"title" yaml:"title"`
	Description string `json:"description" yaml:"description"`

	Proposer string    `json:"proposer" yaml:"proposer"`
	Deposit  sdk.Coins `json:"deposit" yaml:"deposit"`

	CodeIDs []uint64 `json:"code_ids" yaml:"code_ids"`
}

func (s UnpinCodeJSONReq) Content() govtypes.Content {
	return &types.UnpinCodesProposal{
		Title:       s.Title,
		Description: s.Description,
		CodeIDs:     s.CodeIDs,
	}
}

func (s UnpinCodeJSONReq) GetProposer() string {
	return s.Proposer
}

func (s UnpinCodeJSONReq) GetDeposit() sdk.Coins {
	return s.Deposit
}

func (s UnpinCodeJSONReq) GetBaseReq() rest.BaseReq {
	return s.BaseReq
}

func UnpinCodeProposalHandler(cliCtx client.Context) govrest.ProposalRESTHandler {
	return govrest.ProposalRESTHandler{
		SubRoute: "unpin_code",
		Handler: func(w http.ResponseWriter, r *http.Request) {
			var req UnpinCodeJSONReq
			if !rest.ReadRESTReq(w, r, cliCtx.LegacyAmino, &req) {
				return
			}
			toStdTxResponse(cliCtx, w, req)
		},
	}
}

type UpdateInstantiateConfigProposalJSONReq struct {
	BaseReq rest.BaseReq `json:"base_req" yaml:"base_req"`

	Title               string                     `json:"title" yaml:"title"`
	Description         string                     `json:"description" yaml:"description"`
	Proposer            string                     `json:"proposer" yaml:"proposer"`
	Deposit             sdk.Coins                  `json:"deposit" yaml:"deposit"`
	AccessConfigUpdates []types.AccessConfigUpdate `json:"access_config_updates" yaml:"access_config_updates"`
}

func (s UpdateInstantiateConfigProposalJSONReq) Content() govtypes.Content {
	return &types.UpdateInstantiateConfigProposal{
		Title:               s.Title,
		Description:         s.Description,
		AccessConfigUpdates: s.AccessConfigUpdates,
	}
}

func (s UpdateInstantiateConfigProposalJSONReq) GetProposer() string {
	return s.Proposer
}

func (s UpdateInstantiateConfigProposalJSONReq) GetDeposit() sdk.Coins {
	return s.Deposit
}

func (s UpdateInstantiateConfigProposalJSONReq) GetBaseReq() rest.BaseReq {
	return s.BaseReq
}

func UpdateInstantiateConfigProposalHandler(cliCtx client.Context) govrest.ProposalRESTHandler {
	return govrest.ProposalRESTHandler{
		SubRoute: "update_instantiate_config",
		Handler: func(w http.ResponseWriter, r *http.Request) {
			var req UpdateInstantiateConfigProposalJSONReq
			if !rest.ReadRESTReq(w, r, cliCtx.LegacyAmino, &req) {
				return
			}
			toStdTxResponse(cliCtx, w, req)
		},
	}
}

type wasmProposalData interface {
	Content() govtypes.Content
	GetProposer() string
	GetDeposit() sdk.Coins
	GetBaseReq() rest.BaseReq
}

func toStdTxResponse(cliCtx client.Context, w http.ResponseWriter, data wasmProposalData) {
	proposerAddr, err := sdk.AccAddressFromBech32(data.GetProposer())
	if err != nil {
		rest.WriteErrorResponse(w, http.StatusBadRequest, err.Error())
		return
	}
	msg, err := govtypes.NewMsgSubmitProposal(data.Content(), data.GetDeposit(), proposerAddr)
	if err != nil {
		rest.WriteErrorResponse(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := msg.ValidateBasic(); err != nil {
		rest.WriteErrorResponse(w, http.StatusBadRequest, err.Error())
		return
	}
	baseReq := data.GetBaseReq().Sanitize()
	if !baseReq.ValidateBasic(w) {
		return
	}
	tx.WriteGeneratedTxResponse(cliCtx, w, baseReq, msg)
}

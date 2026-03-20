package types

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"

	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
)

func GenesisFixture(mutators ...func(*GenesisState)) GenesisState {
	const (
		numCodes     = 2
		numContracts = 2
		numSequences = 2
		numMsg       = 3
	)

	fixture := GenesisState{
		Params:    DefaultParams(),
		Codes:     make([]Code, numCodes),
		Contracts: make([]Contract, numContracts),
		Sequences: make([]Sequence, numSequences),
	}
	for i := 0; i < numCodes; i++ {
		fixture.Codes[i] = CodeFixture()
	}
	for i := 0; i < numContracts; i++ {
		fixture.Contracts[i] = ContractFixture()
	}
	for i := 0; i < numSequences; i++ {
		fixture.Sequences[i] = Sequence{
			IDKey: randBytes(5),
			Value: uint64(i), // #nosec G115 -- constant iterations.
		}
	}
	fixture.GenMsgs = []GenesisState_GenMsgs{
		{Sum: &GenesisState_GenMsgs_StoreCode{StoreCode: MsgStoreCodeFixture()}},
		{Sum: &GenesisState_GenMsgs_InstantiateContract{InstantiateContract: MsgInstantiateContractFixture()}},
		{Sum: &GenesisState_GenMsgs_ExecuteContract{ExecuteContract: MsgExecuteContractFixture()}},
	}
	for _, m := range mutators {
		m(&fixture)
	}
	return fixture
}

func randBytes(n int) []byte {
	r := make([]byte, n)
	_, _ = rand.Read(r)
	return r
}

func CodeFixture(mutators ...func(*Code)) Code {
	wasmCode := randBytes(100)

	fixture := Code{
		CodeID:    1,
		CodeInfo:  CodeInfoFixture(WithSHA256CodeHash(wasmCode)),
		CodeBytes: wasmCode,
	}

	for _, m := range mutators {
		m(&fixture)
	}
	return fixture
}

func CodeInfoFixture(mutators ...func(*CodeInfo)) CodeInfo {
	wasmCode := bytes.Repeat([]byte{0x1}, 10)
	codeHash := sha256.Sum256(wasmCode)
	const anyAddress = "sei1l976cvcndrr6hnuyzn93azaxx8sc2xre5crtpz"
	fixture := CodeInfo{
		CodeHash:          codeHash[:],
		Creator:           anyAddress,
		InstantiateConfig: AllowEverybody,
	}
	for _, m := range mutators {
		m(&fixture)
	}
	return fixture
}

func ContractFixture(mutators ...func(*Contract)) Contract {
	const anyAddress = "sei1l976cvcndrr6hnuyzn93azaxx8sc2xre5crtpz"

	fixture := Contract{
		ContractAddress: anyAddress,
		ContractInfo:    ContractInfoFixture(OnlyGenesisFields),
		ContractState:   []Model{{Key: []byte("anyKey"), Value: []byte("anyValue")}},
	}

	for _, m := range mutators {
		m(&fixture)
	}
	return fixture
}

func OnlyGenesisFields(info *ContractInfo) {
	info.Created = nil
}

func ContractInfoFixture(mutators ...func(*ContractInfo)) ContractInfo {
	const anyAddress = "sei1l976cvcndrr6hnuyzn93azaxx8sc2xre5crtpz"

	fixture := ContractInfo{
		CodeID:  1,
		Creator: anyAddress,
		Label:   "any",
		Created: &AbsoluteTxPosition{BlockHeight: 1, TxIndex: 1},
	}

	for _, m := range mutators {
		m(&fixture)
	}
	return fixture
}

func WithSHA256CodeHash(wasmCode []byte) func(info *CodeInfo) {
	return func(info *CodeInfo) {
		codeHash := sha256.Sum256(wasmCode)
		info.CodeHash = codeHash[:]
	}
}

func MsgStoreCodeFixture(mutators ...func(*MsgStoreCode)) *MsgStoreCode {
	wasmIdent := []byte("\x00\x61\x73\x6D")
	const anyAddress = "sei1l976cvcndrr6hnuyzn93azaxx8sc2xre5crtpz"
	r := &MsgStoreCode{
		Sender:                anyAddress,
		WASMByteCode:          wasmIdent,
		InstantiatePermission: &AllowEverybody,
	}
	for _, m := range mutators {
		m(r)
	}
	return r
}

func MsgInstantiateContractFixture(mutators ...func(*MsgInstantiateContract)) *MsgInstantiateContract {
	const anyAddress = "sei1l976cvcndrr6hnuyzn93azaxx8sc2xre5crtpz"
	r := &MsgInstantiateContract{
		Sender: anyAddress,
		Admin:  anyAddress,
		CodeID: 1,
		Label:  "testing",
		Msg:    []byte(`{"foo":"bar"}`),
		Funds: sdk.Coins{{
			Denom:  "stake",
			Amount: sdk.NewInt(1),
		}},
	}
	for _, m := range mutators {
		m(r)
	}
	return r
}

func MsgExecuteContractFixture(mutators ...func(*MsgExecuteContract)) *MsgExecuteContract {
	const (
		anyAddress           = "sei1l976cvcndrr6hnuyzn93azaxx8sc2xre5crtpz"
		firstContractAddress = "sei1rs8v2232uv5nw8c88ruvyjy08mmxfx25pur3pl"
	)
	r := &MsgExecuteContract{
		Sender:   anyAddress,
		Contract: firstContractAddress,
		Msg:      []byte(`{"do":"something"}`),
		Funds: sdk.Coins{{
			Denom:  "stake",
			Amount: sdk.NewInt(1),
		}},
	}
	for _, m := range mutators {
		m(r)
	}
	return r
}

func StoreCodeProposalFixture(mutators ...func(*StoreCodeProposal)) *StoreCodeProposal {
	const anyAddress = "sei1rs8v2232uv5nw8c88ruvyjy08mmxfx25pur3pl"
	p := &StoreCodeProposal{
		Title:        "Foo",
		Description:  "Bar",
		RunAs:        anyAddress,
		WASMByteCode: []byte{0x0},
	}
	for _, m := range mutators {
		m(p)
	}
	return p
}

func InstantiateContractProposalFixture(mutators ...func(p *InstantiateContractProposal)) *InstantiateContractProposal {
	var (
		anyValidAddress sdk.AccAddress = bytes.Repeat([]byte{0x1}, ContractAddrLen)

		initMsg = struct {
			Verifier    sdk.AccAddress `json:"verifier"`
			Beneficiary sdk.AccAddress `json:"beneficiary"`
		}{
			Verifier:    anyValidAddress,
			Beneficiary: anyValidAddress,
		}
	)
	const anyAddress = "sei1rs8v2232uv5nw8c88ruvyjy08mmxfx25pur3pl"

	initMsgBz, err := json.Marshal(initMsg)
	if err != nil {
		panic(err)
	}
	p := &InstantiateContractProposal{
		Title:       "Foo",
		Description: "Bar",
		RunAs:       anyAddress,
		Admin:       anyAddress,
		CodeID:      1,
		Label:       "testing",
		Msg:         initMsgBz,
		Funds:       nil,
	}

	for _, m := range mutators {
		m(p)
	}
	return p
}

func MigrateContractProposalFixture(mutators ...func(p *MigrateContractProposal)) *MigrateContractProposal {
	var (
		anyValidAddress sdk.AccAddress = bytes.Repeat([]byte{0x1}, ContractAddrLen)

		migMsg = struct {
			Verifier sdk.AccAddress `json:"verifier"`
		}{Verifier: anyValidAddress}
	)

	migMsgBz, err := json.Marshal(migMsg)
	if err != nil {
		panic(err)
	}
	const (
		contractAddr = "sei1l976cvcndrr6hnuyzn93azaxx8sc2xre5crtpz"
		anyAddress   = "sei1rs8v2232uv5nw8c88ruvyjy08mmxfx25pur3pl"
	)
	p := &MigrateContractProposal{
		Title:       "Foo",
		Description: "Bar",
		Contract:    contractAddr,
		CodeID:      1,
		Msg:         migMsgBz,
	}

	for _, m := range mutators {
		m(p)
	}
	return p
}

func SudoContractProposalFixture(mutators ...func(p *SudoContractProposal)) *SudoContractProposal {
	const (
		contractAddr = "sei1l976cvcndrr6hnuyzn93azaxx8sc2xre5crtpz"
	)

	p := &SudoContractProposal{
		Title:       "Foo",
		Description: "Bar",
		Contract:    contractAddr,
		Msg:         []byte(`{"do":"something"}`),
	}

	for _, m := range mutators {
		m(p)
	}
	return p
}

func ExecuteContractProposalFixture(mutators ...func(p *ExecuteContractProposal)) *ExecuteContractProposal {
	const (
		contractAddr = "sei1l976cvcndrr6hnuyzn93azaxx8sc2xre5crtpz"
		anyAddress   = "sei1rs8v2232uv5nw8c88ruvyjy08mmxfx25pur3pl"
	)

	p := &ExecuteContractProposal{
		Title:       "Foo",
		Description: "Bar",
		Contract:    contractAddr,
		RunAs:       anyAddress,
		Msg:         []byte(`{"do":"something"}`),
		Funds: sdk.Coins{{
			Denom:  "stake",
			Amount: sdk.NewInt(1),
		}},
	}

	for _, m := range mutators {
		m(p)
	}
	return p
}

func UpdateAdminProposalFixture(mutators ...func(p *UpdateAdminProposal)) *UpdateAdminProposal {
	const (
		contractAddr = "sei1rs8v2232uv5nw8c88ruvyjy08mmxfx25pur3pl"
		anyAddress   = "sei1l976cvcndrr6hnuyzn93azaxx8sc2xre5crtpz"
	)

	p := &UpdateAdminProposal{
		Title:       "Foo",
		Description: "Bar",
		NewAdmin:    anyAddress,
		Contract:    contractAddr,
	}
	for _, m := range mutators {
		m(p)
	}
	return p
}

func ClearAdminProposalFixture(mutators ...func(p *ClearAdminProposal)) *ClearAdminProposal {
	const contractAddr = "sei1l976cvcndrr6hnuyzn93azaxx8sc2xre5crtpz"
	p := &ClearAdminProposal{
		Title:       "Foo",
		Description: "Bar",
		Contract:    contractAddr,
	}
	for _, m := range mutators {
		m(p)
	}
	return p
}

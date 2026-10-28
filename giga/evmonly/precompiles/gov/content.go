package gov

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"

	"github.com/ethereum/go-ethereum/accounts/abi"

	"github.com/sei-protocol/sei-chain/giga/evmonly/precompiles"
	chainparams "github.com/sei-protocol/sei-chain/giga/evmonly/precompiles/params"
)

// Proposal types, as the submitted JSON names them.
const (
	TypeText                  = "Text"
	TypeSoftwareUpgrade       = "SoftwareUpgrade"
	TypeCancelSoftwareUpgrade = "CancelSoftwareUpgrade"
	TypeParameterChange       = "ParameterChange"
)

// Size limits on submitted content. The title and description limits are x/gov's.
const (
	maxTitleLength       = 140
	maxDescriptionLength = 10_000
	maxContentLength     = 16 << 10
)

// Content types, as GovStorage.Proposal.contentType numbers them. Each names
// the GovStorage struct its content is the ABI encoding of.
const (
	contentText                  uint8 = 0 // TextContent
	contentSoftwareUpgrade       uint8 = 1 // SoftwareUpgradeContent
	contentCancelSoftwareUpgrade uint8 = 2 // CancelSoftwareUpgradeContent
	contentParameterChange       uint8 = 3 // ParamChangeContent
)

var (
	errUnsupportedType = errors.New("unsupported proposal type")
	errInvalidPlan     = errors.New("invalid upgrade plan")
	errContentTooLong  = errors.New("proposal content is too long")
	errUnknownField    = errors.New("unknown field in proposal JSON")
	errDuplicateField  = errors.New("duplicate field in proposal JSON")
	errInvalidContent  = errors.New("invalid stored proposal content")
)

// submittedProposal is the submitted proposal JSON.
type submittedProposal struct {
	Title       string        `json:"title"`
	Description string        `json:"description"`
	Type        string        `json:"type"`
	IsExpedited bool          `json:"is_expedited,omitempty"`
	Plan        *proposalPlan `json:"plan,omitempty"`

	Changes []chainparams.LegacyChange `json:"changes,omitempty"`
}

// proposalPlan is a SoftwareUpgrade proposal's plan.
type proposalPlan struct {
	Name   string `json:"name"`
	Height int64  `json:"height"`
	Info   string `json:"info,omitempty"`
}

// govContent is a proposal's content, one of the GovStorage content
// structs.
type govContent interface {
	contentType() uint8
	// validate checks the content against the current state at submission.
	validate(s store) error
	// execute carries out passed proposal id.
	execute(block precompiles.BlockContext, s store, id uint64) error
	// v2JSON is V2's proto JSON of the matching Content, as its proposal query
	// returns it.
	v2JSON() []byte
}

// The GovStorage content structs. Field order matches each struct's.
type (
	textContent struct {
		Title       string
		Description string
		IsExpedited bool
	}

	upgradePlan struct {
		Name   string
		Height int64
		Info   string
	}

	softwareUpgradeContent struct {
		Title       string
		Description string
		Plan        upgradePlan
	}

	cancelSoftwareUpgradeContent struct {
		Title       string
		Description string
	}

	paramChangeContent struct {
		Title       string
		Description string
		Changes     []chainparams.Change
		IsExpedited bool
	}
)

var (
	stringArg = abi.ArgumentMarshaling{Type: "string"}
	planArg   = abi.ArgumentMarshaling{Name: "plan", Type: "tuple", InternalType: "struct GovStorage.UpgradePlan", Components: []abi.ArgumentMarshaling{
		named("name", stringArg), {Name: "height", Type: "int64"}, named("info", stringArg),
	}}
)

// contentCodecs encode and decode each content type's struct.
var contentCodecs = map[uint8]contentCodec{
	contentText: newCodec[textContent]("TextContent",
		named("title", stringArg), named("description", stringArg), abi.ArgumentMarshaling{Name: "isExpedited", Type: "bool"}),
	contentSoftwareUpgrade: newCodec[softwareUpgradeContent]("SoftwareUpgradeContent",
		named("title", stringArg), named("description", stringArg), planArg),
	contentCancelSoftwareUpgrade: newCodec[cancelSoftwareUpgradeContent]("CancelSoftwareUpgradeContent",
		named("title", stringArg), named("description", stringArg)),
	contentParameterChange: newCodec[paramChangeContent]("ParamChangeContent",
		named("title", stringArg), named("description", stringArg), abi.ArgumentMarshaling{
			Name: "changes", Type: "tuple[]", InternalType: "struct GovStorage.ParamChange[]", Components: []abi.ArgumentMarshaling{
				named("subspace", stringArg), named("key", stringArg), {Name: "value", Type: "bytes"},
			},
		}, abi.ArgumentMarshaling{Name: "isExpedited", Type: "bool"}),
}

// proposalTypes build the content of each proposal type gov accepts from its
// submitted JSON, rejecting a field the type does not use.
var proposalTypes = map[string]func(submittedProposal) (govContent, error){
	TypeText: func(p submittedProposal) (govContent, error) {
		if err := onlyFields(p, false, false); err != nil {
			return nil, err
		}
		return textContent{Title: p.Title, Description: p.Description, IsExpedited: p.IsExpedited}, nil
	},
	TypeSoftwareUpgrade: func(p submittedProposal) (govContent, error) {
		if err := onlyFields(p, true, false); err != nil {
			return nil, err
		}
		if p.Plan == nil || p.Plan.Name == "" || p.Plan.Height <= 0 {
			return nil, fmt.Errorf("%w: name and a positive height are required", errInvalidPlan)
		}
		plan := upgradePlan{Name: p.Plan.Name, Height: p.Plan.Height, Info: p.Plan.Info}
		return softwareUpgradeContent{Title: p.Title, Description: p.Description, Plan: plan}, nil
	},
	TypeCancelSoftwareUpgrade: func(p submittedProposal) (govContent, error) {
		if err := onlyFields(p, false, false); err != nil {
			return nil, err
		}
		return cancelSoftwareUpgradeContent{Title: p.Title, Description: p.Description}, nil
	},
	TypeParameterChange: func(p submittedProposal) (govContent, error) {
		if err := onlyFields(p, false, true); err != nil {
			return nil, err
		}
		changes, err := chainparams.FromLegacy(p.Changes)
		if err != nil {
			return nil, err
		}
		return paramChangeContent{Title: p.Title, Description: p.Description, Changes: changes, IsExpedited: p.IsExpedited}, nil
	},
}

// onlyFields rejects a plan or changes on a proposal type that does not use it.
func onlyFields(p submittedProposal, plan, changes bool) error {
	if p.Plan != nil && !plan {
		return fmt.Errorf("%w: plan", errUnknownField)
	}
	if p.Changes != nil && !changes {
		return fmt.Errorf("%w: changes", errUnknownField)
	}
	return nil
}

// contentCodec is the ABI encoding of one content struct.
type contentCodec struct {
	args   abi.Arguments
	decode func([]byte) (govContent, error)
}

func newCodec[T govContent](name string, fields ...abi.ArgumentMarshaling) contentCodec {
	t, err := abi.NewType("tuple", "struct GovStorage."+name, fields)
	if err != nil {
		panic(err)
	}
	args := abi.Arguments{{Type: t}}
	return contentCodec{args: args, decode: func(bz []byte) (govContent, error) {
		values, err := args.Unpack(bz)
		if err != nil {
			return nil, fmt.Errorf("%w: %w", errInvalidContent, err)
		}
		return *abi.ConvertType(values[0], new(T)).(*T), nil
	}}
}

func named(name string, arg abi.ArgumentMarshaling) abi.ArgumentMarshaling {
	arg.Name = name
	return arg
}

// encodeContent returns c's type and ABI encoding.
func encodeContent(c govContent) (uint8, []byte) {
	bz, err := contentCodecs[c.contentType()].args.Pack(c)
	if err != nil {
		panic(fmt.Sprintf("encoding %T: %v", c, err)) // every content struct matches its codec
	}
	return c.contentType(), bz
}

// decodeContent decodes content of type t.
func decodeContent(t uint8, bz []byte) (govContent, error) {
	codec, ok := contentCodecs[t]
	if !ok {
		return nil, fmt.Errorf("%w: type %d", errInvalidContent, t)
	}
	return codec.decode(bz)
}

// parseProposal parses submitted proposal JSON into its content.
func parseProposal(content string) (govContent, submittedProposal, error) {
	var p submittedProposal
	if len(content) > maxContentLength {
		return nil, p, fmt.Errorf("%w: %d bytes, max %d", errContentTooLong, len(content), maxContentLength)
	}
	if err := decodeStrict([]byte(content), &p); err != nil {
		return nil, p, fmt.Errorf("failed to parse proposal JSON: %w", err)
	}
	if p.Type == "" {
		return nil, p, errors.New("proposal type is required")
	}
	if err := checkText(p.Title, p.Description); err != nil {
		return nil, p, err
	}
	build, ok := proposalTypes[p.Type]
	if !ok {
		return nil, p, fmt.Errorf("%w: %s", errUnsupportedType, p.Type)
	}
	c, err := build(p)
	return c, p, err
}

// checkText checks a proposal's title and description.
func checkText(title, description string) error {
	if title == "" || description == "" {
		return errors.New("proposal title and description are required")
	}
	if len(title) > maxTitleLength || len(description) > maxDescriptionLength {
		return fmt.Errorf("%w: title max %d, description max %d", errContentTooLong, maxTitleLength, maxDescriptionLength)
	}
	return nil
}

// decodeStrict decodes JSON into v, rejecting at every depth a field v does
// not declare, a field named in different case, and a field given twice.
func decodeStrict(data []byte, v any) error {
	if err := checkFields(data, reflect.TypeOf(v)); err != nil {
		return err
	}
	return json.Unmarshal(data, v)
}

// checkFields checks that every object in raw names only fields of t, each
// once and in the case t declares.
func checkFields(raw json.RawMessage, t reflect.Type) error {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	switch t.Kind() {
	case reflect.Struct:
		members, err := objectMembers(raw)
		if err != nil || members == nil {
			return err
		}
		for _, m := range members {
			f, ok := fieldNamed(t, m.name)
			if !ok {
				return fmt.Errorf("%w: %q", errUnknownField, m.name)
			}
			if err := checkFields(m.value, f.Type); err != nil {
				return err
			}
		}
	case reflect.Slice:
		var items []json.RawMessage
		if json.Unmarshal(raw, &items) != nil {
			return nil // not an array; json.Unmarshal reports it
		}
		for _, item := range items {
			if err := checkFields(item, t.Elem()); err != nil {
				return err
			}
		}
	}
	return nil
}

type jsonMember struct {
	name  string
	value json.RawMessage
}

// objectMembers returns raw's members in order, or nil when raw is not an
// object, and rejects a name given twice.
func objectMembers(raw json.RawMessage) ([]jsonMember, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	if t, err := dec.Token(); err != nil || t != json.Delim('{') {
		return nil, nil // not an object; json.Unmarshal reports it
	}
	members := []jsonMember{}
	for dec.More() {
		t, err := dec.Token()
		if err != nil {
			return nil, err
		}
		name := t.(string) // object keys are always strings
		for _, m := range members {
			if m.name == name {
				return nil, fmt.Errorf("%w: %q", errDuplicateField, name)
			}
		}
		var value json.RawMessage
		if err := dec.Decode(&value); err != nil {
			return nil, err
		}
		members = append(members, jsonMember{name: name, value: value})
	}
	return members, nil
}

// fieldNamed returns the field of struct t whose JSON name is exactly name.
func fieldNamed(t reflect.Type, name string) (reflect.StructField, bool) {
	for i := range t.NumField() {
		f := t.Field(i)
		tag, _, _ := strings.Cut(f.Tag.Get("json"), ",")
		if tag == "" {
			tag = f.Name
		}
		if f.IsExported() && tag != "-" && tag == name {
			return f, true
		}
	}
	return reflect.StructField{}, false
}

// executeProposal carries out passed proposal id.
func executeProposal(block precompiles.BlockContext, s store, id uint64) error {
	c, err := decodeContent(s.proposalContent(id))
	if err != nil {
		return err
	}
	return c.execute(block, s, id)
}

// executionCost is the EndBlock budget executing proposal id costs: one unit
// per param change.
func executionCost(s store, id uint64) uint64 {
	c, err := decodeContent(s.proposalContent(id))
	if err != nil {
		return 0
	}
	if pc, ok := c.(paramChangeContent); ok {
		return uint64(len(pc.Changes))
	}
	return 0
}

// paramChanges converts the ABI-decoded changes argument.
func paramChanges(arg interface{}) ([]chainparams.Change, error) {
	raw, ok := arg.([]struct {
		Subspace string `json:"subspace"`
		Key      string `json:"key"`
		Value    []byte `json:"value"`
	})
	if !ok {
		return nil, fmt.Errorf("unexpected param changes type %T", arg)
	}
	out := make([]chainparams.Change, len(raw))
	for i, c := range raw {
		out[i] = chainparams.Change(c)
	}
	return out, nil
}

func (textContent) contentType() uint8 { return contentText }

func (textContent) validate(store) error { return nil }

func (textContent) execute(precompiles.BlockContext, store, uint64) error { return nil }

func (c textContent) v2JSON() []byte {
	return mustJSON(struct {
		Type        string `json:"@type"`
		Title       string `json:"title"`
		Description string `json:"description"`
		IsExpedited bool   `json:"is_expedited"`
	}{"/cosmos.gov.v1beta1.TextProposal", c.Title, c.Description, c.IsExpedited})
}

func (softwareUpgradeContent) contentType() uint8 { return contentSoftwareUpgrade }

func (softwareUpgradeContent) validate(store) error { return nil }

// execute replaces the plan with c's, as x/upgrade's ScheduleUpgrade.
func (c softwareUpgradeContent) execute(block precompiles.BlockContext, s store, id uint64) error {
	if c.Plan.Height <= 0 || uint64(c.Plan.Height) <= block.Number {
		return fmt.Errorf("%w: height %d is not after block %d", errInvalidPlan, c.Plan.Height, block.Number)
	}
	if s.appliedPlanHeight(c.Plan.Name) != 0 {
		return fmt.Errorf("%w: %s was already applied", errInvalidPlan, c.Plan.Name)
	}
	s.setPlan(Plan{Name: c.Plan.Name, Height: c.Plan.Height, Info: c.Plan.Info, ProposalID: id})
	return nil
}

func (c softwareUpgradeContent) v2JSON() []byte {
	type plan struct {
		Name   string `json:"name"`
		Time   string `json:"time"`
		Height int64  `json:"height,string"`
		Info   string `json:"info"`
	}
	return mustJSON(struct {
		Type        string `json:"@type"`
		Title       string `json:"title"`
		Description string `json:"description"`
		Plan        plan   `json:"plan"`
	}{"/cosmos.upgrade.v1beta1.SoftwareUpgradeProposal", c.Title, c.Description, plan{c.Plan.Name, "0001-01-01T00:00:00Z", c.Plan.Height, c.Plan.Info}})
}

func (cancelSoftwareUpgradeContent) contentType() uint8 { return contentCancelSoftwareUpgrade }

func (cancelSoftwareUpgradeContent) validate(store) error { return nil }

func (cancelSoftwareUpgradeContent) execute(_ precompiles.BlockContext, s store, _ uint64) error {
	s.setPlan(Plan{})
	return nil
}

func (c cancelSoftwareUpgradeContent) v2JSON() []byte {
	return mustJSON(struct {
		Type        string `json:"@type"`
		Title       string `json:"title"`
		Description string `json:"description"`
	}{"/cosmos.upgrade.v1beta1.CancelSoftwareUpgradeProposal", c.Title, c.Description})
}

func (paramChangeContent) contentType() uint8 { return contentParameterChange }

func (c paramChangeContent) validate(s store) error {
	return chainparams.Check(s.State, c.Changes)
}

func (c paramChangeContent) execute(_ precompiles.BlockContext, s store, _ uint64) error {
	return chainparams.Apply(s.State, c.Changes)
}

func (c paramChangeContent) v2JSON() []byte {
	return mustJSON(struct {
		Type        string                 `json:"@type"`
		Title       string                 `json:"title"`
		Description string                 `json:"description"`
		Changes     []chainparams.V2Change `json:"changes"`
		IsExpedited bool                   `json:"is_expedited"`
	}{"/cosmos.params.v1beta1.ParameterChangeProposal", c.Title, c.Description, chainparams.ToV2(c.Changes), c.IsExpedited})
}

// mustJSON marshals v, which holds only strings, numbers, booleans and slices
// of them.
func mustJSON(v any) []byte {
	bz, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return bz
}

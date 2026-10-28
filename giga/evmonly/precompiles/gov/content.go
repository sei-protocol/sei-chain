package gov

import (
	"errors"
	"fmt"

	"github.com/ethereum/go-ethereum/accounts/abi"

	"github.com/sei-protocol/sei-chain/giga/evmonly/precompiles"
	chainparams "github.com/sei-protocol/sei-chain/giga/evmonly/precompiles/params"
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
	errMissingText    = errors.New("proposal title and description are required")
	errInvalidPlan    = errors.New("invalid upgrade plan")
	errContentTooLong = errors.New("proposal content is too long")
	errInvalidContent = errors.New("invalid stored proposal content")
)

// govContent is a proposal's content, one of the GovStorage content
// structs.
type govContent interface {
	contentType() uint8
	// validate checks the content against the current state at submission.
	validate(s store) error
	// execute carries out passed proposal id, writing nothing when it fails.
	execute(block precompiles.BlockContext, s store, id uint64) error
}

// The GovStorage content structs. Field order matches each struct's.
type (
	textContent struct {
		Title       string
		Description string
		IsExpedited bool
	}

	softwareUpgradeContent struct {
		Title       string
		Description string
		Plan        UpgradePlan
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

// checkText checks a proposal's title and description against x/gov's limits.
func checkText(title, description string) error {
	if title == "" || description == "" {
		return errMissingText
	}
	if len(title) > maxTitleLength || len(description) > maxDescriptionLength {
		return fmt.Errorf("%w: title max %d, description max %d", errContentTooLong, maxTitleLength, maxDescriptionLength)
	}
	return nil
}

// executionCost is the EndBlock budget executing c costs: one unit per param
// change.
func executionCost(c govContent) uint64 {
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

func (c textContent) validate(store) error { return checkText(c.Title, c.Description) }

func (textContent) execute(precompiles.BlockContext, store, uint64) error { return nil }

func (softwareUpgradeContent) contentType() uint8 { return contentSoftwareUpgrade }

func (c softwareUpgradeContent) validate(store) error {
	if c.Plan.Name == "" || c.Plan.Height <= 0 {
		return fmt.Errorf("%w: name and a positive height are required", errInvalidPlan)
	}
	return checkText(c.Title, c.Description)
}

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

func (cancelSoftwareUpgradeContent) contentType() uint8 { return contentCancelSoftwareUpgrade }

func (c cancelSoftwareUpgradeContent) validate(store) error { return checkText(c.Title, c.Description) }

func (cancelSoftwareUpgradeContent) execute(_ precompiles.BlockContext, s store, _ uint64) error {
	s.setPlan(Plan{})
	return nil
}

func (paramChangeContent) contentType() uint8 { return contentParameterChange }

func (c paramChangeContent) validate(s store) error {
	if err := checkText(c.Title, c.Description); err != nil {
		return err
	}
	return chainparams.Check(s.State, c.Changes)
}

func (c paramChangeContent) execute(_ precompiles.BlockContext, s store, _ uint64) error {
	return chainparams.Apply(s.State, c.Changes)
}

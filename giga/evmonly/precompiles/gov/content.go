package gov

import (
	"errors"
	"fmt"

	"github.com/ethereum/go-ethereum/accounts/abi"

	"github.com/sei-protocol/sei-chain/giga/evmonly/precompiles"
	chainparams "github.com/sei-protocol/sei-chain/giga/evmonly/precompiles/params"
	"github.com/sei-protocol/sei-chain/giga/evmonly/precompiles/upgrade"
)

// Size limits on submitted content. The title and description limits are x/gov's.
const (
	maxTitleLength       = 140
	maxDescriptionLength = 10_000
	maxContentLength     = 16 << 10
)

// Content types, as GovStorage.Proposal.contentType numbers them. Each names
// the Gov.sol struct its content is the ABI encoding of.
const (
	contentText                  uint8 = 0 // TextContent
	contentSoftwareUpgrade       uint8 = 1 // SoftwareUpgradeContent
	contentCancelSoftwareUpgrade uint8 = 2 // CancelSoftwareUpgradeContent
	contentParameterChange       uint8 = 3 // ParamChangeContent
)

var (
	errMissingText    = errors.New("proposal title and description are required")
	errContentTooLong = errors.New("proposal content is too long")
	errInvalidContent = errors.New("invalid stored proposal content")
)

// govContent is a proposal's content, one of the GovStorage content
// structs.
type govContent interface {
	contentType() uint8
	// validate checks the content against the current state at submission.
	validate(block precompiles.BlockContext, s store) error
	// execute carries out passed proposal id, writing nothing when it fails. Its
	// work is at most one unit per word of the encoded content, which settleCost
	// charges. A software upgrade or cancel also clears the scheduled plan it
	// replaces; settleCost does not charge that work, which upgrade.MaxNameLength
	// and upgrade.MaxInfoLength bound.
	execute(block precompiles.BlockContext, s store, id uint64) error
}

// The Gov.sol content structs. Field order matches each struct's.
type (
	textContent struct {
		Title       string
		Description string
		IsExpedited bool
	}

	softwareUpgradeContent struct {
		Title       string
		Description string
		Plan        upgrade.Plan
		IsExpedited bool
	}

	cancelSoftwareUpgradeContent struct {
		Title       string
		Description string
		Name        string
		IsExpedited bool
	}

	paramChangeContent struct {
		Title       string
		Description string
		Changes     []chainparams.Change
		IsExpedited bool
	}
)

var stringArg = abi.ArgumentMarshaling{Type: "string"}

// contentCodecs encode and decode each content type's struct.
var contentCodecs = map[uint8]contentCodec{
	contentText: newCodec[textContent]("TextContent",
		named("title", stringArg), named("description", stringArg), abi.ArgumentMarshaling{Name: "isExpedited", Type: "bool"}),
	contentSoftwareUpgrade: newCodec[softwareUpgradeContent]("SoftwareUpgradeContent",
		named("title", stringArg), named("description", stringArg), abi.ArgumentMarshaling{
			Name: "plan", Type: "tuple", InternalType: "struct UpgradePlan", Components: []abi.ArgumentMarshaling{
				named("name", stringArg), {Name: "height", Type: "uint64"}, named("info", stringArg),
			},
		}, abi.ArgumentMarshaling{Name: "isExpedited", Type: "bool"}),
	contentCancelSoftwareUpgrade: newCodec[cancelSoftwareUpgradeContent]("CancelSoftwareUpgradeContent",
		named("title", stringArg), named("description", stringArg), named("name", stringArg),
		abi.ArgumentMarshaling{Name: "isExpedited", Type: "bool"}),
	contentParameterChange: newCodec[paramChangeContent]("ParamChangeContent",
		named("title", stringArg), named("description", stringArg), abi.ArgumentMarshaling{
			Name: "changes", Type: "tuple[]", InternalType: "struct ParamChange[]", Components: []abi.ArgumentMarshaling{
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
	t, err := abi.NewType("tuple", "struct "+name, fields)
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

// executeProposal carries out passed proposal id. Stored content that does not
// decode is reported as precompiles.ErrDegraded.
func executeProposal(block precompiles.BlockContext, s store, id uint64) error {
	t, bz, err := s.proposalContent(id)
	if err != nil {
		return fmt.Errorf("%w: %w", precompiles.ErrDegraded, err)
	}
	c, err := decodeContent(t, bz)
	if err != nil {
		return fmt.Errorf("%w: %w", precompiles.ErrDegraded, err)
	}
	return c.execute(block, s, id)
}

func (textContent) contentType() uint8 { return contentText }

func (textContent) validate(precompiles.BlockContext, store) error { return nil }

func (textContent) execute(precompiles.BlockContext, store, uint64) error { return nil }

func (softwareUpgradeContent) contentType() uint8 { return contentSoftwareUpgrade }

func (c softwareUpgradeContent) validate(block precompiles.BlockContext, s store) error {
	return upgrade.Check(s.State, block.Number, c.Plan)
}

func (c softwareUpgradeContent) execute(block precompiles.BlockContext, s store, _ uint64) error {
	return upgrade.Schedule(s.State, block.Number, c.Plan)
}

func (cancelSoftwareUpgradeContent) contentType() uint8 { return contentCancelSoftwareUpgrade }

func (c cancelSoftwareUpgradeContent) validate(_ precompiles.BlockContext, s store) error {
	return upgrade.CheckCancel(s.State, c.Name)
}

func (c cancelSoftwareUpgradeContent) execute(_ precompiles.BlockContext, s store, _ uint64) error {
	return upgrade.Cancel(s.State, c.Name)
}

func (paramChangeContent) contentType() uint8 { return contentParameterChange }

func (c paramChangeContent) validate(_ precompiles.BlockContext, s store) error {
	return chainparams.Check(s.State, c.Changes)
}

func (c paramChangeContent) execute(_ precompiles.BlockContext, s store, _ uint64) error {
	return chainparams.Apply(s.State, c.Changes)
}

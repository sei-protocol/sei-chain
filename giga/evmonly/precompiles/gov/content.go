package gov

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/sei-protocol/sei-chain/giga/evmonly/precompiles"
)

// Proposal types, as the submitted JSON names them.
const (
	TypeText                  = "Text"
	TypeSoftwareUpgrade       = "SoftwareUpgrade"
	TypeCancelSoftwareUpgrade = "CancelSoftwareUpgrade"
)

// Size limits on submitted content. The title and description limits are x/gov's.
const (
	maxTitleLength       = 140
	maxDescriptionLength = 10_000
	maxContentLength     = 16 << 10
)

var (
	errUnsupportedType = errors.New("unsupported proposal type")
	errInvalidPlan     = errors.New("invalid upgrade plan")
	errContentTooLong  = errors.New("proposal content is too long")
)

// submittedProposal is the part of the submitted proposal JSON gov reads.
type submittedProposal struct {
	Title       string        `json:"title"`
	Description string        `json:"description"`
	Type        string        `json:"type"`
	IsExpedited bool          `json:"is_expedited,omitempty"`
	Plan        *proposalPlan `json:"plan,omitempty"`
}

// proposalPlan is a SoftwareUpgrade proposal's plan.
type proposalPlan struct {
	Name   string `json:"name"`
	Height int64  `json:"height"`
	Info   string `json:"info,omitempty"`
}

// proposalType checks a proposal of its type at submission and carries it out
// when the proposal passes. A nil step does nothing.
type proposalType struct {
	validate func(submittedProposal) error
	execute  func(precompiles.BlockContext, store, uint64, submittedProposal) error
}

// proposalTypes are the proposal types gov accepts.
//
// TODO(giga params): ParameterChange joins this table when the params
// precompile migrates to evmonly, executing as a write to its storage.
var proposalTypes = map[string]proposalType{
	TypeText:                  {},
	TypeSoftwareUpgrade:       {validate: validateUpgrade, execute: scheduleUpgrade},
	TypeCancelSoftwareUpgrade: {execute: cancelUpgrade},
}

// parseProposal parses and validates submitted proposal content.
func parseProposal(content string) (submittedProposal, error) {
	var p submittedProposal
	if len(content) > maxContentLength {
		return p, fmt.Errorf("%w: %d bytes, max %d", errContentTooLong, len(content), maxContentLength)
	}
	if err := json.Unmarshal([]byte(content), &p); err != nil {
		return p, fmt.Errorf("failed to parse proposal JSON: %w", err)
	}
	if p.Title == "" || p.Description == "" || p.Type == "" {
		return p, errors.New("proposal title, description and type are required")
	}
	if len(p.Title) > maxTitleLength || len(p.Description) > maxDescriptionLength {
		return p, fmt.Errorf("%w: title max %d, description max %d", errContentTooLong, maxTitleLength, maxDescriptionLength)
	}
	t, ok := proposalTypes[p.Type]
	if !ok {
		return p, fmt.Errorf("%w: %s", errUnsupportedType, p.Type)
	}
	if t.validate != nil {
		if err := t.validate(p); err != nil {
			return p, err
		}
	}
	return p, nil
}

// executeProposal carries out passed proposal id.
func executeProposal(block precompiles.BlockContext, s store, id uint64) error {
	p, err := parseProposal(s.proposalContent(id))
	if err != nil {
		return err
	}
	if execute := proposalTypes[p.Type].execute; execute != nil {
		return execute(block, s, id, p)
	}
	return nil
}

func validateUpgrade(p submittedProposal) error {
	if p.Plan == nil || p.Plan.Name == "" || p.Plan.Height <= 0 {
		return fmt.Errorf("%w: name and a positive height are required", errInvalidPlan)
	}
	return nil
}

// scheduleUpgrade replaces the plan with p's, as x/upgrade's ScheduleUpgrade.
func scheduleUpgrade(block precompiles.BlockContext, s store, id uint64, p submittedProposal) error {
	if uint64(p.Plan.Height) <= block.Number { //nolint:gosec // validateUpgrade made it positive
		return fmt.Errorf("%w: height %d is not after block %d", errInvalidPlan, p.Plan.Height, block.Number)
	}
	if s.appliedPlanHeight(p.Plan.Name) != 0 {
		return fmt.Errorf("%w: %s was already applied", errInvalidPlan, p.Plan.Name)
	}
	s.setPlan(Plan{Name: p.Plan.Name, Height: p.Plan.Height, Info: p.Plan.Info, ProposalID: id})
	return nil
}

func cancelUpgrade(_ precompiles.BlockContext, s store, _ uint64, _ submittedProposal) error {
	s.setPlan(Plan{})
	return nil
}

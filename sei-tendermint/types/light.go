package types

import (
	"bytes"
	"errors"
	"fmt"
	"time"

	tbytes "github.com/sei-protocol/sei-chain/sei-tendermint/libs/bytes"
	tmproto "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/types"
)

// LightClientInfo describes the status of the light client.
type LightClientInfo struct {
	PrimaryID         string          `json:"primaryID"`
	WitnessesID       []string        `json:"witnessesID"`
	NumPeers          int             `json:"number_of_peers,string"`
	LastTrustedHeight int64           `json:"last_trusted_height,string"`
	LastTrustedHash   tbytes.HexBytes `json:"last_trusted_hash"`
	LatestBlockTime   time.Time       `json:"latest_block_time"`
	TrustingPeriod    string          `json:"trusting_period"`
	// TrustedBlockExpired is true if LatestBlockTime + TrustingPeriod is before
	// the time /status was called.
	TrustedBlockExpired bool `json:"trusted_block_expired"`
}

// LightBlock pairs a SignedHeader with its ValidatorSet and forms the basis of
// the light client.
type LightBlock struct {
	*SignedHeader `json:"signed_header"`
	ValidatorSet  *ValidatorSet `json:"validator_set"`
}

// ValidateBasic checks that the LightBlock is internally consistent. It does
// not verify any cryptographic signatures.
func (lb LightBlock) ValidateBasic(chainID string) error {
	switch {
	case lb.SignedHeader == nil:
		return errors.New("missing signed header")
	case lb.ValidatorSet == nil:
		return errors.New("missing validator set")
	}

	if err := lb.SignedHeader.ValidateBasic(chainID); err != nil {
		return fmt.Errorf("invalid signed header: %w", err)
	}
	if err := lb.ValidatorSet.ValidateBasic(); err != nil {
		return fmt.Errorf("invalid validator set: %w", err)
	}

	// The validator set must match the hash committed in the header.
	if valSetHash := lb.ValidatorSet.Hash(); !bytes.Equal(lb.ValidatorsHash, valSetHash) {
		return fmt.Errorf("expected validator hash of header to match validator set hash (%X != %X)",
			lb.ValidatorsHash, valSetHash)
	}

	return nil
}

// String returns a string representation of the LightBlock.
func (lb LightBlock) String() string {
	return lb.StringIndented("")
}

// StringIndented returns an indented string representation of the LightBlock,
// showing its SignedHeader and ValidatorSet.
func (lb LightBlock) StringIndented(indent string) string {
	return fmt.Sprintf(`LightBlock{
%s  %v
%s  %v
%s}`,
		indent, lb.SignedHeader.StringIndented(indent+"  "),
		indent, lb.ValidatorSet.StringIndented(indent+"  "),
		indent)
}

// ToProto converts the LightBlock to its protobuf representation.
func (lb *LightBlock) ToProto() (*tmproto.LightBlock, error) {
	if lb == nil {
		return nil, nil
	}

	var lbp tmproto.LightBlock
	if lb.SignedHeader != nil {
		lbp.SignedHeader = lb.SignedHeader.ToProto()
	}
	if lb.ValidatorSet != nil {
		vs, err := lb.ValidatorSet.ToProto()
		if err != nil {
			return nil, err
		}
		lbp.ValidatorSet = vs
	}

	return &lbp, nil
}

// LightBlockFromProto converts a protobuf LightBlock back into a LightBlock.
// It returns an error if the signed header or validator set is missing or
// invalid.
func LightBlockFromProto(pb *tmproto.LightBlock) (*LightBlock, error) {
	switch {
	case pb == nil:
		return nil, errors.New("nil light block")
	case pb.SignedHeader == nil:
		return nil, errors.New("nil signed header")
	case pb.ValidatorSet == nil:
		return nil, errors.New("nil validator set")
	}

	sh, err := SignedHeaderFromProto(pb.SignedHeader)
	if err != nil {
		return nil, err
	}

	vals, err := ValidatorSetFromProto(pb.ValidatorSet)
	if err != nil {
		return nil, err
	}

	return &LightBlock{SignedHeader: sh, ValidatorSet: vals}, nil
}

//-----------------------------------------------------------------------------

// SignedHeader is a Header along with the Commit that proves it.
type SignedHeader struct {
	*Header `json:"header"`
	Commit  *Commit `json:"commit"`
}

// ValidateBasic checks that the header and commit are internally consistent
// and belong to the given chain. It does not verify cryptographic signatures;
// use a Verifier to establish that the commit actually proves the header.
func (sh SignedHeader) ValidateBasic(chainID string) error {
	switch {
	case sh.Header == nil:
		return errors.New("missing header")
	case sh.Commit == nil:
		return errors.New("missing commit")
	}

	if err := sh.Header.ValidateBasic(); err != nil {
		return fmt.Errorf("invalid header: %w", err)
	}
	if err := sh.Commit.ValidateBasic(); err != nil {
		return fmt.Errorf("invalid commit: %w", err)
	}

	if sh.ChainID != chainID {
		return fmt.Errorf("header belongs to another chain %q, not %q", sh.ChainID, chainID)
	}

	// The commit must reference the same block as the header.
	if sh.Commit.Height != sh.Height {
		return fmt.Errorf("header and commit height mismatch: %d vs %d", sh.Height, sh.Commit.Height)
	}
	if hhash, chash := sh.Hash(), sh.Commit.BlockID.Hash; !bytes.Equal(hhash, chash) {
		return fmt.Errorf("commit signs block %X, header is block %X", chash, hhash)
	}

	return nil
}

// String returns a string representation of the SignedHeader.
func (sh SignedHeader) String() string {
	return sh.StringIndented("")
}

// StringIndented returns an indented string representation of the
// SignedHeader, showing its Header and Commit.
func (sh SignedHeader) StringIndented(indent string) string {
	return fmt.Sprintf(`SignedHeader{
%s  %v
%s  %v
%s}`,
		indent, sh.Header.StringIndented(indent+"  "),
		indent, sh.Commit.StringIndented(indent+"  "),
		indent)
}

// ToProto converts the SignedHeader to its protobuf representation.
func (sh *SignedHeader) ToProto() *tmproto.SignedHeader {
	if sh == nil {
		return nil
	}

	psh := &tmproto.SignedHeader{}
	if sh.Header != nil {
		psh.Header = sh.Header.ToProto()
	}
	if sh.Commit != nil {
		psh.Commit = sh.Commit.ToProto()
	}

	return psh
}

// SignedHeaderFromProto converts a protobuf SignedHeader back into a
// SignedHeader. It returns an error if the header or commit is invalid.
func SignedHeaderFromProto(shp *tmproto.SignedHeader) (*SignedHeader, error) {
	switch {
	case shp == nil:
		return nil, errors.New("nil SignedHeader")
	case shp.Header == nil:
		return nil, errors.New("nil SignedHeader header")
	case shp.Commit == nil:
		return nil, errors.New("nil SignedHeader commit")
	}

	h, err := HeaderFromProto(shp.Header)
	if err != nil {
		return nil, err
	}
	c, err := CommitFromProto(shp.Commit)
	if err != nil {
		return nil, err
	}
	return &SignedHeader{Header: &h, Commit: c}, nil
}

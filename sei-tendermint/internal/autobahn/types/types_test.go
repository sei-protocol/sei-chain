package types

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/tendermint/tendermint/internal/protoutils"
	"github.com/tendermint/tendermint/libs/utils"
)

func firstErr(errs ...error) error {
	for _, err := range errs {
		if err != nil {
			return err
		}
	}
	return nil
}

// msgTest tests a converter, generalized msg converter and signed msg converter.
func msgTest[T Msg, P protoutils.Message](rng utils.Rng, msg T, conv protoutils.Conv[T, P]) error {
	return firstErr(
		conv.Test(msg),
		MsgConv.Test(msg),
		SignedMsgConv[T]().Test(GenSigned(rng, msg)),
	)
}

// TestConv tests the protobuf converters.
func TestConv(t *testing.T) {
	rng := utils.TestRng()
	for range 10 {
		if err := firstErr(
			TimeConv.Test(utils.GenTimestamp(rng)),
			SignatureConv.Test(GenSignature(rng)),
			BlockHeaderConv.Test(GenBlockHeader(rng)),
			PayloadConv.Test(GenPayload(rng)),
			BlockConv.Test(GenBlock(rng)),
			LaneQCConv.Test(GenLaneQC(rng)),
			FullProposalConv.Test(GenFullProposal(rng)),
			PrepareQCConv.Test(GenPrepareQC(rng)),
			CommitQCConv.Test(GenCommitQC(rng)),
			FullCommitQCConv.Test(GenFullCommitQC(rng)),
			TimeoutQCConv.Test(GenTimeoutQC(rng)),
			FullTimeoutVoteConv.Test(GenFullTimeoutVote(rng)),
			AppProposalConv.Test(GenAppProposal(rng)),
			AppQCConv.Test(GenAppQC(rng)),
			AppVoteConv.Test(GenAppVote(rng)),
			ConsensusReqConv.Test(GenFullProposal(rng)),
			ConsensusReqConv.Test(&ConsensusReqPrepareVote{GenSigned(rng, GenPrepareVote(rng))}),
			ConsensusReqConv.Test(&ConsensusReqCommitVote{GenSigned(rng, GenCommitVote(rng))}),
			ConsensusReqConv.Test(GenFullTimeoutVote(rng)),
			ConsensusReqConv.Test(GenTimeoutQC(rng)),
			msgTest(rng, GenLaneProposal(rng), LaneProposalConv),
			msgTest(rng, GenLaneVote(rng), LaneVoteConv),
			msgTest(rng, GenProposal(rng), ProposalConv),
			msgTest(rng, GenPrepareVote(rng), PrepareVoteConv),
			msgTest(rng, GenCommitVote(rng), CommitVoteConv),
			msgTest(rng, GenTimeoutVote(rng), TimeoutVoteConv),
			msgTest(rng, GenAppVote(rng), AppVoteConv),
		); err != nil {
			t.Error(err)
		}
	}
}

func TestMarshal(t *testing.T) {
	rng := utils.TestRng()
	var got, want struct {
		K  PublicKey
		Mk utils.Option[PublicKey]
		My utils.Option[PublicKey]
	}
	want.K = GenPublicKey(rng)
	want.Mk = utils.Some(GenPublicKey(rng))
	j, err := json.Marshal(want)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(j, &got); err != nil {
		t.Fatal(err)
	}
	if err := utils.TestDiff(want, got); err != nil {
		t.Fatal(err)
	}
}

func makePrepareQC(keys []SecretKey, vote *PrepareVote) *PrepareQC {
	var votes []*Signed[*PrepareVote]
	for _, k := range keys {
		votes = append(votes, Sign(k, vote))
	}
	return NewPrepareQC(votes)
}

func TestNewTimeoutQC(t *testing.T) {
	rng := utils.TestRng()
	_, keys := GenCommittee(rng, 10)
	view := GenView(rng)
	var votes []*FullTimeoutVote
	wantView := View{}
	for _, k := range keys {
		pView := View{
			Index:  view.Index,
			Number: GenViewNumber(rng) % view.Number,
		}
		p := newProposal(pView, time.Now(), utils.GenSlice(rng, GenLaneRange), utils.Some(GenAppProposal(rng)))
		if wantView.Less(pView) {
			wantView = pView
		}
		votes = append(votes, NewFullTimeoutVote(k, view, utils.Some(makePrepareQC(keys, NewPrepareVote(p)))))
	}
	tQC := NewTimeoutQC(votes)
	pQC, ok := tQC.LatestPrepareQC().Get()
	if !ok {
		t.Fatalf("tQC.LatestPrepareQC() missing")
	}
	if gotView := pQC.View(); gotView != wantView {
		t.Fatalf("tQC.LatestPrepareQC().View() = %v, want %v", gotView, wantView)
	}
}

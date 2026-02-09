package grpc_test

import (
	"context"
	"net"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/test/bufconn"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-tendermint/crypto"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/log"
	tmrand "github.com/sei-protocol/sei-chain/sei-tendermint/libs/rand"
	tmgrpc "github.com/sei-protocol/sei-chain/sei-tendermint/privval/grpc"
	privvalproto "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/privval"
	tmproto "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/types"
)

const chainID = "chain-id"

func dialer(t *testing.T, pv types.PrivValidator, logger log.Logger) (*grpc.Server, func(context.Context, string) (net.Conn, error)) {
	listener := bufconn.Listen(1024 * 1024)

	server := grpc.NewServer()

	s := tmgrpc.NewSignerServer(logger, chainID, pv)

	privvalproto.RegisterPrivValidatorAPIServer(server, s)

	go func() { require.NoError(t, server.Serve(listener)) }()

	return server, func(context.Context, string) (net.Conn, error) {
		return listener.Dial()
	}
}

func TestSignerClient_GetPubKey(t *testing.T) {

	ctx := t.Context()

	mockPV := types.NewMockPV()
	logger := log.NewTestingLogger(t)
	srv, dialer := dialer(t, mockPV, logger)
	defer srv.Stop()

	conn, err := grpc.DialContext(ctx, "",
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithContextDialer(dialer),
	)
	require.NoError(t, err)
	defer conn.Close()

	client, err := tmgrpc.NewSignerClient(conn, chainID, logger)
	require.NoError(t, err)

	pk, err := client.GetPubKey(ctx)
	require.NoError(t, err)
	assert.Equal(t, mockPV.PrivKey.Public(), pk)
}

func TestSignerClient_SignVote(t *testing.T) {
	ctx := t.Context()

	mockPV := types.NewMockPV()
	logger := log.NewTestingLogger(t)
	srv, dialer := dialer(t, mockPV, logger)
	defer srv.Stop()

	conn, err := grpc.DialContext(ctx, "",
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithContextDialer(dialer),
	)
	require.NoError(t, err)
	defer conn.Close()

	client, err := tmgrpc.NewSignerClient(conn, chainID, logger)
	require.NoError(t, err)

	ts := time.Now()
	hash := tmrand.Bytes(crypto.HashSize)
	valAddr := tmrand.Bytes(crypto.AddressSize)

	want := &types.Vote{
		Type:             tmproto.PrecommitType,
		Height:           1,
		Round:            2,
		BlockID:          types.BlockID{Hash: hash, PartSetHeader: types.PartSetHeader{Hash: hash, Total: 2}},
		Timestamp:        ts,
		ValidatorAddress: valAddr,
		ValidatorIndex:   1,
	}

	have := &types.Vote{
		Type:             tmproto.PrecommitType,
		Height:           1,
		Round:            2,
		BlockID:          types.BlockID{Hash: hash, PartSetHeader: types.PartSetHeader{Hash: hash, Total: 2}},
		Timestamp:        ts,
		ValidatorAddress: valAddr,
		ValidatorIndex:   1,
	}

	pbHave := have.ToProto()

	err = client.SignVote(ctx, chainID, pbHave)
	require.NoError(t, err)

	pbWant := want.ToProto()

	require.NoError(t, mockPV.SignVote(ctx, chainID, pbWant))

	assert.Equal(t, pbWant.Signature, pbHave.Signature)
}

func TestSignerClient_SignProposal(t *testing.T) {
	ctx := t.Context()

	mockPV := types.NewMockPV()
	logger := log.NewTestingLogger(t)
	srv, dialer := dialer(t, mockPV, logger)
	defer srv.Stop()

	conn, err := grpc.DialContext(ctx, "",
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithContextDialer(dialer),
	)
	require.NoError(t, err)
	defer conn.Close()

	client, err := tmgrpc.NewSignerClient(conn, chainID, logger)
	require.NoError(t, err)

	ts := time.Now()
	hash := tmrand.Bytes(crypto.HashSize)

	have := &types.Proposal{
		Type:      tmproto.ProposalType,
		Height:    1,
		Round:     2,
		POLRound:  2,
		BlockID:   types.BlockID{Hash: hash, PartSetHeader: types.PartSetHeader{Hash: hash, Total: 2}},
		Timestamp: ts,
	}
	want := &types.Proposal{
		Type:      tmproto.ProposalType,
		Height:    1,
		Round:     2,
		POLRound:  2,
		BlockID:   types.BlockID{Hash: hash, PartSetHeader: types.PartSetHeader{Hash: hash, Total: 2}},
		Timestamp: ts,
	}

	pbHave := have.ToProto()

	err = client.SignProposal(ctx, chainID, pbHave)
	require.NoError(t, err)

	pbWant := want.ToProto()

	require.NoError(t, mockPV.SignProposal(ctx, chainID, pbWant))

	assert.Equal(t, pbWant.Signature, pbHave.Signature)
}

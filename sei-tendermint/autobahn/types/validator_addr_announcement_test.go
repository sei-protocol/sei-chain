package types

import (
	"fmt"
	"testing"

	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/pb"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
)

func testAnnouncement(endpoints ...ValidatorEndpoint) ValidatorAddrAnnouncement {
	return ValidatorAddrAnnouncement{
		Version:   7,
		NodeKey:   "node",
		HostPort:  "10.0.0.1:26656",
		Endpoints: endpoints,
	}
}

func TestValidatorAddrAnnouncementConv_RoundTripsEndpointsInOrder(t *testing.T) {
	want := testAnnouncement(
		ValidatorEndpoint{Name: "rpc", HostPort: "10.0.0.1:8545"},
		ValidatorEndpoint{Name: "evm", HostPort: "10.0.0.1:8546"},
		ValidatorEndpoint{Name: "rpc", HostPort: "10.0.0.2:8545"},
	)
	got, err := ValidatorAddrAnnouncementConv.Unmarshal(ValidatorAddrAnnouncementConv.Marshal(want))
	require.NoError(t, err)
	require.Equal(t, want, got)
}

func TestValidatorAddrAnnouncementConv_RoundTripsWithoutEndpoints(t *testing.T) {
	want := testAnnouncement()
	got, err := ValidatorAddrAnnouncementConv.Unmarshal(ValidatorAddrAnnouncementConv.Marshal(want))
	require.NoError(t, err)
	require.Equal(t, want.Version, got.Version)
	require.Equal(t, want.NodeKey, got.NodeKey)
	require.Equal(t, want.HostPort, got.HostPort)
	require.Equal(t, 0, len(got.Endpoints))
}

func TestValidatorAddrAnnouncementConv_EndpointsChangeTheEncoding(t *testing.T) {
	without := ValidatorAddrAnnouncementConv.Marshal(testAnnouncement())
	with := ValidatorAddrAnnouncementConv.Marshal(testAnnouncement(ValidatorEndpoint{Name: "rpc", HostPort: "10.0.0.1:8545"}))
	other := ValidatorAddrAnnouncementConv.Marshal(testAnnouncement(ValidatorEndpoint{Name: "rpc", HostPort: "10.0.0.1:9999"}))
	require.NotEqual(t, string(without), string(with))
	require.NotEqual(t, string(with), string(other))
}

func TestValidatorAddrAnnouncementConv_AcceptsMaxEndpoints(t *testing.T) {
	endpoints := make([]ValidatorEndpoint, 8)
	for i := range endpoints {
		endpoints[i] = ValidatorEndpoint{Name: fmt.Sprint("e", i), HostPort: fmt.Sprint("10.0.0.1:", 9000+i)}
	}
	want := testAnnouncement(endpoints...)
	got, err := ValidatorAddrAnnouncementConv.Unmarshal(ValidatorAddrAnnouncementConv.Marshal(want))
	require.NoError(t, err)
	require.Equal(t, want, got)
}

func TestValidatorAddrAnnouncementConv_RejectsTooManyEndpoints(t *testing.T) {
	endpoints := make([]ValidatorEndpoint, 9)
	for i := range endpoints {
		endpoints[i] = ValidatorEndpoint{Name: "e", HostPort: "10.0.0.1:9000"}
	}
	_, err := ValidatorAddrAnnouncementConv.Unmarshal(ValidatorAddrAnnouncementConv.Marshal(testAnnouncement(endpoints...)))
	require.Error(t, err)
}

func TestValidatorAddrAnnouncementConv_RejectsOversizedEndpointFields(t *testing.T) {
	long := func(n int) string { return string(make([]byte, n)) }
	for name, endpoint := range map[string]ValidatorEndpoint{
		"name":      {Name: long(65), HostPort: "10.0.0.1:9000"},
		"host_port": {Name: "rpc", HostPort: long(261)},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := ValidatorAddrAnnouncementConv.Unmarshal(ValidatorAddrAnnouncementConv.Marshal(testAnnouncement(endpoint)))
			require.Error(t, err)
		})
	}
}

func TestValidatorAddrAnnouncementConv_RejectsEndpointMissingFields(t *testing.T) {
	for name, endpoint := range map[string]*pb.ValidatorEndpoint{
		"name":      {HostPort: new("10.0.0.1:9000")},
		"host_port": {Name: new("rpc")},
	} {
		t.Run(name, func(t *testing.T) {
			msg := ValidatorAddrAnnouncementConv.Encode(testAnnouncement())
			msg.Endpoints = []*pb.ValidatorEndpoint{endpoint}
			_, err := ValidatorAddrAnnouncementConv.Decode(msg)
			require.Error(t, err)
		})
	}
}

func TestValidatorAddrMsgConv_RoundTripsEachVariant(t *testing.T) {
	rng := utils.TestRng()
	for range 10 {
		require.NoError(t, ValidatorAddrMsgConv.Test(ValidatorAddrAnnouncementMsg{
			Ann: GenSecretKey(rng).SignValidatorAddr(testAnnouncement(ValidatorEndpoint{Name: "rpc", HostPort: "10.0.0.1:8545"})),
		}))
		require.NoError(t, ValidatorAddrMsgConv.Test(ValidatorAddrAck{
			Validator: GenPublicKey(rng),
			Version:   rng.Uint64(),
		}))
	}
}

func TestValidatorAddrMsgConv_RejectsEmptyMessage(t *testing.T) {
	_, err := ValidatorAddrMsgConv.Decode(&pb.ValidatorAddrMsg{})
	require.Error(t, err)
}

func TestValidatorAddrMsgConv_RejectsUndecodableVariants(t *testing.T) {
	for name, msg := range map[string]*pb.ValidatorAddrMsg{
		"ack":          {T: &pb.ValidatorAddrMsg_Ack{Ack: &pb.ValidatorAddrAck{}}},
		"announcement": {T: &pb.ValidatorAddrMsg_Announcement{Announcement: &pb.SignedValidatorAddrAnnouncement{}}},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := ValidatorAddrMsgConv.Decode(msg)
			require.Error(t, err)
		})
	}
}

func TestSignedValidatorAddrAnnouncement_VerifiesAfterRoundTrip(t *testing.T) {
	rng := utils.TestRng()
	key := GenSecretKey(rng)
	signed := key.SignValidatorAddr(testAnnouncement(ValidatorEndpoint{Name: "rpc", HostPort: "10.0.0.1:8545"}))
	got, err := SignedValidatorAddrAnnouncementConv.Unmarshal(SignedValidatorAddrAnnouncementConv.Marshal(signed))
	require.NoError(t, err)
	require.Equal(t, key.Public(), got.Key())
	require.NoError(t, got.VerifySig())
}

func TestSignedValidatorAddrAnnouncement_RejectsChangedVersion(t *testing.T) {
	rng := utils.TestRng()
	signed := GenSecretKey(rng).SignValidatorAddr(testAnnouncement())
	p := SignedValidatorAddrAnnouncementConv.Encode(signed)
	p.Msg.Version = new(uint64(8))
	got, err := SignedValidatorAddrAnnouncementConv.Decode(p)
	require.NoError(t, err)
	require.Error(t, got.VerifySig())
}

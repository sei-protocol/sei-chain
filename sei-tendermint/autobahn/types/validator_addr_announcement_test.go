package types

import (
	"fmt"
	"testing"

	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/pb"
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

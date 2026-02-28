package p2p

import (
	"net/netip"
	"testing"

	"github.com/sei-protocol/sei-chain/sei-tendermint/crypto/ed25519"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/tcp"
	"github.com/sei-protocol/sei-chain/sei-tendermint/types"
)

func TestNewNodeID(t *testing.T) {
	// Most tests are in TestNodeID_Validate, this just checks that it's validated.
	testcases := []struct {
		input  string
		expect types.NodeID
		ok     bool
	}{
		{"", "", false},
		{"foo", "", false},
		{"00112233445566778899aabbccddeeff00112233", "00112233445566778899aabbccddeeff00112233", true},
		{"00112233445566778899AABBCCDDEEFF00112233", "00112233445566778899aabbccddeeff00112233", true},
		{"00112233445566778899aabbccddeeff0011223", "", false},
		{"00112233445566778899aabbccddeeff0011223g", "", false},
	}
	for _, tc := range testcases {
		t.Run(tc.input, func(t *testing.T) {
			id, err := types.NewNodeID(tc.input)
			if !tc.ok {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				require.Equal(t, id, tc.expect)
			}
		})
	}
}

func TestNewNodeIDFromPubKey(t *testing.T) {
	privKey := ed25519.TestSecretKey([]byte{43, 55, 33})
	nodeID := types.NodeIDFromPubKey(privKey.Public())
	require.NoError(t, nodeID.Validate())
}

func TestNodeID_Bytes(t *testing.T) {
	testcases := []struct {
		nodeID types.NodeID
		expect []byte
		ok     bool
	}{
		{"", []byte{}, true},
		{"01f0", []byte{0x01, 0xf0}, true},
		{"01F0", []byte{0x01, 0xf0}, true},
		{"01F", nil, false},
		{"01g0", nil, false},
	}
	for _, tc := range testcases {
		t.Run(string(tc.nodeID), func(t *testing.T) {
			bz, err := tc.nodeID.Bytes()
			if tc.ok {
				require.NoError(t, err)
				require.Equal(t, tc.expect, bz)
			} else {
				require.Error(t, err)
			}
		})
	}
}

func TestNodeID_Validate(t *testing.T) {
	testcases := []struct {
		nodeID types.NodeID
		ok     bool
	}{
		{"", false},
		{"00", false},
		{"00112233445566778899aabbccddeeff00112233", true},
		{"00112233445566778899aabbccddeeff001122334", false},
		{"00112233445566778899aabbccddeeffgg001122", false},
		{"00112233445566778899AABBCCDDEEFF00112233", false},
	}
	for _, tc := range testcases {
		t.Run(string(tc.nodeID), func(t *testing.T) {
			err := tc.nodeID.Validate()
			if tc.ok {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
		})
	}
}

func TestParseNodeAddress(t *testing.T) {
	user := "00112233445566778899aabbccddeeff00112233"
	id := types.NodeID(user)

	testcases := []struct {
		url    string
		expect NodeAddress
		ok     bool
	}{
		// Valid addresses.
		{
			user + "@127.0.0.1",
			NodeAddress{NodeID: id, Hostname: "127.0.0.1", Port: defaultPort},
			true,
		},
		{
			user + "@hostname.domain",
			NodeAddress{NodeID: id, Hostname: "hostname.domain", Port: defaultPort},
			true,
		},
		{
			user + "@hostname.domain:80",
			NodeAddress{NodeID: id, Hostname: "hostname.domain", Port: 80},
			true,
		},
		{
			user + "@%F0%9F%91%8B",
			NodeAddress{NodeID: id, Hostname: "👋", Port: defaultPort},
			true,
		},
		{
			user + "@%F0%9F%91%8B:80/path",
			NodeAddress{NodeID: id, Hostname: "👋", Port: 80},
			true,
		},
		{
			user + "@127.0.0.1:26657",
			NodeAddress{NodeID: id, Hostname: "127.0.0.1", Port: 26657},
			true,
		},
		{
			user + "@0.0.0.0:0",
			NodeAddress{NodeID: id, Hostname: "0.0.0.0", Port: defaultPort},
			true,
		},
		{
			user + "@[1::]",
			NodeAddress{NodeID: id, Hostname: "1::", Port: defaultPort},
			true,
		},
		{
			user + "@[fd80:b10c::2]:1234",
			NodeAddress{NodeID: id, Hostname: "fd80:b10c::2", Port: 1234},
			true,
		},

		// Invalid addresses.
		{"", NodeAddress{}, false},
		{"127.0.0.1", NodeAddress{}, false},
		{"hostname", NodeAddress{}, false},
		{"scheme:", NodeAddress{}, false},
		{user + "@%F%F0", NodeAddress{}, false},
		{"//" + user + "@127.0.0.1", NodeAddress{}, false},
		{"://" + user + "@127.0.0.1", NodeAddress{}, false},
		{"mconn://foo@127.0.0.1", NodeAddress{}, false},
		{"mconn://" + user + "@127.0.0.1:65536", NodeAddress{}, false},
		{"mconn://" + user + "@:80", NodeAddress{}, false},
	}
	for _, tc := range testcases {
		t.Run(tc.url, func(t *testing.T) {
			address, err := ParseNodeAddress(tc.url)
			if !tc.ok {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				require.Equal(t, tc.expect, address)
			}
		})
	}
}

func TestNodeAddress_Resolve(t *testing.T) {
	testcases := []struct {
		address NodeAddress
		expect  Endpoint
		ok      bool
	}{
		// Valid networked addresses (with hostname).
		{
			NodeAddress{Hostname: "127.0.0.1", Port: 80},
			Endpoint{netip.AddrPortFrom(tcp.IPv4Loopback(), 80)},
			true,
		},
		{
			NodeAddress{Hostname: "127.0.0.1"},
			Endpoint{netip.AddrPortFrom(tcp.IPv4Loopback(), 0)},
			true,
		},
		{
			NodeAddress{Hostname: "::1"},
			Endpoint{netip.AddrPortFrom(netip.IPv6Loopback(), 0)},
			true,
		},
		{
			NodeAddress{Hostname: "8.8.8.8"},
			Endpoint{netip.AddrPortFrom(netip.AddrFrom4([4]byte{8, 8, 8, 8}), 0)},
			true,
		},
		{
			NodeAddress{Hostname: "2001:0db8::ff00:0042:8329"},
			Endpoint{netip.AddrPortFrom(netip.AddrFrom16([16]byte{
				0x20, 0x01, 0x0d, 0xb8, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0xff, 0x00, 0x00, 0x42, 0x83, 0x29}), 0)},
			true,
		},
		{
			NodeAddress{Hostname: "some.missing.host.tendermint.com"},
			Endpoint{},
			false,
		},

		// Invalid addresses.
		{NodeAddress{}, Endpoint{}, false},
		{NodeAddress{Hostname: "127.0.0.1:80"}, Endpoint{}, false},
		{NodeAddress{Hostname: "💥"}, Endpoint{}, false},
	}
	for _, tc := range testcases {
		t.Run(tc.address.String(), func(t *testing.T) {
			endpoints, err := tc.address.Resolve(t.Context())
			if !tc.ok {
				require.Error(t, err)
				return
			}
			ok := false
			for _, e := range endpoints {
				ok = ok || e == tc.expect
			}
			if !ok {
				t.Fatalf("%v not in %v", tc.expect, endpoints)
			}
		})
	}
	t.Run("Resolve localhost", func(t *testing.T) {
		addr := NodeAddress{Hostname: "localhost", Port: 80}
		endpoints, err := addr.Resolve(t.Context())
		require.NoError(t, err)
		require.True(t, len(endpoints) > 0)
		for _, got := range endpoints {
			require.True(t, got.Addr().IsLoopback())
			// Any loopback address is acceptable, so ignore it in comparison.
			want := Endpoint{netip.AddrPortFrom(got.Addr(), 80)}
			require.Equal(t, want, got)
		}
	})
}

func TestNodeAddress_String(t *testing.T) {
	id := types.NodeID("00112233445566778899aabbccddeeff00112233")
	user := string(id)
	testcases := []struct {
		address NodeAddress
		expect  string
	}{
		// Valid networked addresses (with hostname).
		{
			NodeAddress{NodeID: id, Hostname: "host", Port: 80},
			"mconn://" + user + "@host:80",
		},
		{
			NodeAddress{NodeID: id, Hostname: "host.domain"},
			"mconn://" + user + "@host.domain",
		},

		// Addresses with weird contents, which are technically fine (not harmful).
		{
			NodeAddress{NodeID: "👨", Hostname: "💻", Port: 80},
			"mconn://%F0%9F%91%A8@%F0%9F%92%BB:80",
		},
	}
	for _, tc := range testcases {
		t.Run(tc.address.String(), func(t *testing.T) {
			require.Equal(t, tc.expect, tc.address.String())
		})
	}
}

func TestNodeAddress_IsPublic(t *testing.T) {
	rng := utils.TestRng()
	id := makeNodeID(rng)
	testcases := map[string]bool{
		"192.168.1.10":  false,
		"93.184.216.34": true,
		"example.com":   true,
		"100.64.0.1":    false,
	}
	for hostname, isPublic := range testcases {
		addr := NodeAddress{NodeID: id, Hostname: hostname, Port: defaultPort}
		require.Equal(t, isPublic, addr.IsPublic())
	}
}

func TestNodeAddress_Validate(t *testing.T) {
	id := types.NodeID("00112233445566778899aabbccddeeff00112233")
	testcases := []struct {
		address NodeAddress
		ok      bool
	}{
		// Valid addresses.
		{NodeAddress{NodeID: id, Hostname: "host", Port: 80}, true},

		// Invalid addresses.
		{NodeAddress{}, false},
		{NodeAddress{NodeID: id, Hostname: "host"}, false},
		{NodeAddress{NodeID: "foo", Hostname: "host", Port: defaultPort}, false},
		{NodeAddress{NodeID: id, Port: defaultPort}, false},
	}
	for _, tc := range testcases {
		t.Run(tc.address.String(), func(t *testing.T) {
			err := tc.address.Validate()
			if tc.ok {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
		})
	}
}

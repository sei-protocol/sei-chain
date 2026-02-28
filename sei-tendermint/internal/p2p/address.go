package p2p

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/sei-protocol/sei-chain/sei-tendermint/types"
)

const defaultPort uint16 = 26657

var (
	// stringHasScheme tries to detect URLs with schemes. It looks for a : before a / (if any).
	stringHasScheme = func(str string) bool {
		return strings.Contains(str, "://")
	}

	// reSchemeIsHost tries to detect URLs where the scheme part is instead a
	// hostname, i.e. of the form "host:80/path" where host: is a hostname.
	reSchemeIsHost = regexp.MustCompile(`^[^/:]+:\d+(/|$)`)
)

// NodeAddress is a node address URL. It differs from a transport Endpoint in
// that it contains the node's ID, and that the address hostname may be resolved
// into multiple IP addresses (and thus multiple endpoints).
//
// If the URL is opaque, i.e. of the form "scheme:opaque", then the opaque part
// is expected to contain a node ID.
type NodeAddress struct {
	NodeID   types.NodeID
	Hostname string
	Port     uint16
}

var cgnat = netip.MustParsePrefix("100.64.0.0/10")

// IsPublic checks if the address is routable from the public internet.
// It is good enough to exclude internal addresses of cloud providers.
// As a simplification, it treats non-IP Hostnames (DNS addresses) as public.
// TODO(gprusak): DNS addresses should be eliminated from PEX entirely - all
// addresses should be resolved locally and only then advertised to peers.
func (a NodeAddress) IsPublic() bool {
	ip, err := netip.ParseAddr(a.Hostname)
	if err != nil {
		return true
	}
	return ip.IsGlobalUnicast() && !ip.IsPrivate() && !cgnat.Contains(ip.Unmap())
}

// ParseNodeAddress parses a node address URL into a NodeAddress, normalizing
// and validating it.
func ParseNodeAddress(urlString string) (NodeAddress, error) {
	// url.Parse requires a scheme, so if it fails to parse a scheme-less URL
	// we try to apply a default scheme.
	url, err := url.Parse(urlString)
	if (err != nil || url.Scheme == "") &&
		(!stringHasScheme(urlString) || reSchemeIsHost.MatchString(urlString)) {
		url, err = url.Parse("mconn://" + urlString)
	}
	if err != nil {
		return NodeAddress{}, fmt.Errorf("invalid node address %q: %w", urlString, err)
	}

	address := NodeAddress{}

	// Opaque URLs are expected to contain only a node ID.
	if url.Opaque != "" {
		address.NodeID = types.NodeID(url.Opaque)
		return address, address.Validate()
	}

	// Otherwise, just parse a normal networked URL.
	if url.User != nil {
		address.NodeID = types.NodeID(strings.ToLower(url.User.Username()))
	}

	address.Hostname = url.Hostname()

	if portString := url.Port(); portString != "" {
		port64, err := strconv.ParseUint(portString, 10, 16)
		if err != nil {
			return NodeAddress{}, fmt.Errorf("invalid port %q: %w", url.Port(), err)
		}
		address.Port = uint16(port64)
	}
	// For some reasons, missing or 0 port on parsing is interpretented as the default port.
	if address.Port == 0 {
		address.Port = defaultPort
	}
	return address, address.Validate()
}

// Resolve resolves a NodeAddress into a set of Endpoints, by expanding
// out a DNS hostname to IP addresses.
func (a NodeAddress) Resolve(ctx context.Context) ([]Endpoint, error) {
	// LookIP for some reason returns IPv6-embedded addresses.
	ips, err := net.DefaultResolver.LookupIP(ctx, "ip", a.Hostname)
	if err != nil {
		return nil, err
	}
	endpoints := make([]Endpoint, len(ips))
	for i, ip := range ips {
		ip, ok := netip.AddrFromSlice(ip)
		if !ok {
			return nil, fmt.Errorf("LookupIP returned invalid IP %q", ip)
		}
		endpoints[i] = Endpoint{netip.AddrPortFrom(ip.Unmap(), a.Port)}
	}
	return endpoints, nil
}

// String formats the address as a URL string.
func (a NodeAddress) String() string {
	u := url.URL{Scheme: "mconn"}
	if a.NodeID != "" {
		u.User = url.User(string(a.NodeID))
	}
	if a.Port > 0 {
		u.Host = net.JoinHostPort(a.Hostname, strconv.Itoa(int(a.Port)))
	} else {
		u.Host = a.Hostname
	}
	return strings.TrimPrefix(u.String(), "//")
}

// Validate validates a NodeAddress.
func (a NodeAddress) Validate() error {
	if a.NodeID == "" {
		return errors.New("no peer ID")
	}
	if err := a.NodeID.Validate(); err != nil {
		return fmt.Errorf("invalid peer ID: %w", err)
	}
	if a.Port == 0 {
		return errors.New("missing port")
	}
	if a.Hostname == "" {
		return errors.New("missing hostname")
	}
	return nil
}

package utils

import (
	"fmt"
	"net"
	"net/url"
	"strings"
)

// CheckRPCURL reports whether u is a ws or wss URL with a host and without
// userinfo. It does not reject loopback, link-local, unspecified, zone IDs,
// or names that resolve to those.
func CheckRPCURL(u url.URL) error {
	switch u.Scheme {
	case "ws", "wss":
	default:
		return fmt.Errorf("scheme %q, want ws or wss", u.Scheme)
	}
	if u.Host == "" {
		return fmt.Errorf("missing host")
	}
	if u.User != nil {
		return fmt.Errorf("userinfo not allowed")
	}
	return nil
}

// IsLoopbackOrLinkLocalURL reports whether u's host is the name "localhost", a
// loopback IP, an unspecified IP (0.0.0.0 / ::), or a link-local IP. A host
// that resolves to one of those through DNS is not detected.
func IsLoopbackOrLinkLocalURL(u url.URL) bool {
	host := u.Hostname()
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && (ip.IsLoopback() || ip.IsUnspecified() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast())
}

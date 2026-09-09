package utils

import (
	"fmt"
	"net"
	"net/url"
	"strings"
)

// CheckHTTPURL reports whether u is an http or https URL with a host and
// without userinfo.
//
// TODO: need more complete checks.
func CheckHTTPURL(u url.URL) error {
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("scheme %q, want http or https", u.Scheme)
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
// loopback IP, or a link-local IP. A host that resolves to one of those through
// DNS is not detected.
func IsLoopbackOrLinkLocalURL(u url.URL) bool {
	host := u.Hostname()
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && (ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast())
}

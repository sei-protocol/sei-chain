package utils

import (
	"fmt"
	"net/url"
)

// CheckHTTPURL reports whether u is an http or https URL with a host and
// without userinfo.
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

package otlp

import (
	"fmt"
	"net/url"
	"strings"
)

// splitEndpointScheme splits an optional URL scheme off an OTLP endpoint
// value. The exporters' WithEndpoint expects host:port, so a value written
// as a URL (the form most OTel docs show, e.g. http://collector:4318) must
// have its scheme removed before it is stored. The scheme additionally
// carries TLS intent: http means plaintext, https means TLS. tls is nil
// when the value has no scheme, leaving the TLS decision to the tls block.
func splitEndpointScheme(raw string) (hostPort string, tls *bool, err error) {
	if !strings.Contains(raw, "://") {
		return raw, nil, nil
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", nil, fmt.Errorf("invalid endpoint URL: %w", err)
	}
	if u.User != nil {
		return "", nil, fmt.Errorf("endpoint: credentials do not belong in the endpoint; use headers")
	}
	var tlsVal bool
	switch u.Scheme {
	case "http":
		tlsVal = false
	case "https":
		tlsVal = true
	default:
		return "", nil, fmt.Errorf("unsupported scheme %q in endpoint: only http:// and https:// are accepted; give host:port otherwise", u.Scheme)
	}
	if u.Path != "" && u.Path != "/" {
		return "", nil, fmt.Errorf("endpoint must be host:port; the URL path is set by the signal (got %q)", u.Path)
	}
	if u.RawQuery != "" || u.Fragment != "" {
		return "", nil, fmt.Errorf("endpoint must be host:port; query and fragment are not allowed")
	}
	if u.Hostname() == "" {
		return "", nil, fmt.Errorf("endpoint has an empty host; give host:port")
	}
	return u.Host, &tlsVal, nil
}

// fallbackEndpointScheme remembers a fallback endpoint's original value and
// the TLS intent carried by its URL scheme until the root TLS outcome is
// final.
type fallbackEndpointScheme struct {
	entry string
	tls   *bool
}

// tlsEnabledExplicit reports whether a raw tls block sets `enabled`
// explicitly, so a URL scheme may only steer TLS when the operator did
// not state it.
func tlsEnabledExplicit(raw interface{}) bool {
	m := readStringKeyedMap(raw)
	if m == nil {
		return false
	}
	_, ok := m["enabled"].(bool)
	return ok
}

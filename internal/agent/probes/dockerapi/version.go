// Package dockerapi picks the Docker Engine API version a probe speaks.
//
// The Engine refuses a request whose version prefix lies outside the range
// it serves, and that range moves: Engine 29 serves 1.44 to 1.52 and
// refuses 1.43, while Engine 20.10 serves nothing above 1.41. A fixed
// prefix therefore breaks on one end of the installed base or the other.
// The version is read from the daemon and clamped, which is what the
// Docker SDK's own negotiation does.
package dockerapi

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
)

// Highest is the newest API version whose responses the probes are known
// to decode. A daemon serving more is spoken to at this version; a daemon
// whose minimum is above it is spoken to at its minimum.
const Highest = "1.44"

// Version remembers the negotiated version for one probe. The zero value
// is ready to use.
type Version struct {
	mu sync.Mutex
	v  string
}

// URL builds the request URL for path (starting with /) on the socket
// client, negotiating the version on first use. When the daemon cannot be
// asked, Highest is used and negotiation is retried on the next call.
func (s *Version) URL(client *http.Client, path string) string {
	return "http://localhost/v" + s.get(client) + path
}

// Forget drops the negotiated version, so the next call asks again. Call
// it when the daemon refuses a version: it may have been upgraded while
// the agent ran.
func (s *Version) Forget() {
	s.mu.Lock()
	s.v = ""
	s.mu.Unlock()
}

func (s *Version) get(client *http.Client) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.v != "" {
		return s.v
	}
	v, err := Negotiate(client)
	if err != nil {
		return Highest
	}
	s.v = v
	return v
}

// Negotiate asks the daemon which versions it serves, on the unversioned
// /version endpoint, and returns the one to speak.
func Negotiate(client *http.Client) (string, error) {
	resp, err := client.Get("http://localhost/version")
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("docker /version returned %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", fmt.Errorf("reading docker /version: %w", err)
	}
	var info struct {
		APIVersion    string `json:"ApiVersion"`
		MinAPIVersion string `json:"MinAPIVersion"`
	}
	if err := json.Unmarshal(body, &info); err != nil {
		return "", fmt.Errorf("decoding docker /version: %w", err)
	}
	if info.APIVersion == "" {
		return "", fmt.Errorf("docker /version named no API version")
	}
	return Choose(info.APIVersion, info.MinAPIVersion), nil
}

// Choose clamps Highest into the range the daemon serves.
func Choose(serverMax, serverMin string) string {
	v := Highest
	if less(serverMax, v) {
		v = serverMax
	}
	if serverMin != "" && less(v, serverMin) {
		v = serverMin
	}
	return v
}

// less compares two "major.minor" versions numerically: 1.9 < 1.44.
func less(a, b string) bool {
	amaj, amin := split(a)
	bmaj, bmin := split(b)
	if amaj != bmaj {
		return amaj < bmaj
	}
	return amin < bmin
}

func split(v string) (int, int) {
	major, minor, _ := strings.Cut(strings.TrimPrefix(v, "v"), ".")
	ma, _ := strconv.Atoi(major)
	mi, _ := strconv.Atoi(minor)
	return ma, mi
}

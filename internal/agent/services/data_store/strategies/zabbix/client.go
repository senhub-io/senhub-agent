package zabbix

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// client speaks the active-agent side of the Zabbix protocol: one TCP
// connection per request, a JSON document each way. It uses the fields a
// 6.0 server understands, which every later server still accepts, so the
// same agent talks to 6.0 LTS, 7.0 LTS and 8.x.
type client struct {
	cfg     Config
	tlsConf *tls.Config
	session string
	nextID  atomic.Uint64
	addrs   []string
	dial    func(ctx context.Context, addr string) (net.Conn, error)

	mu       sync.Mutex
	idx      int
	redirect string
	rev      int64
}

// activeItem is one item the server wants for this host.
type activeItem struct {
	Key    string          `json:"key"`
	ItemID int64           `json:"itemid"`
	Delay  json.RawMessage `json:"delay"`
}

// agentValue is one value pushed to the server.
type agentValue struct {
	Host  string `json:"host"`
	Key   string `json:"key"`
	Value string `json:"value"`
	ID    uint64 `json:"id"`
	Clock int64  `json:"clock"`
	NS    int64  `json:"ns"`
}

type response struct {
	Response string       `json:"response"`
	Info     string       `json:"info"`
	Data     []activeItem `json:"data"`
	Redirect *redirection `json:"redirect"`
}

// redirection is how a proxy group answers every request type when the
// member being asked is not the one holding this host. The reply carries
// no data and says "failed", so a client that only reads the response
// field concludes the server refused it and never collects anything.
type redirection struct {
	Address  string `json:"address"`
	Revision int64  `json:"revision"`
}

// pushResult is what the server said it did with a batch.
type pushResult struct {
	Processed, Failed, Total int
}

func newClient(cfg Config) (*client, error) {
	c := &client{cfg: cfg, session: newSession()}
	if cfg.TLS.Enabled {
		tc, err := buildTLS(cfg.TLS, cfg.Server)
		if err != nil {
			return nil, err
		}
		c.tlsConf = tc
	}
	c.addrs = cfg.addresses()
	if len(c.addrs) == 0 {
		return nil, fmt.Errorf("zabbix: no server address configured")
	}
	c.dial = c.dialServer
	return c, nil
}

func newSession() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return strconv.FormatInt(time.Now().UnixNano(), 16)
	}
	return hex.EncodeToString(b)
}

func buildTLS(t TLSConfig, server string) (*tls.Config, error) {
	conf := &tls.Config{MinVersion: tls.VersionTLS12, InsecureSkipVerify: t.InsecureSkipVerify} // #nosec G402 - operator opt-in
	// The name is left empty when the operator pinned none, so each
	// connection can present the name of the address it is dialling: a
	// proxy group sends us to members the configuration never listed.
	conf.ServerName = t.ServerName
	_ = server
	if t.CAFile != "" {
		pem, err := os.ReadFile(t.CAFile) // #nosec G304 - operator-supplied path
		if err != nil {
			return nil, fmt.Errorf("zabbix: reading tls.ca_file: %w", err)
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(pem) {
			return nil, fmt.Errorf("zabbix: tls.ca_file %s holds no certificate", t.CAFile)
		}
		conf.RootCAs = pool
	}
	if t.CertFile != "" {
		cert, err := tls.LoadX509KeyPair(t.CertFile, t.KeyFile)
		if err != nil {
			return nil, fmt.Errorf("zabbix: loading tls.cert_file/key_file: %w", err)
		}
		conf.Certificates = []tls.Certificate{cert}
	}
	return conf, nil
}

func (c *client) dialServer(ctx context.Context, addr string) (net.Conn, error) {
	d := net.Dialer{Timeout: c.cfg.Timeout}
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, err
	}
	if c.tlsConf == nil {
		return conn, nil
	}
	conf := c.tlsConf
	if conf.ServerName == "" {
		if host, _, splitErr := net.SplitHostPort(addr); splitErr == nil {
			conf = c.tlsConf.Clone()
			conf.ServerName = host
		}
	}
	tc := tls.Client(conn, conf)
	if err := tc.HandshakeContext(ctx); err != nil {
		conn.Close()
		return nil, err
	}
	return tc, nil
}

// target is the address to talk to: the one a proxy group sent us to, or
// the configured address we are currently on.
func (c *client) target() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.redirect != "" {
		return c.redirect
	}
	return c.addrs[c.idx]
}

// follow records where a proxy group wants us and reports whether that
// changed anything. The revision it sends with the address orders the
// group's decisions, so a reply that overtook a newer one is dropped.
func (c *client) follow(r redirection) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if r.Address == "" || r.Revision < c.rev {
		return false
	}
	if c.redirect == r.Address {
		c.rev = r.Revision
		return false
	}
	c.redirect = r.Address
	c.rev = r.Revision
	return true
}

// stepAside is called when the address we are on stops answering. A
// redirection is forgotten entirely, because the member that held this
// host is the one that went down and the group's other members are what
// can say where it went; otherwise we move on to the next configured
// address.
func (c *client) stepAside() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.redirect != "" {
		c.redirect = ""
		c.rev = 0
		return
	}
	c.idx = (c.idx + 1) % len(c.addrs)
}

// exchange sends one request, reads the reply, and follows a proxy
// group's redirection once. Every request type is redirected the same
// way, so handling it here is what keeps the check list, the values and
// the heartbeat on the same member.
func (c *client) exchange(ctx context.Context, req interface{}) (response, error) {
	resp, err := c.roundTrip(ctx, req)
	if err != nil || resp.Redirect == nil || !c.follow(*resp.Redirect) {
		return resp, err
	}
	resp, err = c.roundTrip(ctx, req)
	if err == nil && resp.Redirect != nil {
		// Moved again while we were moving. Remember it and let the next
		// request go straight there rather than chasing it in a loop.
		c.follow(*resp.Redirect)
	}
	return resp, err
}

// roundTrip sends one request to the current target and reads the reply.
func (c *client) roundTrip(ctx context.Context, req interface{}) (response, error) {
	var resp response
	payload, err := json.Marshal(req)
	if err != nil {
		return resp, fmt.Errorf("encoding request: %w", err)
	}
	ctx, cancel := context.WithTimeout(ctx, c.cfg.Timeout)
	defer cancel()

	addr := c.target()
	conn, err := c.dial(ctx, addr)
	if err != nil {
		c.stepAside()
		return resp, fmt.Errorf("connecting to %s: %w", addr, err)
	}
	defer conn.Close()
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	}
	if err := writeFrame(conn, payload); err != nil {
		return resp, fmt.Errorf("sending request: %w", err)
	}
	raw, err := readFrame(conn)
	if err != nil {
		return resp, err
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return resp, fmt.Errorf("decoding reply: %w", err)
	}
	return resp, nil
}

// activeChecks asks which items the server wants for this host. The
// request is also what registers the host: an unknown host with matching
// metadata is created by the server's autoregistration action.
func (c *client) activeChecks(ctx context.Context) ([]activeItem, error) {
	req := map[string]interface{}{
		"request":       "active checks",
		"host":          c.cfg.Hostname,
		"host_metadata": metadataWithPlatform(c.cfg.HostMetadata),
	}
	if c.cfg.Passive.Enabled {
		// The port the autoregistration action writes on the host's
		// agent interface, so the server polls where the listener is.
		req["port"] = c.cfg.Passive.Port
	}
	resp, err := c.exchange(ctx, req)
	if err != nil {
		return nil, err
	}
	if resp.Response != "success" {
		if strings.Contains(resp.Info, "not found") {
			return nil, fmt.Errorf("%w: %s", errHostUnknown, resp.Info)
		}
		return nil, fmt.Errorf("server refused the check list: %s", firstNonEmpty(resp.Info, resp.Response))
	}
	return resp.Data, nil
}

// errHostUnknown is the server's answer while the host does not exist
// yet; the request that got it is what triggers the autoregistration.
var errHostUnknown = errors.New("host not known to the server")

// sendValues pushes one batch and reports what the server processed.
func (c *client) sendValues(ctx context.Context, items []item, now time.Time) (pushResult, error) {
	var res pushResult
	if len(items) == 0 {
		return res, nil
	}
	values := make([]agentValue, len(items))
	for i, it := range items {
		values[i] = agentValue{
			Host:  c.cfg.Hostname,
			Key:   it.Key,
			Value: it.Value,
			ID:    c.nextID.Add(1),
			Clock: now.Unix(),
			NS:    int64(now.Nanosecond()),
		}
	}
	resp, err := c.exchange(ctx, map[string]interface{}{
		"request": "agent data",
		"session": c.session,
		"host":    c.cfg.Hostname,
		"data":    values,
		"clock":   now.Unix(),
		"ns":      now.Nanosecond(),
	})
	if err != nil {
		return res, err
	}
	if resp.Response != "success" {
		return res, fmt.Errorf("server refused the batch: %s", firstNonEmpty(resp.Info, resp.Response))
	}
	res = parseInfo(resp.Info)
	return res, nil
}

// heartbeat tells the server the host is alive between two pushes. The
// server does not answer a heartbeat, it closes the connection; only an
// explicit refusal (a server before 6.2 answering "failed") is an error,
// and the caller then stops sending it.
func (c *client) heartbeat(ctx context.Context) error {
	resp, err := c.exchange(ctx, map[string]interface{}{
		"request":        "active check heartbeat",
		"host":           c.cfg.Hostname,
		"heartbeat_freq": int(c.cfg.HeartbeatInterval.Seconds()),
	})
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		return nil
	}
	if err != nil {
		return err
	}
	if resp.Response != "" && resp.Response != "success" {
		return fmt.Errorf("heartbeat refused: %s", firstNonEmpty(resp.Info, resp.Response))
	}
	return nil
}

var infoPattern = regexp.MustCompile(`processed: (\d+); failed: (\d+); total: (\d+)`)

func parseInfo(info string) pushResult {
	m := infoPattern.FindStringSubmatch(info)
	if m == nil {
		return pushResult{}
	}
	p, _ := strconv.Atoi(m[1])
	f, _ := strconv.Atoi(m[2])
	t, _ := strconv.Atoi(m[3])
	return pushResult{Processed: p, Failed: f, Total: t}
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// metadataWithPlatform appends the agent's operating system to the host
// metadata. The autoregistration action matches on it to link the
// template set that platform can actually feed: a Windows performance
// counter declared on a Linux host can never receive a value, and an
// operator reads that empty line as a defect. Appending rather than
// replacing keeps whatever the operator wrote matchable as before.
func metadataWithPlatform(metadata string) string {
	metadata = strings.TrimSpace(metadata)
	if metadata == "" {
		return runtime.GOOS
	}
	return metadata + " " + runtime.GOOS
}

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
	"strconv"
	"strings"
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
	dial    func(ctx context.Context) (net.Conn, error)
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
	if t.ServerName != "" {
		conf.ServerName = t.ServerName
	} else if host, _, err := net.SplitHostPort(server); err == nil {
		conf.ServerName = host
	}
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

func (c *client) dialServer(ctx context.Context) (net.Conn, error) {
	d := net.Dialer{Timeout: c.cfg.Timeout}
	conn, err := d.DialContext(ctx, "tcp", c.cfg.Server)
	if err != nil {
		return nil, err
	}
	if c.tlsConf == nil {
		return conn, nil
	}
	tc := tls.Client(conn, c.tlsConf)
	if err := tc.HandshakeContext(ctx); err != nil {
		conn.Close()
		return nil, err
	}
	return tc, nil
}

// exchange sends one request and reads the reply.
func (c *client) exchange(ctx context.Context, req interface{}) (response, error) {
	var resp response
	payload, err := json.Marshal(req)
	if err != nil {
		return resp, fmt.Errorf("encoding request: %w", err)
	}
	ctx, cancel := context.WithTimeout(ctx, c.cfg.Timeout)
	defer cancel()

	conn, err := c.dial(ctx)
	if err != nil {
		return resp, fmt.Errorf("connecting to %s: %w", c.cfg.Server, err)
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
		"host_metadata": c.cfg.HostMetadata,
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

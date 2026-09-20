package zabbix

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"

	"senhub-agent.go/internal/agent/cliArgs"
	"senhub-agent.go/internal/agent/services/logger"
)

// The passive listener answers the server's polls, one item per
// connection. Two wire dialects exist: before 7.0 the server sends the
// bare key and reads a framed value; from 7.0 it first tries a framed
// JSON batch ({"request":"passive checks"}) and falls back to the old
// dialect when the agent answers ZBX_NOTSUPPORTED. Both are served, so
// no server ever falls back.
//
// The listener answers agent.ping (the availability icon), agent.version
// and agent.hostname, and any key the active push would send, so an
// operator may poll instead of receiving.

const (
	notSupported      = "ZBX_NOTSUPPORTED"
	unsupportedKey    = "Unsupported item key."
	passiveDeadline   = 5 * time.Second
	advertisedAgent   = "6.0.0"
	advertisedVariant = 2
)

// itemLookup resolves one key to its current value.
type itemLookup func(key string) (string, bool)

type passiveListener struct {
	cfg      PassiveConfig
	hostname string
	lookup   itemLookup
	logger   *logger.ModuleLogger
	allowed  []*net.IPNet

	ln net.Listener
	wg sync.WaitGroup
}

func newPassiveListener(cfg Config, lookup itemLookup, log *logger.ModuleLogger) (*passiveListener, error) {
	allowed, err := allowedNets(cfg)
	if err != nil {
		return nil, err
	}
	return &passiveListener{cfg: cfg.Passive, hostname: cfg.Hostname, lookup: lookup, logger: log, allowed: allowed}, nil
}

// allowedNets turns the allow list into networks; an empty list means
// the addresses the configured server resolves to.
func allowedNets(cfg Config) ([]*net.IPNet, error) {
	entries := cfg.Passive.Allow
	if len(entries) == 0 {
		host, _, err := net.SplitHostPort(cfg.Server)
		if err != nil {
			return nil, fmt.Errorf("zabbix: passive: cannot read the server host from %q", cfg.Server)
		}
		ips, err := net.LookupIP(host)
		if err != nil {
			return nil, fmt.Errorf("zabbix: passive: resolving %s to build the allow list: %w", host, err)
		}
		for _, ip := range ips {
			entries = append(entries, ip.String())
		}
	}
	nets := make([]*net.IPNet, 0, len(entries))
	for _, e := range entries {
		if _, n, err := net.ParseCIDR(e); err == nil {
			nets = append(nets, n)
			continue
		}
		ip := net.ParseIP(e)
		if ip == nil {
			return nil, fmt.Errorf("zabbix: passive: %q is neither an IP address nor a CIDR range", e)
		}
		bits := 32
		if ip.To4() == nil {
			bits = 128
		}
		nets = append(nets, &net.IPNet{IP: ip, Mask: net.CIDRMask(bits, bits)})
	}
	return nets, nil
}

func (p *passiveListener) allows(addr net.Addr) bool {
	host, _, err := net.SplitHostPort(addr.String())
	if err != nil {
		return false
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}
	for _, n := range p.allowed {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}

func (p *passiveListener) start(ctx context.Context) error {
	addr := net.JoinHostPort(p.cfg.BindAddress, fmt.Sprint(p.cfg.Port))
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("zabbix: passive: listening on %s: %w", addr, err)
	}
	p.ln = ln
	p.wg.Add(1)
	go p.serve(ctx)
	return nil
}

func (p *passiveListener) addr() string {
	if p.ln == nil {
		return ""
	}
	return p.ln.Addr().String()
}

func (p *passiveListener) stop() {
	if p.ln != nil {
		p.ln.Close()
	}
	p.wg.Wait()
}

func (p *passiveListener) serve(ctx context.Context) {
	defer p.wg.Done()
	for {
		conn, err := p.ln.Accept()
		if err != nil {
			return
		}
		if ctx.Err() != nil {
			conn.Close()
			return
		}
		if !p.allows(conn.RemoteAddr()) {
			p.logger.Debug().Str("from", conn.RemoteAddr().String()).Msg("Passive poll refused: address not allowed")
			conn.Close()
			continue
		}
		p.wg.Add(1)
		go func() {
			defer p.wg.Done()
			defer conn.Close()
			p.handle(conn)
		}()
	}
}

// handle answers one poll. It peeks at the first bytes to tell the
// framed JSON dialect from the bare key.
func (p *passiveListener) handle(conn net.Conn) {
	_ = conn.SetDeadline(time.Now().Add(passiveDeadline))
	r := bufio.NewReader(conn)
	head, err := r.Peek(4)
	if err != nil {
		return
	}
	if string(head) == frameMagic {
		p.handleJSON(conn, r)
		return
	}
	line, err := r.ReadString('\n')
	if err != nil && line == "" {
		return
	}
	key := strings.TrimRight(line, "\r\n")
	value, ok := p.resolve(key)
	if !ok {
		_ = writeFrame(conn, []byte(notSupported+"\x00"+unsupportedKey))
		return
	}
	_ = writeFrame(conn, []byte(value))
}

type passiveRequest struct {
	Request string `json:"request"`
	Data    []struct {
		Key string `json:"key"`
	} `json:"data"`
}

type passiveReply struct {
	Version string        `json:"version"`
	Variant int           `json:"variant"`
	Data    []passiveItem `json:"data"`
}

type passiveItem struct {
	Value string `json:"value,omitempty"`
	Error string `json:"error,omitempty"`
}

func (p *passiveListener) handleJSON(conn net.Conn, r *bufio.Reader) {
	payload, err := readFrame(r)
	if err != nil {
		return
	}
	var req passiveRequest
	if err := json.Unmarshal(payload, &req); err != nil || req.Request != "passive checks" {
		_ = writeFrame(conn, []byte(notSupported+"\x00"+"Unsupported request."))
		return
	}
	reply := passiveReply{Version: advertisedAgent, Variant: advertisedVariant}
	for _, d := range req.Data {
		if value, ok := p.resolve(d.Key); ok {
			reply.Data = append(reply.Data, passiveItem{Value: value})
		} else {
			reply.Data = append(reply.Data, passiveItem{Error: unsupportedKey})
		}
	}
	body, err := json.Marshal(reply)
	if err != nil {
		return
	}
	_ = writeFrame(conn, body)
}

func (p *passiveListener) resolve(key string) (string, bool) {
	switch key {
	case "agent.ping":
		return "1", true
	case "agent.version":
		if cliArgs.Version == "" {
			return "0.0.0-dev", true
		}
		return cliArgs.Version, true
	case "agent.hostname":
		return p.hostname, true
	}
	if p.lookup == nil {
		return "", false
	}
	return p.lookup(key)
}

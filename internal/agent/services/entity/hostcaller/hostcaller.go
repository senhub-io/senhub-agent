// Package hostcaller reports who connects to this host's listeners, as OTLP
// log records and one metric. It is deliberately outside the entity rail:
// callers are workstations, clients and external addresses, which belong in a
// log store and not in the topology, where only flows between applications
// have a place.
//
// The rows come from the socket read hostdep already does: an ESTABLISHED row
// whose local port is in the LISTEN set is inbound. The tracker keeps, per
// listener and per caller address, the same debounce position hostdep keeps
// per peer endpoint (hits needed to appear, the same miss tolerance to go
// away), and emits:
//
//   - senhub.listener.caller.seen once the caller has been observed on
//     `threshold` scrapes, timestamped at its first observation;
//   - senhub.listener.caller.gone once it has been missing past the miss
//     tolerance, timestamped at its last observation;
//   - senhub.listener.caller.activity, one per caller per window.
//
// At most MaxCallersPerListener callers per listener hold a place. The set is
// frozen once full; a place freed by a caller that went away is given to the
// next candidate by first appearance, then by address. The others are counted
// in the over_cap class of the metric and get no record.
package hostcaller

import (
	"net/netip"
	"sort"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	gnet "github.com/shirou/gopsutil/v3/net"

	"senhub-agent.go/internal/agent/services/agentstate"
	"senhub-agent.go/internal/agent/services/entity"
	"senhub-agent.go/internal/agent/services/entity/hostdep"
	"senhub-agent.go/internal/agent/services/exporthook"
)

// PublicMode says what leaves the host about a caller whose address is public.
type PublicMode string

const (
	// PublicClear reports the address as it is (default).
	PublicClear PublicMode = "clear"
	// PublicTruncated reports the /24 (IPv4) or /48 (IPv6) the address is in,
	// in senhub.listener.caller.address.prefix, and no client.address.
	PublicTruncated PublicMode = "truncated"
	// PublicCount only counts the address, in the public class of the metric.
	PublicCount PublicMode = "count"
)

// ParsePublicMode reads the operator's value. An empty value is the default;
// a value that names no mode reads as count, the one that sends nothing: a
// typo in a privacy setting must not publish what it was meant to hold back.
func ParsePublicMode(v string) PublicMode {
	switch PublicMode(v) {
	case "", PublicClear:
		return PublicClear
	case PublicTruncated:
		return PublicTruncated
	default:
		return PublicCount
	}
}

const (
	// MaxCallersPerListener bounds the callers that get records per listener.
	MaxCallersPerListener = 32
	// maxPendingFactor bounds, per listener, the callers followed while
	// they wait for the debounce or for a place, as a multiple of the cap.
	maxPendingFactor = 8
	// ActivityWindow is the cadence of senhub.listener.caller.activity.
	ActivityWindow = 15 * time.Minute

	transportTCP = "tcp"

	EventSeen     = "senhub.listener.caller.seen"
	EventGone     = "senhub.listener.caller.gone"
	EventActivity = "senhub.listener.caller.activity"

	MetricCount = "senhub.listener.caller.count"

	ClassEmitted = "emitted"
	ClassOverCap = "over_cap"
	ClassPublic  = "public"
	ClassLocal   = "local"

	attrEndpoint    = "service.endpoint"
	attrTransport   = "network.transport"
	attrServerPort  = "server.port"
	attrClient      = "client.address"
	attrNetworkType = "network.type"
	attrPrefix      = "senhub.listener.caller.address.prefix"
	attrConnCount   = "senhub.listener.caller.connection.count"
	attrObserved    = "senhub.listener.caller.connection.observed"
	attrMax         = "senhub.listener.caller.connection.max"
	attrWindow      = "senhub.listener.caller.window.duration"
	attrPresence    = "senhub.listener.caller.presence.duration"
	attrClass       = "senhub.listener.caller.class"
)

// Config is what a tracker needs.
type Config struct {
	// Threshold is the debounce, entities.depends_on.debounce.
	Threshold int
	Public    PublicMode
	// Publish takes the records; nil means agentstate.PublishLog.
	Publish func(agentstate.LogRecord)
	// Interfaces lists the host interfaces; nil means gnet.Interfaces.
	Interfaces func() (gnet.InterfaceStatList, error)
	// OnInterfaceError is called once, the first time the interface list
	// cannot be read. Callers are then classified by address alone.
	OnInterfaceError func(error)
	// Cap and Window override the package defaults; zero keeps them.
	Cap    int
	Window time.Duration
}

type connKey struct {
	localIP string
	port    uint32
}

type caller struct {
	first, last time.Time
	hits        int
	misses      int
	asserted    bool

	winConns map[connKey]struct{}
	winMax   int
	winLast  int
}

type listener struct {
	port    uint32
	tracked map[netip.Addr]*caller
}

type classCounts struct{ emitted, overCap, public, local int }

// Tracker follows the callers of every listener of the host.
type Tracker struct {
	threshold  int
	forget     int
	public     PublicMode
	publish    func(agentstate.LogRecord)
	interfaces func() (gnet.InterfaceStatList, error)
	onIfaceErr func(error)
	ifaceWarn  sync.Once
	maxCallers int
	window     time.Duration

	mu        sync.Mutex
	hostID    string
	listeners map[uint32]*listener
	lastRead  time.Time
	winStart  time.Time
	winEnd    time.Time
	counts    map[uint32]classCounts
}

// New builds a tracker.
func New(cfg Config) *Tracker {
	t := &Tracker{
		threshold:  cfg.Threshold,
		public:     cfg.Public,
		publish:    cfg.Publish,
		interfaces: cfg.Interfaces,
		onIfaceErr: cfg.OnInterfaceError,
		maxCallers: cfg.Cap,
		window:     cfg.Window,
		listeners:  map[uint32]*listener{},
		counts:     map[uint32]classCounts{},
	}
	if t.threshold < 1 {
		t.threshold = hostdep.DefaultThreshold
	}
	t.forget = hostdep.ForgetAfter(t.threshold)
	if t.public == "" {
		t.public = PublicClear
	}
	if t.publish == nil {
		t.publish = agentstate.PublishLog
	}
	if t.interfaces == nil {
		t.interfaces = gnet.Interfaces
	}
	if t.maxCallers < 1 {
		t.maxCallers = MaxCallersPerListener
	}
	if t.window <= 0 {
		t.window = ActivityWindow
	}
	return t
}

type presence struct {
	count int
	conns map[connKey]struct{}
	local bool
}

// Observe folds one successful socket read into the tracker. It is the
// hostdep.InboundSink: a failed read never reaches it, so a transient error
// does not count as a miss.
func (t *Tracker) Observe(hostID string, rows []hostdep.InboundRow, now time.Time) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.hostID = hostID

	t.rollWindow(now)
	t.lastRead = now

	var ifc *ifaceView
	bridged := func(addr netip.Addr, localIP string) bool {
		if ifc == nil {
			var err error
			ifc, err = newIfaceView(t.interfaces)
			if err != nil && t.onIfaceErr != nil {
				t.ifaceWarn.Do(func() { t.onIfaceErr(err) })
			}
		}
		return ifc.bridged(addr, localIP) || ifc.own(addr)
	}

	byPort := map[uint32]map[netip.Addr]*presence{}
	for _, r := range rows {
		addr, ok := parseAddr(r.CallerIP)
		if !ok {
			continue
		}
		perAddr := byPort[r.LocalPort]
		if perAddr == nil {
			perAddr = map[netip.Addr]*presence{}
			byPort[r.LocalPort] = perAddr
		}
		p := perAddr[addr]
		if p == nil {
			p = &presence{conns: map[connKey]struct{}{}}
			perAddr[addr] = p
		}
		p.count++
		p.conns[connKey{localIP: r.LocalIP, port: r.CallerPort}] = struct{}{}
		if !p.local && (isLocalCaller(addr) || bridged(addr, r.LocalIP)) {
			p.local = true
		}
	}
	for port := range byPort {
		if _, ok := t.listeners[port]; !ok {
			t.listeners[port] = &listener{port: port, tracked: map[netip.Addr]*caller{}}
		}
	}

	counts := map[uint32]classCounts{}
	ports := make([]uint32, 0, len(t.listeners))
	for port := range t.listeners {
		ports = append(ports, port)
	}
	sort.Slice(ports, func(i, j int) bool { return ports[i] < ports[j] })
	for _, port := range ports {
		l := t.listeners[port]
		c := t.observeListener(l, byPort[port], now)
		if len(l.tracked) == 0 && c == (classCounts{}) {
			delete(t.listeners, port)
			continue
		}
		counts[port] = c
	}
	t.counts = counts
}

func (t *Tracker) observeListener(l *listener, present map[netip.Addr]*presence, now time.Time) classCounts {
	var counts classCounts
	loggable := map[netip.Addr]*presence{}
	for addr, p := range present {
		switch {
		case p.local:
			counts.local++
		case t.public == PublicCount && isPublic(addr):
			counts.public++
		default:
			loggable[addr] = p
		}
	}

	tracked := sortedAddrs(l.tracked)
	for _, addr := range tracked {
		c := l.tracked[addr]
		if p, ok := loggable[addr]; ok {
			c.hits++
			if c.hits > t.threshold {
				c.hits = t.threshold
			}
			c.misses = 0
			c.last = now
			c.record(p)
			continue
		}
		c.misses++
		if c.misses > t.forget {
			delete(l.tracked, addr)
			if c.asserted {
				t.emit(EventGone, l, addr, c.last, now, 0, map[string]any{
					attrPresence: c.last.Sub(c.first).Seconds(),
				})
			}
		}
	}

	// A new caller starts pending and holds no place: places are granted on
	// confirmation only, so a burst of one-shot callers cannot fill them. The
	// pending set is bounded so a scan cannot grow it without limit.
	for _, addr := range sortedPresence(loggable) {
		if _, held := l.tracked[addr]; held {
			continue
		}
		if len(l.tracked) >= maxPendingFactor*t.maxCallers {
			continue
		}
		c := &caller{first: now, last: now, hits: 1}
		c.record(loggable[addr])
		l.tracked[addr] = c
	}

	// Confirmation: the callers that reached the threshold, earliest first
	// appearance first, then by address, up to the places left.
	confirmed := 0
	var ready []netip.Addr
	for addr, c := range l.tracked {
		switch {
		case c.asserted:
			confirmed++
		case c.hits >= t.threshold && loggable[addr] != nil:
			ready = append(ready, addr)
		}
	}
	sort.Slice(ready, func(i, j int) bool {
		a, b := l.tracked[ready[i]], l.tracked[ready[j]]
		if !a.first.Equal(b.first) {
			return a.first.Before(b.first)
		}
		return ready[i].Compare(ready[j]) < 0
	})
	for _, addr := range ready {
		if confirmed >= t.maxCallers {
			break
		}
		confirmed++
		c := l.tracked[addr]
		c.asserted = true
		t.emit(EventSeen, l, addr, c.first, now, loggable[addr].count, nil)
	}

	// Classes: a confirmed caller is emitted. A pending one is emitted while
	// a place remains for it, over_cap once none does; one the bounded set
	// could not hold is over_cap.
	free := t.maxCallers - confirmed
	var pending []netip.Addr
	for addr := range loggable {
		c, held := l.tracked[addr]
		switch {
		case held && c.asserted:
			counts.emitted++
		case held:
			pending = append(pending, addr)
		default:
			counts.overCap++
		}
	}
	sort.Slice(pending, func(i, j int) bool {
		a, b := l.tracked[pending[i]], l.tracked[pending[j]]
		if !a.first.Equal(b.first) {
			return a.first.Before(b.first)
		}
		return pending[i].Compare(pending[j]) < 0
	})
	for range pending {
		if free > 0 {
			free--
			counts.emitted++
		} else {
			counts.overCap++
		}
	}
	return counts
}

func sortedPresence(m map[netip.Addr]*presence) []netip.Addr {
	out := make([]netip.Addr, 0, len(m))
	for a := range m {
		out = append(out, a)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Compare(out[j]) < 0 })
	return out
}

func (c *caller) record(p *presence) {
	if c.winConns == nil {
		c.winConns = map[connKey]struct{}{}
	}
	for k := range p.conns {
		c.winConns[k] = struct{}{}
	}
	if p.count > c.winMax {
		c.winMax = p.count
	}
	c.winLast = p.count
}

// rollWindow closes the activity window when the read is past its end.
func (t *Tracker) rollWindow(now time.Time) {
	if t.winEnd.IsZero() {
		t.winStart = now
		t.winEnd = now.Truncate(t.window).Add(t.window)
		return
	}
	if now.Before(t.winEnd) {
		return
	}
	// After a gap of more than a window between two reads, the data ends at
	// the last read, not at the window boundary: report the span actually
	// covered instead of claiming a full window.
	end := t.winEnd
	if !now.Before(t.winEnd.Add(t.window)) && t.lastRead.After(t.winStart) {
		end = t.lastRead
	}
	dur := end.Sub(t.winStart).Seconds()
	ports := make([]uint32, 0, len(t.listeners))
	for port := range t.listeners {
		ports = append(ports, port)
	}
	sort.Slice(ports, func(i, j int) bool { return ports[i] < ports[j] })
	for _, port := range ports {
		l := t.listeners[port]
		for _, addr := range sortedAddrs(l.tracked) {
			c := l.tracked[addr]
			if c.asserted && len(c.winConns) > 0 {
				t.emit(EventActivity, l, addr, end, now, c.winLast, map[string]any{
					attrObserved: int64(len(c.winConns)),
					attrMax:      int64(c.winMax),
					attrWindow:   dur,
				})
			}
			c.winConns, c.winMax, c.winLast = nil, 0, 0
		}
	}
	t.winStart = now.Truncate(t.window)
	t.winEnd = t.winStart.Add(t.window)
}

func (t *Tracker) emit(event string, l *listener, addr netip.Addr, at, observed time.Time, connCount int, extra map[string]any) {
	endpoint := entity.ListenerEndpoint(t.hostID, int(l.port), transportTCP)
	attrs := map[string]string{
		attrEndpoint:  endpoint,
		attrTransport: transportTCP,
	}
	typed := map[string]any{
		attrServerPort: int64(l.port),
		attrConnCount:  int64(connCount),
	}
	if isPublic(addr) && t.public == PublicTruncated {
		attrs[attrPrefix] = truncate(addr)
	} else {
		attrs[attrClient] = addr.String()
	}
	if addr.Is4() {
		attrs[attrNetworkType] = "ipv4"
	} else {
		attrs[attrNetworkType] = "ipv6"
	}
	for k, v := range extra {
		typed[k] = v
	}
	for k, v := range typed {
		switch n := v.(type) {
		case int64:
			attrs[k] = strconv.FormatInt(n, 10)
		case float64:
			attrs[k] = strconv.FormatFloat(n, 'f', -1, 64)
		}
	}
	t.publish(agentstate.LogRecord{
		Timestamp:         at,
		ObservedTimestamp: observed,
		Severity:          agentstate.LogSeverityInfo,
		SeverityText:      "INFO",
		Body:              event,
		EventName:         event,
		Attributes:        attrs,
		TypedAttributes:   typed,
		ProducerProbeName: "senhub-agent",
		ProducerProbeType: "agent",
		TargetStrategies:  []string{"otlp"},
	})
}

// SelfRecords is the metric: the distinct caller addresses of the last read,
// per listener and class. The four classes partition the addresses.
func (t *Tracker) SelfRecords() []exporthook.SelfRecord {
	t.mu.Lock()
	defer t.mu.Unlock()
	ports := make([]uint32, 0, len(t.counts))
	for port := range t.counts {
		ports = append(ports, port)
	}
	sort.Slice(ports, func(i, j int) bool { return ports[i] < ports[j] })
	var out []exporthook.SelfRecord
	for _, port := range ports {
		c := t.counts[port]
		endpoint := entity.ListenerEndpoint(t.hostID, int(port), transportTCP)
		for _, cl := range []struct {
			name string
			n    int
		}{{ClassEmitted, c.emitted}, {ClassOverCap, c.overCap}, {ClassPublic, c.public}, {ClassLocal, c.local}} {
			out = append(out, exporthook.SelfRecord{
				Name:        MetricCount,
				Unit:        "{address}",
				Type:        "gauge",
				Description: "Distinct caller addresses of a local listener at the last socket read, by class: emitted (has log records), over_cap (beyond the per-listener cap), public (counted only, by setting), local (host-local, never reported).",
				Value:       float64(cl.n),
				Attributes: map[string]string{
					attrEndpoint:  endpoint,
					attrTransport: transportTCP,
					attrClass:     cl.name,
				},
			})
		}
	}
	return out
}

var (
	active     atomic.Pointer[Tracker]
	registerMu sync.Once
)

// Install makes t the tracker whose counts the agent exports as a metric and
// returns the function that removes it. Providers cannot be unregistered, so
// one is registered for the process and reads the installed tracker.
func Install(t *Tracker) (uninstall func()) {
	registerMu.Do(func() {
		exporthook.RegisterSelfRecordProvider(func(time.Time) []exporthook.SelfRecord {
			if cur := active.Load(); cur != nil {
				return cur.SelfRecords()
			}
			return nil
		})
	})
	active.Store(t)
	return func() { active.CompareAndSwap(t, nil) }
}

func sortedAddrs(m map[netip.Addr]*caller) []netip.Addr {
	out := make([]netip.Addr, 0, len(m))
	for a := range m {
		out = append(out, a)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Compare(out[j]) < 0 })
	return out
}

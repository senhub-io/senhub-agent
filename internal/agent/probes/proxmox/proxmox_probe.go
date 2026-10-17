package proxmox

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sync/atomic"
	"time"

	"senhub-agent.go/internal/agent/probes/dbcommon"
	"senhub-agent.go/internal/agent/probes/types"
	"senhub-agent.go/internal/agent/services/data_store"
	"senhub-agent.go/internal/agent/services/logger"
	"senhub-agent.go/internal/agent/tags"
)

// ProbeType is the stable technical identifier used in YAML config, license
// claims, transformer file names, and discriminant-tag keys.
const ProbeType = "proxmox"

const (
	defaultInterval = 60 * time.Second
	defaultTimeout  = 15 * time.Second
	apiBase         = "/api2/json"
)

// ProxmoxProbe collects Proxmox VE metrics for nodes, VMs, containers,
// and storage via the cluster REST API.
type ProxmoxProbe struct {
	*types.BaseProbe
	cfg          probeConfig
	moduleLogger *logger.ModuleLogger
	client       *http.Client
	entitySrc    *proxmoxEntitySource

	// resourcesDegraded is true while /cluster/resources is failing and the
	// per-node lists are used instead; it only exists to log each transition once.
	resourcesDegraded atomic.Bool

	denied deniedLog
}

type probeConfig struct {
	Endpoint     string
	TokenID      string
	TokenSecret  string
	VerifyTLS    bool
	Node         string // empty = all nodes
	InstanceName string // optional operator-assigned stable id for the PVE surface entity
	Interval     time.Duration
	Timeout      time.Duration
}

// NewProxmoxProbe constructs the probe. Config errors are returned immediately
// so the agent marks the probe unhealthy at startup rather than failing silently.
func NewProxmoxProbe(config map[string]interface{}, baseLogger *logger.Logger) (types.Probe, error) {
	moduleLogger := logger.NewModuleLogger(baseLogger, "probe.proxmox")

	cfg, err := parseConfig(config)
	if err != nil {
		return nil, err
	}

	transport := &http.Transport{
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: !cfg.VerifyTLS, // #nosec G402 - operator opt-in for self-signed PVE installs
		},
	}
	client := &http.Client{
		Timeout:   cfg.Timeout,
		Transport: transport,
	}

	p := &ProxmoxProbe{
		BaseProbe:    &types.BaseProbe{},
		cfg:          cfg,
		moduleLogger: moduleLogger,
		client:       client,
	}
	p.SetProbeType(ProbeType)

	p.entitySrc = newProxmoxEntitySource(cfg, moduleLogger)
	p.SetEntitySource(p.entitySrc)

	return p, nil
}

func parseConfig(config map[string]interface{}) (probeConfig, error) {
	cfg := probeConfig{
		VerifyTLS: true,
		Interval:  defaultInterval,
		Timeout:   defaultTimeout,
	}

	endpoint, _ := config["endpoint"].(string)
	if endpoint == "" {
		return cfg, fmt.Errorf("proxmox: endpoint is required")
	}
	cfg.Endpoint = endpoint

	tokenID, _ := config["token_id"].(string)
	if tokenID == "" {
		return cfg, fmt.Errorf("proxmox: token_id is required (user@realm!tokenname)")
	}
	cfg.TokenID = tokenID

	tokenSecret, _ := config["token_secret"].(string)
	if tokenSecret == "" {
		return cfg, fmt.Errorf("proxmox: token_secret is required")
	}
	cfg.TokenSecret = tokenSecret

	if v, ok := config["verify_tls"].(bool); ok {
		cfg.VerifyTLS = v
	}
	if v, ok := config["node"].(string); ok {
		cfg.Node = v
	}
	if v, ok := config["instance_name"].(string); ok {
		cfg.InstanceName = v
	}
	if v, ok := types.IntParam(config, "interval"); ok && v > 0 {
		cfg.Interval = time.Duration(v) * time.Second
	}
	if v, ok := types.IntParam(config, "timeout"); ok && v > 0 {
		cfg.Timeout = time.Duration(v) * time.Second
	}
	return cfg, nil
}

func (p *ProxmoxProbe) GetInterval() time.Duration { return p.cfg.Interval }
func (p *ProxmoxProbe) ShouldStart() bool          { return true }

func (p *ProxmoxProbe) OnStart(_ chan struct{}) error {
	p.moduleLogger.Info().
		Str("endpoint", p.cfg.Endpoint).
		Str("node_filter", p.cfg.Node).
		Msg("starting proxmox probe")

	return nil
}

func (p *ProxmoxProbe) OnShutdown(_ context.Context) error {
	p.client.CloseIdleConnections()
	return nil
}

// entityIDTags builds the probe's base identity tags. The value comes from the
// entity source accessor so tags always match the emitted entity ID. It is nil
// when the entity source has not pinned an ID yet.
func (p *ProxmoxProbe) entityIDTags() []tags.Tag {
	if id := p.entitySrc.InstanceID(); id != "" {
		return []tags.Tag{{Key: "service.instance.id", Value: id}}
	}
	return nil
}

// checkResultPoint emits exactly one check-result datapoint per collection,
// carrying the probe's identity tags plus metric_type=status.
func (p *ProxmoxProbe) checkResultPoint(err error, now time.Time, identityTags []tags.Tag) data_store.DataPoint {
	t := make([]tags.Tag, 0, len(identityTags)+1)
	t = append(t, identityTags...)
	t = append(t, tags.Tag{Key: "metric_type", Value: "status"})
	return data_store.DataPoint{
		Name:      dbcommon.CheckResultMetric,
		Value:     float64(dbcommon.ClassifyError(err)),
		Timestamp: now,
		Tags:      t,
	}
}

// Collect fetches one metrics cycle from the Proxmox API and returns
// datapoints for all nodes (and their VMs/containers/storage) that match
// the optional node filter. A failure on one node logs a warning and
// continues so a single unreachable node does not blank all metrics.
//
// senhub.proxmox.up is always emitted: 0 before the first successful API
// call, 1 after a successful node list. If the list fails the probe logs a
// warning and returns the up=0 point non-fatally so the series stays visible
// and distinguishable from an empty-but-healthy cluster.
func (p *ProxmoxProbe) Collect() ([]data_store.DataPoint, error) {
	now := time.Now()

	upTags := []tags.Tag{
		{Key: "metric_type", Value: "availability"},
	}
	upPoint := data_store.DataPoint{Name: "senhub.proxmox.up", Value: 0, Timestamp: now, Tags: upTags}

	nodes, err := p.fetchNodes()
	if err != nil {
		p.moduleLogger.Warn().Err(err).Msg("proxmox: listing nodes failed")
		identityTags := p.entityIDTags()
		upPoint.Tags = append(upPoint.Tags, identityTags...)
		points := []data_store.DataPoint{upPoint, p.checkResultPoint(err, now, identityTags)}
		return p.BaseProbe.EnrichDataPointsWithProbeName(points, p.GetName()), nil
	}

	upPoint.Value = 1
	points := []data_store.DataPoint{upPoint}

	guests, guestsOK := p.fetchGuests()

	for _, n := range nodes {
		if p.cfg.Node != "" && n.Node != p.cfg.Node {
			continue
		}
		points = append(points, p.collectNode(n, guests, guestsOK, now)...)
	}

	// Refresh entity snapshot so the topology rail stays current.
	// fetchClusterName is best-effort: a standalone install or a failing
	// cluster/status call returns "" and the entity falls back to the
	// agent machine-id. The cluster name is stable once set so a one-off
	// failure here does not flip the identity.
	clusterName, _ := p.fetchClusterName()
	p.entitySrc.refresh(clusterName)

	identityTags := p.entityIDTags()
	for i := range points {
		points[i].Tags = append(points[i].Tags, identityTags...)
	}
	points = append(points, p.checkResultPoint(nil, now, identityTags))

	return p.BaseProbe.EnrichDataPointsWithProbeName(points, p.GetName()), nil
}

// collectNode gathers node-level, VM, LXC, and storage metrics.
// Partial failures (e.g. storage unavailable) are logged but non-fatal.
func (p *ProxmoxProbe) collectNode(n pveNode, guests []pveGuest, guestsOK bool, now time.Time) []data_store.DataPoint {
	var points []data_store.DataPoint

	nodeTags := []tags.Tag{
		{Key: "proxmox.node", Value: n.Node},
		{Key: "metric_type", Value: "node"},
	}

	// Node status.
	online := float64(0)
	if n.Status == "online" {
		online = 1
	}
	points = append(points,
		data_store.DataPoint{Name: "proxmox.node.status", Value: online, Timestamp: now, Tags: nodeTags},
	)

	// Detailed node metrics (CPU/memory) via /nodes/{node}/status.
	if ns, err := p.fetchNodeStatus(n.Node); err == nil {
		cpu := float64(ns.CPU) * 100
		points = append(points,
			data_store.DataPoint{Name: "proxmox.node.cpu.utilization", Value: cpu, Timestamp: now, Tags: nodeTags},
			data_store.DataPoint{Name: "proxmox.node.memory.used", Value: float64(ns.Memory.Used), Timestamp: now, Tags: nodeTags},
			data_store.DataPoint{Name: "proxmox.node.memory.total", Value: float64(ns.Memory.Total), Timestamp: now, Tags: nodeTags},
		)
	} else {
		p.moduleLogger.Warn().Err(err).Str("node", n.Node).Msg("failed to fetch node status")
	}

	// Guests (QEMU and LXC). The cluster-wide listing is the primary source:
	// since PVE 9 the per-node qemu list no longer carries disk counters.
	if guestsOK {
		for _, g := range guests {
			if g.Node == n.Node {
				points = append(points, p.collectVMStatus(n.Node, g.pveVMListItem, g.Type, now)...)
			}
		}
	} else {
		vms, err := p.fetchVMs(n.Node)
		if err != nil {
			p.moduleLogger.Warn().Err(err).Str("node", n.Node).Msg("failed to list VMs")
		}
		for _, vm := range vms {
			points = append(points, p.collectVMStatus(n.Node, vm, "qemu", now)...)
		}
		ctrs, err := p.fetchContainers(n.Node)
		if err != nil {
			p.moduleLogger.Warn().Err(err).Str("node", n.Node).Msg("failed to list LXC containers")
		}
		for _, ctr := range ctrs {
			points = append(points, p.collectVMStatus(n.Node, ctr, "lxc", now)...)
		}
	}

	points = append(points, p.collectDisks(n.Node, now)...)

	// Storage.
	storages, err := p.fetchStorage(n.Node)
	if err != nil {
		p.moduleLogger.Warn().Err(err).Str("node", n.Node).Msg("failed to list storage")
	}
	for _, st := range storages {
		stTags := []tags.Tag{
			{Key: "proxmox.node", Value: n.Node},
			{Key: "proxmox.storage", Value: st.Storage},
			{Key: "metric_type", Value: "storage"},
		}
		points = append(points,
			data_store.DataPoint{Name: "proxmox.storage.used", Value: float64(st.Used), Timestamp: now, Tags: stTags},
			data_store.DataPoint{Name: "proxmox.storage.total", Value: float64(st.Total), Timestamp: now, Tags: stTags},
		)
	}

	return points
}

// collectVMStatus converts a VM/LXC status response into datapoints.
// vmType is "qemu" or "lxc".
func (p *ProxmoxProbe) collectVMStatus(node string, vm pveVMStatus, vmType string, now time.Time) []data_store.DataPoint {
	vmTags := []tags.Tag{
		{Key: "proxmox.node", Value: node},
		{Key: "proxmox.vmid", Value: fmt.Sprintf("%d", vm.VMID)},
		{Key: "proxmox.vm.name", Value: vm.Name},
		{Key: "proxmox.vm.type", Value: vmType},
		{Key: "metric_type", Value: "vm"},
	}

	var points []data_store.DataPoint
	add := func(name string, v *float64) {
		if v != nil {
			points = append(points, data_store.DataPoint{Name: name, Value: *v, Timestamp: now, Tags: vmTags})
		}
	}
	scale := func(v *float64, f float64) *float64 {
		if v == nil {
			return nil
		}
		r := *v * f
		return &r
	}
	var status *float64
	if vm.Status != "" {
		r := float64(0)
		if vm.Status == "running" {
			r = 1
		}
		status = &r
	}

	add("proxmox.vm.cpu.utilization", scale(vm.CPU, 100))
	add("proxmox.vm.memory.used", vm.Mem)
	add("proxmox.vm.memory.total", vm.MaxMem)
	add("proxmox.vm.disk.read", vm.DiskRead)
	add("proxmox.vm.disk.write", vm.DiskWrite)
	add("proxmox.vm.network.in", vm.NetIn)
	add("proxmox.vm.network.out", vm.NetOut)
	add("proxmox.vm.status", status)
	return points
}

// fetchGuests lists every QEMU and LXC guest of the cluster in one request.
// ok is false when the request failed, in which case the caller falls back
// to the per-node lists; the transition in and out of that state is logged
// once, not at every cycle.
func (p *ProxmoxProbe) fetchGuests() (guests []pveGuest, ok bool) {
	err := p.apiGet("/cluster/resources?"+url.Values{"type": {"vm"}}.Encode(), &guests)
	if err != nil {
		if p.resourcesDegraded.CompareAndSwap(false, true) {
			p.moduleLogger.Warn().Err(err).Msg("cluster resources unavailable, falling back to per-node guest lists (disk counters may be missing)")
		}
		return nil, false
	}
	if p.resourcesDegraded.CompareAndSwap(true, false) {
		p.moduleLogger.Info().Msg("cluster resources available again")
	}
	return guests, true
}

// --- API client helpers ---

func (p *ProxmoxProbe) apiGet(path string, out interface{}) error {
	url := p.cfg.Endpoint + apiBase + path
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("building request for %s: %w", path, err)
	}
	req.Header.Set("Authorization", fmt.Sprintf("PVEAPIToken=%s=%s", p.cfg.TokenID, p.cfg.TokenSecret))

	resp, err := p.client.Do(req)
	if err != nil {
		return fmt.Errorf("GET %s: %w", path, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return &apiStatusError{Path: path, Status: resp.StatusCode}
	}

	var envelope struct {
		Data json.RawMessage `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		return fmt.Errorf("decoding %s response: %w", path, err)
	}
	return json.Unmarshal(envelope.Data, out)
}

// apiStatusError reports a non-200 answer so callers can tell a missing
// permission (403) from a failing server.
type apiStatusError struct {
	Path   string
	Status int
}

func (e *apiStatusError) Error() string {
	return fmt.Sprintf("GET %s: unexpected status %d", e.Path, e.Status)
}

// fetchClusterName queries GET /cluster/status and returns the cluster name
// from the entry whose type is "cluster". Returns ("", nil) on a standalone
// install (no cluster entry) and ("", err) when the call itself fails.
func (p *ProxmoxProbe) fetchClusterName() (string, error) {
	var items []pveClusterStatusItem
	if err := p.apiGet("/cluster/status", &items); err != nil {
		return "", err
	}
	for _, item := range items {
		if item.Type == "cluster" {
			return item.Name, nil
		}
	}
	return "", nil
}

// --- API response types ---

type pveClusterStatusItem struct {
	Type string `json:"type"`
	Name string `json:"name"`
}

type pveNodeListItem struct {
	Node   string `json:"node"`
	Status string `json:"status"`
}

type pveNode struct {
	Node   string
	Status string
}

type pveNodeStatusResp struct {
	CPU    float64 `json:"cpu"`
	Memory struct {
		Used  int64 `json:"used"`
		Total int64 `json:"total"`
	} `json:"memory"`
}

// pveVMListItem keeps every counter as a pointer: PVE omits a field it
// cannot report, and an absent field must not become a zero datapoint.
type pveVMListItem struct {
	VMID      int      `json:"vmid"`
	Name      string   `json:"name"`
	Status    string   `json:"status"`
	CPU       *float64 `json:"cpu"`
	Mem       *float64 `json:"mem"`
	MaxMem    *float64 `json:"maxmem"`
	DiskRead  *float64 `json:"diskread"`
	DiskWrite *float64 `json:"diskwrite"`
	NetIn     *float64 `json:"netin"`
	NetOut    *float64 `json:"netout"`
}

// pveGuest is one entry of /cluster/resources?type=vm.
type pveGuest struct {
	pveVMListItem
	Node string `json:"node"`
	Type string `json:"type"`
}

// pveVMStatus is used for both QEMU VMs and LXC containers (same shape).
type pveVMStatus = pveVMListItem

type pveStorageItem struct {
	Storage string `json:"storage"`
	Used    int64  `json:"used"`
	Total   int64  `json:"total"`
}

func (p *ProxmoxProbe) fetchNodes() ([]pveNode, error) {
	var items []pveNodeListItem
	if err := p.apiGet("/nodes", &items); err != nil {
		return nil, err
	}
	nodes := make([]pveNode, len(items))
	for i, it := range items {
		nodes[i] = pveNode{Node: it.Node, Status: it.Status}
	}
	return nodes, nil
}

func (p *ProxmoxProbe) fetchNodeStatus(node string) (pveNodeStatusResp, error) {
	var ns pveNodeStatusResp
	err := p.apiGet(fmt.Sprintf("/nodes/%s/status", node), &ns)
	return ns, err
}

func (p *ProxmoxProbe) fetchVMs(node string) ([]pveVMListItem, error) {
	var items []pveVMListItem
	err := p.apiGet(fmt.Sprintf("/nodes/%s/qemu", node), &items)
	return items, err
}

func (p *ProxmoxProbe) fetchContainers(node string) ([]pveVMListItem, error) {
	var items []pveVMListItem
	err := p.apiGet(fmt.Sprintf("/nodes/%s/lxc", node), &items)
	return items, err
}

func (p *ProxmoxProbe) fetchStorage(node string) ([]pveStorageItem, error) {
	var items []pveStorageItem
	err := p.apiGet(fmt.Sprintf("/nodes/%s/storage", node), &items)
	return items, err
}

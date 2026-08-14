package swarm

import (
	"strconv"
	"strings"
	"time"

	"senhub-agent.go/internal/agent/services/data_store"
	"senhub-agent.go/internal/agent/tags"
)

// Overlay networks: who is on which segment, and what can reach what.
//
// An overlay is a virtual L2 segment stretched across every node of the swarm.
// It matters for two reasons an operator cannot see from any single host:
//
//   - It is the reachability boundary. Two services on the same overlay resolve
//     each other by name and can talk; two services on different overlays
//     cannot, whatever the firewall says. When a deploy "cannot reach the
//     database", the first question is whether both are on the same segment,
//     and answering it today means reading three `docker network inspect`
//     outputs on the right node.
//   - The `ingress` overlay carries the routing mesh: a port published there
//     answers on EVERY node, including the ones running none of the service.
//
// What this reports, and what it does not
//
// Reported: the segments, their subnets, and the attachment map — which
// services and how many tasks sit on each, with the service's virtual IP.
// That is the reachability graph: it answers "can A talk to B", "what else is
// on this segment", "which segment carries this published port".
//
// NOT reported: how much traffic flows between two given services. The Engine
// API exposes no per-peer counters, and per-container interface counters cannot
// be attributed to a named overlay — the stats endpoint keys them by interface
// name (eth0, eth1), which the API never maps back to a network. Measuring
// actual volume per flow needs conntrack or eBPF on each node, a different
// instrument with different privileges. Claiming a traffic matrix out of an
// attachment map would be inventing numbers, so the probe reports the topology
// it can prove and stays silent on the rest.

// overlayPoints emits the segments and the attachment map.
func (p *swarmProbe) overlayPoints(networks []network, services []service, allTasks []task, clusterTags []tags.Tag, now time.Time) []data_store.DataPoint {
	overlays := overlayNetworks(networks)
	if len(overlays) == 0 {
		return nil
	}

	byID := make(map[string]*network, len(overlays))
	for i := range overlays {
		byID[overlays[i].ID] = &overlays[i]
	}
	// A service declares its networks by id OR by name depending on how it was
	// created, so both spellings must resolve to the same segment.
	byName := make(map[string]*network, len(overlays))
	for i := range overlays {
		byName[overlays[i].Name] = &overlays[i]
	}

	tasksPerNetwork := map[string]int{}
	for i := range allTasks {
		for _, att := range allTasks[i].NetworksAttachments {
			if _, ok := byID[att.Network.ID]; ok {
				tasksPerNetwork[att.Network.ID]++
			}
		}
	}

	servicesPerNetwork := map[string]int{}
	var points []data_store.DataPoint

	for i := range services {
		s := &services[i]
		vips := map[string]string{}
		for _, v := range s.Endpoint.VirtualIPs {
			vips[v.NetworkID] = v.Addr
		}
		for _, netID := range serviceNetworkIDs(s, byID, byName) {
			servicesPerNetwork[netID]++
			n := byID[netID]
			t := withTags(clusterTags,
				tags.Tag{Key: "swarm.service.id", Value: s.ID},
				tags.Tag{Key: "swarm.service.name", Value: s.Spec.Name},
				tags.Tag{Key: "swarm.network.id", Value: n.ID},
				tags.Tag{Key: "swarm.network.name", Value: n.Name},
				tags.Tag{Key: "network.segment.id", Value: segmentID(n.ID)},
				tags.Tag{Key: "metric_type", Value: "network"},
			)
			// The service's virtual IP on this segment is what its name
			// resolves to for every other service attached to it — the address
			// a reachability problem is actually about.
			if vip := strings.TrimSpace(vips[netID]); vip != "" {
				t = withTags(t, tags.Tag{Key: "swarm.service.vip", Value: vip})
			}
			points = append(points, point("swarm.service.network.attached", 1, now, t))
		}
	}

	for i := range overlays {
		n := &overlays[i]
		base := withTags(clusterTags,
			tags.Tag{Key: "swarm.network.id", Value: n.ID},
			tags.Tag{Key: "swarm.network.name", Value: n.Name},
			tags.Tag{Key: "swarm.network.subnet", Value: primarySubnet(n)},
			// The identity of the network.segment entity these metrics
			// describe. The bare Swarm id stays too: it is what an operator
			// reads in `docker network ls`.
			tags.Tag{Key: "network.segment.id", Value: segmentID(n.ID)},
			tags.Tag{Key: "metric_type", Value: "network"},
		)
		points = append(points,
			point("swarm.network.services", float64(servicesPerNetwork[n.ID]), now, base),
			point("swarm.network.tasks", float64(tasksPerNetwork[n.ID]), now, base),
			point("swarm.network.ingress", boolValue(n.Ingress), now, base),
			point("swarm.network.attachable", boolValue(n.Attachable), now, base),
			point("swarm.network.internal", boolValue(n.Internal), now, base),
			point("swarm.network.address.capacity", float64(subnetCapacity(primarySubnet(n))), now, base),
		)
	}

	points = append(points, point("swarm.cluster.networks", float64(len(overlays)), now,
		withTags(clusterTags, tags.Tag{Key: "metric_type", Value: "network"})))

	return points
}

// overlayNetworks keeps the swarm-scoped overlay segments.
//
// Driver AND scope are both checked: a local overlay can exist on a single
// engine without being part of the cluster's data plane, and counting it would
// report a segment no other node can see.
func overlayNetworks(networks []network) []network {
	var out []network
	for i := range networks {
		n := networks[i]
		if strings.EqualFold(n.Driver, "overlay") && strings.EqualFold(n.Scope, "swarm") {
			out = append(out, n)
		}
	}
	return out
}

// serviceNetworkIDs resolves the segments a service is attached to.
//
// Attachments live in two places depending on the Engine version that created
// the service (TaskTemplate.Networks is current, Spec.Networks is the legacy
// slot) and each entry may name a network by id or by name. Reading only one
// place, or only one spelling, silently reports a service as attached to
// nothing — which reads exactly like a service correctly isolated.
func serviceNetworkIDs(s *service, byID, byName map[string]*network) []string {
	seen := map[string]bool{}
	var out []string

	add := func(target string) {
		target = strings.TrimSpace(target)
		if target == "" {
			return
		}
		var n *network
		if v, ok := byID[target]; ok {
			n = v
		} else if v, ok := byName[target]; ok {
			n = v
		}
		if n == nil || seen[n.ID] {
			return
		}
		seen[n.ID] = true
		out = append(out, n.ID)
	}

	for _, a := range s.Spec.TaskTemplate.Networks {
		add(a.Target)
	}
	for _, a := range s.Spec.Networks {
		add(a.Target)
	}
	// A service with published ports rides the ingress segment whether or not
	// it declares it — that is how the routing mesh reaches it.
	if hasPublishedPort(s) {
		for id, n := range byID {
			if n.Ingress {
				add(id)
			}
		}
	}
	return out
}

func hasPublishedPort(s *service) bool {
	for _, p := range s.Endpoint.Ports {
		if p.PublishedPort != 0 {
			return true
		}
	}
	for _, p := range s.Endpoint.Spec.Ports {
		if p.PublishedPort != 0 {
			return true
		}
	}
	return false
}

func primarySubnet(n *network) string {
	if len(n.IPAM.Config) == 0 {
		return ""
	}
	return n.IPAM.Config[0].Subnet
}

// subnetCapacity returns the number of addresses a CIDR can hand out.
//
// An overlay that runs out of addresses refuses new tasks with a scheduling
// error that names neither the network nor the exhaustion, so the capacity is
// worth having next to the attachment count: a /24 with 250 tasks on it is a
// deploy about to fail. Returns 0 for anything unparseable rather than a guess.
func subnetCapacity(cidr string) int64 {
	idx := strings.LastIndex(cidr, "/")
	if idx < 0 {
		return 0
	}
	bits, err := strconv.Atoi(cidr[idx+1:])
	if err != nil {
		return 0
	}
	total := 32
	if strings.Contains(cidr[:idx], ":") {
		// An IPv6 overlay's address space is effectively unbounded; reporting
		// 2^n here would produce a number no panel can render usefully.
		return 0
	}
	if bits < 0 || bits > total {
		return 0
	}
	size := int64(1) << (total - bits)
	// Network and broadcast addresses are not assignable.
	if size >= 2 {
		size -= 2
	}
	return size
}

// segmentID renders a Swarm network id as the consumer's subtype-prefixed
// segment identity.
//
// The prefix is not decoration: the identity scale is by precedence, and only
// `swarm:` is frozen (ADR 0034). A bare network id would say nothing about
// which authority assigned it, and the day a second subtype arrives there
// would be no way to tell the two apart — the mistake the consumer refused to
// make for `vlan:` and `k8s:`, where no assigned identifier exists at all.
func segmentID(networkID string) string {
	return "swarm:" + networkID
}

// segmentFactsOf reduces the overlay networks to what the graph carries: the
// identity and the descriptive facts, never the measurements. The counts and
// the address capacity stay on the metric rail — they change every cycle, and
// an entity that churns on every heartbeat is noise in a topology feed.
func segmentFactsOf(networks []network) []segmentFacts {
	overlays := overlayNetworks(networks)
	out := make([]segmentFacts, 0, len(overlays))
	for i := range overlays {
		n := &overlays[i]
		out = append(out, segmentFacts{
			id:       segmentID(n.ID),
			name:     n.Name,
			subnet:   primarySubnet(n),
			ingress:  n.Ingress,
			internal: n.Internal,
		})
	}
	return out
}

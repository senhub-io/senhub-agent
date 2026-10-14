package snmppoll

import (
	"fmt"
	"strings"

	"senhub-agent.go/internal/agent/services/entity"
	"senhub-agent.go/internal/agent/services/logger"
	"senhub-agent.go/internal/agent/services/snmpcore"
)

// LAG / LACP aggregation modelling (IANAifType 161 ieee8023adLag).
//
// The physical member ports and the logical aggregate interface are both
// network.interface entities.  This file collects the relationship from
// IF-MIB ifStackTable and IEEE8023-LAG-MIB, and then applies the frozen
// attributes and the aggregated_into relation to the observation.
//
// Collection is only attempted when entity.AggregationEnabled() is true.
// All walks here are optional: an error only logs at Debug and leaves the
// affected part of the aggregation data empty.

// ifTypeIEEE8023adLag is IANAifType ieee8023adLag.
const ifTypeIEEE8023adLag = 161

const (
	ifStackStatusOID               = "1.3.6.1.2.1.31.1.2.1.3"
	dot3adAggPortAttachedAggIDOID  = "1.2.840.10006.300.43.1.2.1.1.13"
	dot3adAggPortActorOperStateOID = "1.2.840.10006.300.43.1.2.1.1.21"
)

// lagData is the raw aggregation topology collected from a device.
type lagData struct {
	// members maps a physical member ifIndex to its aggregate ifIndex.
	members map[string]string
	// memberState maps a member ifIndex to "active" or "standby".
	memberState map[string]string
	// aggregateMode maps an aggregate ifIndex to "lacp" or "static".
	aggregateMode map[string]string
}

// collectLagData walks ifStackTable and IEEE8023-LAG-MIB (when enabled) and
// builds a lagData.  If aggregation is disabled this returns an empty
// lagData (no walks are issued).  Walk errors are logged at Debug and do
// not fail the sweep.
func collectLagData(client snmpClient, ifaces []ifaceRow, log *logger.ModuleLogger) lagData {
	data := lagData{
		members:       map[string]string{},
		memberState:   map[string]string{},
		aggregateMode: map[string]string{},
	}

	if !entity.AggregationEnabled() {
		return data
	}

	// index -> interface row for ifType and ifOperStatus lookups.
	ifaceByIndex := make(map[string]*ifaceRow, len(ifaces))
	for i := range ifaces {
		ifaceByIndex[ifaces[i].Index] = &ifaces[i]
	}

	// ---- IF-MIB ifStackTable ----
	ifStackBinds, err := client.WalkRaw(ifStackStatusOID)
	if err != nil {
		log.Debug().Err(err).Msg("ifStackTable walk failed; aggregation topology may be incomplete")
	} else {
		for _, b := range ifStackBinds {
			higher, lower, ok := parseIfStackIndex(b.OID, ifStackStatusOID)
			if !ok || higher == "0" || lower == "0" {
				continue
			}
			if ifc, ok := ifaceByIndex[higher]; !ok || ifc.IfType != ifTypeIEEE8023adLag {
				continue // only aggregates (ifType 161) are supported
			}
			if v, ok := snmpcore.AsInt(b.Value); ok && v == 1 {
				data.members[lower] = higher
			}
		}
	}

	// ---- IEEE8023-LAG-MIB ----
	attachedBinds, err1 := client.WalkRaw(dot3adAggPortAttachedAggIDOID)
	if err1 != nil {
		log.Debug().Err(err1).Msg("dot3adAggPortAttachedAggID walk failed; LACP membership may be incomplete")
	}
	actorBinds, err2 := client.WalkRaw(dot3adAggPortActorOperStateOID)
	if err2 != nil {
		log.Debug().Err(err2).Msg("dot3adAggPortActorOperState walk failed; LACP state may be incomplete")
	}

	lagMIBMembers := map[string]bool{}
	actorState := map[string]byte{}

	for _, b := range attachedBinds {
		memberIdx, ok := strings.CutPrefix(b.OID, dot3adAggPortAttachedAggIDOID+".")
		if !ok {
			continue
		}
		aggIdx, ok := snmpcore.AsInt(b.Value)
		if !ok || aggIdx == 0 {
			continue
		}
		data.members[memberIdx] = fmt.Sprintf("%d", aggIdx)
		lagMIBMembers[memberIdx] = true
	}
	for _, b := range actorBinds {
		memberIdx, ok := strings.CutPrefix(b.OID, dot3adAggPortActorOperStateOID+".")
		if !ok {
			continue
		}
		octets := snmpcore.AsBytes(b.Value)
		if len(octets) == 0 {
			continue
		}
		actorState[memberIdx] = octets[0]
		lagMIBMembers[memberIdx] = true
	}

	// ---- member state ----
	// The state exists only where LACP gives one: a member the LAG MIB says
	// nothing about (a static port-channel) has no backup notion, so its
	// state stays absent and the aggregate's mode says every member carries
	// traffic. ifOperStatus is not a substitute: a link that is up says
	// nothing about LACP collecting and distributing.
	for memberIdx := range data.members {
		st, ok := actorState[memberIdx]
		if !ok {
			continue
		}
		if st&0x30 == 0x30 {
			data.memberState[memberIdx] = "active"
		} else {
			data.memberState[memberIdx] = "standby"
		}
	}

	// ---- aggregate mode ----
	aggregates := map[string]bool{}
	for _, aggIdx := range data.members {
		aggregates[aggIdx] = true
	}
	for aggIdx := range aggregates {
		mode := "static"
		for memberIdx, aIdx := range data.members {
			if aIdx == aggIdx && lagMIBMembers[memberIdx] {
				mode = "lacp"
				break
			}
		}
		data.aggregateMode[aggIdx] = mode
	}

	return data
}

// parseIfStackIndex splits an ifStackStatus OID suffix of the form
// "<higherLayer>.<lowerLayer>" and returns both indices.
func parseIfStackIndex(oid, base string) (higher, lower string, ok bool) {
	suffix, ok := strings.CutPrefix(oid, base+".")
	if !ok {
		return "", "", false
	}
	parts := strings.Split(suffix, ".")
	if len(parts) != 2 {
		return "", "", false
	}
	return parts[0], parts[1], true
}

// applyLag adds the aggregated_into relations and the per-interface
// aggregation attributes to an observation.  It only emits when both the
// member and aggregate interfaces are present as entities in the same
// observation (rule 4).
func applyLag(obs *entity.Observation, lag lagData, ifaces []ifaceRow, deviceID string) {
	if len(lag.members) == 0 {
		return
	}

	// ifIndex -> ifName
	ifNameByIndex := make(map[string]string, len(ifaces))
	for _, ifc := range ifaces {
		if ifc.Name != "" {
			ifNameByIndex[ifc.Index] = ifc.Name
		}
	}
	// ifName -> ifIndex (to identify member/aggregate entities)
	ifNameToIndex := make(map[string]string, len(ifNameByIndex))
	for idx, name := range ifNameByIndex {
		ifNameToIndex[name] = idx
	}

	// emitted interface names from the observation
	emitted := map[string]bool{}
	for _, e := range obs.Entities {
		if e.Type == entityTypeNetworkInterface {
			if name, ok := e.ID[idKeyInterfaceName].(string); ok {
				emitted[name] = true
			}
		}
	}

	// relations: member -> aggregate
	for memberIdx, aggIdx := range lag.members {
		memberName := ifNameByIndex[memberIdx]
		aggName := ifNameByIndex[aggIdx]
		if memberName == "" || aggName == "" {
			continue
		}
		if !emitted[memberName] || !emitted[aggName] {
			continue
		}
		obs.Relations = append(obs.Relations, entity.Relation{
			Type:     entity.RelAggregatedInto,
			FromType: entityTypeNetworkInterface,
			FromID:   interfacePortKey(deviceID, memberName),
			ToType:   entityTypeNetworkInterface,
			ToID:     interfacePortKey(deviceID, aggName),
		})
	}

	// attributes
	for i := range obs.Entities {
		e := &obs.Entities[i]
		if e.Type != entityTypeNetworkInterface {
			continue
		}
		name, _ := e.ID[idKeyInterfaceName].(string)
		if name == "" {
			continue
		}
		idx := ifNameToIndex[name]
		if idx == "" {
			continue
		}
		if state, ok := lag.memberState[idx]; ok {
			if e.Attributes == nil {
				e.Attributes = map[string]any{}
			}
			e.Attributes[entity.AttrAggregationState] = state
		}
		if mode, ok := lag.aggregateMode[idx]; ok {
			if e.Attributes == nil {
				e.Attributes = map[string]any{}
			}
			e.Attributes[entity.AttrAggregationMode] = mode
		}
	}
}

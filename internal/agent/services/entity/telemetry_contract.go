package entity

import "github.com/toise-dev/toise/pkg/emit/wire"

// Canonical entity-type vocabulary and the telemetry declaration each type
// carries. Both halves of the entity/telemetry correlation contract live
// here; see docs/developer-guide/engineering/ENTITY-TELEMETRY-CONTRACT.md.
//
// Until this file existed there was no list of the types the agent emits:
// every source declared its own private `entityTypeHost = "host"` constant,
// duplicated across ~25 files. Nothing could walk "every entity type",
// which is why network.interface could be documented as shipped while its
// host half joined nothing (#748) — a claim verified against the emitter
// instead of against the join.

// Entity types. The spelling is not ours: these alias the Toise SDK's `wire`
// package, which is the single in-repo spelling shared by the SDK, the
// conformance kit and the consumer's ingest boundary.
//
// Aliasing rather than redeclaring is the whole point. A constant declared
// here would be a copy maintained in parallel, and a copy is what let
// `process` and `compute.vm` be emitted for months while this file claimed
// ten types (#753). Now a type the consumer does not register does not
// compile.
//
// `wire` is stdlib-only, so consuming the vocabulary pulls no protocol stack
// into the agent's module graph.
const (
	TypeHost             = wire.TypeHost
	TypeProcess          = wire.TypeProcess
	TypeContainer        = wire.TypeContainer
	TypeComputeVM        = wire.TypeComputeVM
	TypeServiceInstance  = wire.TypeServiceInstance
	TypeServiceListener  = wire.TypeServiceListener
	TypeDB               = wire.TypeDatabase
	TypeNetworkDevice    = wire.TypeNetworkDevice
	TypeNetworkInterface = wire.TypeNetworkInterface
	TypeNetworkAddress   = wire.TypeNetworkAddress
	TypeNetworkRoute     = wire.TypeNetworkRoute
	TypeNetworkEndpoint  = wire.TypeNetworkEndpoint
	TypePod              = wire.TypePod
)

// Relation types, same reasoning. The last three are registered but legacy:
// the consumer's frontier still accepts them, producers emit the entity form
// instead (topology-as-entities, ADR 0022).
const (
	RelRunsOn       = wire.RelTypeRunsOn
	RelHasInterface = wire.RelTypeHasInterface
	RelBoundTo      = wire.RelTypeBoundTo
	RelNextHopVia   = wire.RelTypeNextHopVia
	RelListensOn    = wire.RelTypeListensOn
	RelMonitors     = wire.RelTypeMonitors
	RelHasRoute     = wire.RelTypeHasRoute
	RelConnectedTo  = wire.RelTypeConnectedTo
	RelDependsOn    = wire.RelTypeDependsOn
	RelSameAs       = wire.RelTypeSameAs
)

// AllTypes is every entity type the agent may emit — taken from the SDK, not
// listed here. A locally-held list is a second source of truth, and the two
// diverge the moment one of them is edited.
var AllTypes = wire.EntityTypes()

// AllRelationTypes is the relation vocabulary, same source.
var AllRelationTypes = wire.RelationTypes()

// TelemetryStatus is the C4 classification: where a type's telemetry is, or
// an explicit statement that there is none.
type TelemetryStatus string

const (
	// StatusOwnKey — the type has telemetry of its own, located by its
	// identity attribute.
	StatusOwnKey TelemetryStatus = "own-key"
	// StatusInherited — no telemetry of its own, but a structural edge
	// reaches one that has.
	StatusInherited TelemetryStatus = "inherited"
	// StatusGraphOnly — no telemetry of its own and no structural edge
	// reaches any. A statement about what this producer emits and what the
	// graph carries; a consumer may still resolve one by computation on its
	// own side, which is not a guarantee this contract makes.
	StatusGraphOnly TelemetryStatus = "graph-only"
)

// Carrier says where the subject key rides.
type Carrier string

const (
	// CarrierResource — on the OTLP resource, once per export. Used when the
	// subject IS the agent's own host or the agent itself.
	CarrierResource Carrier = "resource"
	// CarrierDatapoint — a per-datapoint attribute. Required whenever the
	// subject is a remote target: no resource describes it, because the
	// resource always describes the machine the agent runs on.
	CarrierDatapoint Carrier = "datapoint"
	// CarrierNone — inherited or graph-only types carry no subject key.
	CarrierNone Carrier = ""
)

// TelemetryDeclaration is what a type declares about its telemetry. It is
// the enforceable half of the contract: C6 checks emitted output against it.
type TelemetryDeclaration struct {
	Status TelemetryStatus

	// SubjectKey is the identity attribute that must appear on the type's
	// telemetry, byte-identical to the value carried in the entity's ID.
	// Empty for inherited and graph-only types.
	SubjectKey string

	// Carrier says where SubjectKey rides. Empty unless Status is own-key.
	Carrier Carrier

	// InheritedVia names the structural edge that reaches telemetry.
	// Set only when Status is inherited.
	InheritedVia string

	// Shipped reports whether the declaration currently holds in emitted
	// output. A false here is a known, tracked gap — never a licence to
	// leave it undeclared, and Gap must name the issue.
	Shipped bool

	// Gap explains what is missing when Shipped is false.
	Gap string
}

// TelemetryContract maps every entity type to its declaration.
//
// Ordering constraint (C6): a subject key must not be emitted before the
// type's identity is unique. Publishing a colliding value as a join key
// turns a gap the consumer can detect into a wrong answer it cannot.
var TelemetryContract = map[string]TelemetryDeclaration{
	TypeHost: {
		Status:     StatusOwnKey,
		SubjectKey: "host.id",
		Carrier:    CarrierResource,
		Shipped:    true,
	},
	TypeContainer: {
		Status:     StatusOwnKey,
		SubjectKey: "container.id",
		Carrier:    CarrierDatapoint,
		Shipped:    true,
	},
	TypeServiceInstance: {
		Status:     StatusOwnKey,
		SubjectKey: "service.instance.id",
		Carrier:    CarrierResource,
		Shipped:    true,
	},
	TypeServiceListener: {
		Status:       StatusInherited,
		InheritedVia: "runs_on",
		Shipped:      true,
	},
	TypeDB: {
		Status:     StatusOwnKey,
		SubjectKey: "db.instance.id",
		Carrier:    CarrierDatapoint,
		Shipped:    false,
		Gap: "db.instance.id lives only on the entity rail; no db probe stamps " +
			"it on its metrics, and it is not on the resource either (the " +
			"resource describes the agent's host, never a remote target). " +
			"Blocked behind the identity repair: the address:port fallback " +
			"collapses distinct servers, so publishing it as a join key would " +
			"turn a detectable gap into an undetectable wrong answer (#740, #741)",
	},
	TypeNetworkDevice: {
		Status:     StatusOwnKey,
		SubjectKey: "network.device.id",
		Carrier:    CarrierDatapoint,
		Shipped:    true,
	},
	TypeNetworkInterface: {
		Status:     StatusOwnKey,
		SubjectKey: "interface.name",
		Carrier:    CarrierDatapoint,
		Shipped:    true,
	},
	TypeProcess: {
		Status:     StatusOwnKey,
		SubjectKey: "process.pid",
		Carrier:    CarrierDatapoint,
		Shipped:    false,
		Gap: "the pid IS stamped on process metrics, but the identity it comes " +
			"from is not host-scoped: {process.pid, process.creation.time} " +
			"collides between two hosts that started a process with the same pid " +
			"at the same instant. Publishing the pid as a join key before the " +
			"identity is scoped would point a consumer at another machine's " +
			"process — the ordering constraint again (contract §2d, #753)",
	},
	TypeComputeVM: {
		Status:     StatusOwnKey,
		SubjectKey: "vmid",
		Carrier:    CarrierDatapoint,
		Shipped:    false,
		Gap: "vmid lives only on the entity rail (hyperv/entity_source.go); no " +
			"hypervisor metric carries it, so a compute.vm entity reaches none " +
			"of its own telemetry. Same shape as db (#741), found by comparing " +
			"the vocabulary with Toise's registry (#753)",
	},
	TypePod: {
		Status:     StatusOwnKey,
		SubjectKey: "k8s.pod.uid",
		Carrier:    CarrierDatapoint,
		Shipped:    false,
		Gap: "the pod owns telemetry no container has — the network namespace " +
			"is shared, so network measurements belong to the pod and nothing " +
			"else. That is the argument that settled the type with the consumer, " +
			"and the metric side of it is not built yet: pod metrics carry " +
			"k8s.pod.name, which is namespace-scoped and reusable, not the UID " +
			"the entity is keyed on. Same shape as db (#756)",
	},
	TypeNetworkAddress: {
		Status:  StatusGraphOnly,
		Shipped: true,
	},
	TypeNetworkRoute: {
		Status:  StatusGraphOnly,
		Shipped: true,
	},
	TypeNetworkEndpoint: {
		Status:  StatusGraphOnly,
		Shipped: true,
	},
}

// DeclarationFor returns the declaration for an entity type.
func DeclarationFor(entityType string) (TelemetryDeclaration, bool) {
	d, ok := TelemetryContract[entityType]
	return d, ok
}

package entity

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

// Entity types, frozen with the Toise team. A new type is a contract
// change, not a local decision.
const (
	TypeHost             = "host"
	TypeContainer        = "container"
	TypeServiceInstance  = "service.instance"
	TypeServiceListener  = "service.listener"
	TypeDB               = "db"
	TypeNetworkDevice    = "network.device"
	TypeNetworkInterface = "network.interface"
	TypeNetworkAddress   = "network.address"
	TypeNetworkRoute     = "network.route"
	TypeNetworkEndpoint  = "network.endpoint"
	TypeProcess          = "process"
	TypeComputeVM        = "compute.vm"
)

// AllTypes is every entity type the agent may emit. The C6 test walks it,
// so a type added here without a declaration below fails the build's tests
// rather than shipping undeclared.
var AllTypes = []string{
	TypeHost,
	TypeContainer,
	TypeServiceInstance,
	TypeServiceListener,
	TypeDB,
	TypeNetworkDevice,
	TypeNetworkInterface,
	TypeNetworkAddress,
	TypeNetworkRoute,
	TypeNetworkEndpoint,
	TypeProcess,
	TypeComputeVM,
}

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

package template

// The nameplate is what the agent already knows about the machine and
// what Zabbix keeps in host inventory: the operating system, the
// hardware, the serial number. It travels inside the agent on the
// entity rail, as attributes of the host entity, and the Zabbix output
// carried metrics only — so a host that registered by itself arrived
// with a full set of measurements and an empty inventory, which an
// operator then fills by hand or leaves empty for ever.
//
// Only the facts that have a Zabbix inventory field are sent. The other
// half of the entity rail, the relationships, has no home here: Zabbix
// has hosts, groups and tags, not a graph. Flattening a graph into an
// inventory field would lose what makes that rail worth having, so it
// is deliberately left where it belongs.
//
// The list lives here, in the package that writes the template, and the
// output reads it from here too. The key builders are mirrored between
// the two packages on purpose and guarded by a test; a static table is
// not worth the same risk.

// NameplateField ties one fact the agent knows to the item that carries
// it and the inventory field Zabbix files it under.
type NameplateField struct {
	// Key is the item key, before the configured prefix is applied.
	Key string
	// Attribute is the host entity attribute holding the value.
	Attribute string
	// Inventory is the Zabbix host inventory field the item populates,
	// spelled as the export format spells it.
	Inventory string
	// Name is what an operator reads on the host page.
	Name string
	// Description says where the value comes from, since an inventory
	// field filled by itself is otherwise unexplained.
	Description string
}

// NameplateFields is the whole mapping, in the order an operator reads
// it. A fact the agent did not find is not sent, so its inventory field
// stays as it was rather than being blanked.
var NameplateFields = []NameplateField{
	{
		Key: "host.name", Attribute: "host.name", Inventory: "NAME",
		Name:        "Host name",
		Description: "Name the machine reports for itself, which may differ from the name it registered under.",
	},
	{
		Key: "host.os", Attribute: "os.name", Inventory: "OS",
		Name:        "Operating system",
		Description: "Operating system name, as the machine reports it.",
	},
	{
		Key: "host.os.full", Attribute: "os.description", Inventory: "OS_FULL",
		Name:        "Operating system, full",
		Description: "Full operating system description, including the build.",
	},
	{
		Key: "host.os.short", Attribute: "os.type", Inventory: "OS_SHORT",
		Name:        "Operating system family",
		Description: "Operating system family: linux, windows, darwin.",
	},
	{
		Key: "host.type", Attribute: "host.chassis.type", Inventory: "TYPE",
		Name:        "Chassis type",
		Description: "What the machine is: a server, a virtual machine, a laptop.",
	},
	{
		Key: "host.hardware", Attribute: "host.cpu.model.name", Inventory: "HARDWARE",
		Name:        "Processor",
		Description: "Processor model the machine reports.",
	},
	{
		Key: "host.vendor", Attribute: "hw.vendor", Inventory: "VENDOR",
		Name:        "Hardware vendor",
		Description: "Vendor named on the machine's firmware.",
	},
	{
		Key: "host.model", Attribute: "hw.model", Inventory: "MODEL",
		Name:        "Hardware model",
		Description: "Model named on the machine's firmware.",
	},
	{
		Key: "host.serial", Attribute: "hw.serial_number", Inventory: "SERIALNO_A",
		Name:        "Serial number",
		Description: "Serial number from the machine's firmware, which is what ties this host to an asset record.",
	},
}

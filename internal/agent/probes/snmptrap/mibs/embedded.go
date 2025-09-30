package mibs

// EmbeddedMIBs contains standard and vendor-specific MIB definitions
// These are embedded in the binary to provide basic trap translation capabilities
var EmbeddedMIBs = map[string]string{
	// Standard RFC MIBs
	"SNMPv2-MIB": snmpv2MIB,
	"IF-MIB":     ifMIB,
	"IP-MIB":     ipMIB,
	"TCP-MIB":    tcpMIB,
	"UDP-MIB":    udpMIB,
	
	// Vendor-specific MIBs (core definitions)
	"CISCO-SMI":          ciscoSMI,
	"PALOALTO-MIB":       paloaltoMIB,
	"FORTINET-CORE-MIB":  fortinetCoreMIB,
	"JUNIPER-SMI":        juniperSMI,
	"F5-BIGIP-MIB":       f5BigipMIB,
	"DELL-MIB":           dellMIB,
	"HPE-MIB":            hpeMIB,
}

// Standard SNMPv2 MIB definitions
const snmpv2MIB = `
-- SNMPv2-MIB Core Definitions
enterprises OBJECT IDENTIFIER ::= { iso org(3) dod(6) internet(1) private(4) 1 }

-- Standard trap definitions
coldStart NOTIFICATION-TYPE
    STATUS current
    DESCRIPTION "A coldStart trap signifies that the SNMP entity is reinitializing itself."
    ::= { snmpTraps 1 }

warmStart NOTIFICATION-TYPE
    STATUS current
    DESCRIPTION "A warmStart trap signifies that the SNMP entity is reinitializing itself."
    ::= { snmpTraps 2 }

linkDown NOTIFICATION-TYPE
    STATUS current
    DESCRIPTION "A linkDown trap signifies that the SNMP entity has detected a failure in one of the communication links."
    ::= { snmpTraps 3 }

linkUp NOTIFICATION-TYPE
    STATUS current
    DESCRIPTION "A linkUp trap signifies that the SNMP entity has detected that one of the communication links has come up."
    ::= { snmpTraps 4 }

authenticationFailure NOTIFICATION-TYPE
    STATUS current
    DESCRIPTION "An authenticationFailure trap signifies that the SNMP entity has received a protocol message that is not properly authenticated."
    ::= { snmpTraps 5 }
`

// Interface MIB definitions
const ifMIB = `
-- IF-MIB Core Interface Definitions
ifIndex OBJECT-TYPE
    SYNTAX InterfaceIndex
    MAX-ACCESS read-only
    STATUS current
    DESCRIPTION "A unique value for each interface."
    ::= { ifEntry 1 }

ifDescr OBJECT-TYPE
    SYNTAX DisplayString (SIZE (0..255))
    MAX-ACCESS read-only
    STATUS current
    DESCRIPTION "A textual string containing information about the interface."
    ::= { ifEntry 2 }

ifOperStatus OBJECT-TYPE
    SYNTAX INTEGER { up(1), down(2), testing(3), unknown(4), dormant(5), notPresent(6), lowerLayerDown(7) }
    MAX-ACCESS read-only
    STATUS current
    DESCRIPTION "The current operational state of the interface."
    ::= { ifEntry 8 }

ifAdminStatus OBJECT-TYPE
    SYNTAX INTEGER { up(1), down(2), testing(3) }
    MAX-ACCESS read-write
    STATUS current
    DESCRIPTION "The desired state of the interface."
    ::= { ifEntry 7 }
`

// IP MIB definitions
const ipMIB = `
-- IP-MIB Core IP Definitions
ipForwarding OBJECT-TYPE
    SYNTAX INTEGER { forwarding(1), notForwarding(2) }
    MAX-ACCESS read-write
    STATUS current
    DESCRIPTION "The indication of whether this entity is acting as an IP gateway."
    ::= { ip 1 }

ipAddrTable OBJECT-TYPE
    SYNTAX SEQUENCE OF IpAddrEntry
    MAX-ACCESS not-accessible
    STATUS current
    DESCRIPTION "The table of addressing information relevant to this entity's IP addresses."
    ::= { ip 20 }
`

// TCP MIB definitions  
const tcpMIB = `
-- TCP-MIB Core TCP Definitions
tcpConnState OBJECT-TYPE
    SYNTAX INTEGER {
        closed(1),
        listen(2),
        synSent(3),
        synReceived(4),
        established(5),
        finWait1(6),
        finWait2(7),
        closeWait(8),
        lastAck(9),
        closing(10),
        timeWait(11),
        deleteTCB(12)
    }
    MAX-ACCESS read-write
    STATUS current
    DESCRIPTION "The state of this TCP connection."
    ::= { tcpConnEntry 1 }
`

// UDP MIB definitions
const udpMIB = `
-- UDP-MIB Core UDP Definitions
udpLocalAddress OBJECT-TYPE
    SYNTAX IpAddress
    MAX-ACCESS read-only
    STATUS current
    DESCRIPTION "The local IP address for this UDP listener."
    ::= { udpEntry 1 }

udpLocalPort OBJECT-TYPE
    SYNTAX INTEGER (0..65535)
    MAX-ACCESS read-only
    STATUS current
    DESCRIPTION "The local port number for this UDP listener."
    ::= { udpEntry 2 }
`

// Cisco SMI definitions
const ciscoSMI = `
-- CISCO-SMI Core Definitions
cisco OBJECT IDENTIFIER ::= { enterprises 9 }
ciscoProducts OBJECT IDENTIFIER ::= { cisco 1 }
ciscoMgmt OBJECT IDENTIFIER ::= { cisco 9 }

-- Common Cisco trap definitions
ciscoConfigManEvent NOTIFICATION-TYPE
    STATUS current
    DESCRIPTION "Cisco configuration management event"
    ::= { cisco 2 0 1 }

ciscoEnvMonTemperatureNotification NOTIFICATION-TYPE
    STATUS current
    DESCRIPTION "Cisco environmental monitoring temperature notification"
    ::= { cisco 13 4 1 }

ciscoPowerSupplyFailureNotif NOTIFICATION-TYPE
    STATUS current
    DESCRIPTION "Cisco power supply failure notification"
    ::= { cisco 13 4 2 }

ciscoFanFailureNotif NOTIFICATION-TYPE
    STATUS current
    DESCRIPTION "Cisco fan failure notification"
    ::= { cisco 13 4 3 }
`

// Palo Alto MIB definitions
const paloaltoMIB = `
-- PALOALTO-MIB Core Definitions
paloalto OBJECT IDENTIFIER ::= { enterprises 25461 }
paloaltoProducts OBJECT IDENTIFIER ::= { paloalto 1 }
paloaltoMibs OBJECT IDENTIFIER ::= { paloalto 2 }

-- Palo Alto trap definitions
panSessionUtilization NOTIFICATION-TYPE
    STATUS current
    DESCRIPTION "Palo Alto session utilization threshold exceeded"
    ::= { paloaltoMibs 1 1 }

panGlobalCounterThreshold NOTIFICATION-TYPE
    STATUS current
    DESCRIPTION "Palo Alto global counter threshold notification"
    ::= { paloaltoMibs 1 2 }

panThreatDetection NOTIFICATION-TYPE
    STATUS current
    DESCRIPTION "Palo Alto threat detection notification"
    ::= { paloaltoMibs 1 3 }

panSystemResourceUtilization NOTIFICATION-TYPE
    STATUS current
    DESCRIPTION "Palo Alto system resource utilization notification"
    ::= { paloaltoMibs 1 4 }
`

// Fortinet Core MIB definitions
const fortinetCoreMIB = `
-- FORTINET-CORE-MIB Core Definitions
fortinet OBJECT IDENTIFIER ::= { enterprises 12356 }
fortigate OBJECT IDENTIFIER ::= { fortinet 101 }

-- Fortinet trap definitions
fgTrapVpnTunState NOTIFICATION-TYPE
    STATUS current
    DESCRIPTION "Fortinet VPN tunnel state change notification"
    ::= { fortigate 6 2 1 }

fgTrapHaSwitch NOTIFICATION-TYPE
    STATUS current
    DESCRIPTION "Fortinet HA switch notification"
    ::= { fortigate 6 2 2 }

fgTrapHaMemberDown NOTIFICATION-TYPE
    STATUS current
    DESCRIPTION "Fortinet HA member down notification"
    ::= { fortigate 6 2 3 }

fgTrapVirusDetected NOTIFICATION-TYPE
    STATUS current
    DESCRIPTION "Fortinet virus detection notification"
    ::= { fortigate 6 2 4 }

fgTrapIntrusionDetected NOTIFICATION-TYPE
    STATUS current
    DESCRIPTION "Fortinet intrusion detection notification"
    ::= { fortigate 6 2 5 }
`

// Juniper SMI definitions
const juniperSMI = `
-- JUNIPER-SMI Core Definitions
juniperMIB OBJECT IDENTIFIER ::= { enterprises 2636 }
jnxMibs OBJECT IDENTIFIER ::= { juniperMIB 3 }

-- Juniper trap definitions
jnxChassisTemperatureOverThreshold NOTIFICATION-TYPE
    STATUS current
    DESCRIPTION "Juniper chassis temperature over threshold"
    ::= { jnxMibs 1 1 1 }

jnxPowerSupplyFailure NOTIFICATION-TYPE
    STATUS current
    DESCRIPTION "Juniper power supply failure notification"
    ::= { jnxMibs 1 1 2 }

jnxFanFailure NOTIFICATION-TYPE
    STATUS current
    DESCRIPTION "Juniper fan failure notification"
    ::= { jnxMibs 1 1 3 }

jnxBgpM2PeerStateChanged NOTIFICATION-TYPE
    STATUS current
    DESCRIPTION "Juniper BGP peer state change notification"
    ::= { jnxMibs 1 1 4 }
`

// F5 BIG-IP MIB definitions
const f5BigipMIB = `
-- F5-BIGIP-MIB Core Definitions
f5 OBJECT IDENTIFIER ::= { enterprises 3375 }
bigipCompliance OBJECT IDENTIFIER ::= { f5 1 }
bigipNotification OBJECT IDENTIFIER ::= { f5 2 4 }

-- F5 trap definitions
bigipAgentStart NOTIFICATION-TYPE
    STATUS current
    DESCRIPTION "F5 BIG-IP agent start notification"
    ::= { bigipNotification 1 }

bigipAgentShutdown NOTIFICATION-TYPE
    STATUS current
    DESCRIPTION "F5 BIG-IP agent shutdown notification"
    ::= { bigipNotification 2 }

bigipNodeDown NOTIFICATION-TYPE
    STATUS current
    DESCRIPTION "F5 BIG-IP node down notification"
    ::= { bigipNotification 3 }

bigipNodeUp NOTIFICATION-TYPE
    STATUS current
    DESCRIPTION "F5 BIG-IP node up notification"
    ::= { bigipNotification 4 }

bigipPoolMemberDown NOTIFICATION-TYPE
    STATUS current
    DESCRIPTION "F5 BIG-IP pool member down notification"
    ::= { bigipNotification 5 }
`

// Dell MIB definitions
const dellMIB = `
-- DELL-MIB Core Definitions
dell OBJECT IDENTIFIER ::= { enterprises 674 }
server3 OBJECT IDENTIFIER ::= { dell 10892 }
baseboardGroup OBJECT IDENTIFIER ::= { server3 1 }

-- Dell server trap definitions
dellServerTemperatureProbe NOTIFICATION-TYPE
    STATUS current
    DESCRIPTION "Dell server temperature probe notification"
    ::= { baseboardGroup 6 1 }

dellServerFanProbe NOTIFICATION-TYPE
    STATUS current
    DESCRIPTION "Dell server fan probe notification"
    ::= { baseboardGroup 6 2 }

dellServerVoltageProbe NOTIFICATION-TYPE
    STATUS current
    DESCRIPTION "Dell server voltage probe notification"
    ::= { baseboardGroup 6 3 }

dellServerPowerSupply NOTIFICATION-TYPE
    STATUS current
    DESCRIPTION "Dell server power supply notification"
    ::= { baseboardGroup 6 4 }
`

// HPE MIB definitions
const hpeMIB = `
-- HPE-MIB Core Definitions
hpe OBJECT IDENTIFIER ::= { enterprises 47196 }
hpeServer OBJECT IDENTIFIER ::= { hpe 4 }
hpeServerTraps OBJECT IDENTIFIER ::= { hpeServer 0 }

-- HPE server trap definitions
hpeServerCriticalTrap NOTIFICATION-TYPE
    STATUS current
    DESCRIPTION "HPE server critical event notification"
    ::= { hpeServerTraps 1 }

hpeServerWarningTrap NOTIFICATION-TYPE
    STATUS current
    DESCRIPTION "HPE server warning event notification"
    ::= { hpeServerTraps 2 }

hpeServerInformationalTrap NOTIFICATION-TYPE
    STATUS current
    DESCRIPTION "HPE server informational event notification"
    ::= { hpeServerTraps 3 }

hpeServerThermalTrap NOTIFICATION-TYPE
    STATUS current
    DESCRIPTION "HPE server thermal event notification"
    ::= { hpeServerTraps 4 }

hpeServerPowerTrap NOTIFICATION-TYPE
    STATUS current
    DESCRIPTION "HPE server power event notification"
    ::= { hpeServerTraps 5 }
`

// GetMIBContent returns the content of a specific embedded MIB
func GetMIBContent(mibName string) (string, bool) {
	content, exists := EmbeddedMIBs[mibName]
	return content, exists
}

// GetAllMIBNames returns a list of all embedded MIB names
func GetAllMIBNames() []string {
	names := make([]string, 0, len(EmbeddedMIBs))
	for name := range EmbeddedMIBs {
		names = append(names, name)
	}
	return names
}

// IsStandardMIB checks if a MIB is a standard RFC MIB
func IsStandardMIB(mibName string) bool {
	standardMIBs := map[string]bool{
		"SNMPv2-MIB": true,
		"IF-MIB":     true,
		"IP-MIB":     true,
		"TCP-MIB":    true,
		"UDP-MIB":    true,
	}
	return standardMIBs[mibName]
}

// GetVendorMIBs returns MIBs for a specific vendor
func GetVendorMIBs(vendor string) []string {
	vendorMIBs := map[string][]string{
		"cisco":     {"CISCO-SMI"},
		"paloalto":  {"PALOALTO-MIB"},
		"fortinet":  {"FORTINET-CORE-MIB"},
		"juniper":   {"JUNIPER-SMI"},
		"f5":        {"F5-BIGIP-MIB"},
		"dell":      {"DELL-MIB"},
		"hpe":       {"HPE-MIB"},
	}
	
	mibs, exists := vendorMIBs[vendor]
	if !exists {
		return []string{}
	}
	return mibs
}
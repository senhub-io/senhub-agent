package hostservice

// Identity and attribute keys of the host.service entity. Every producer
// (the supervisor probes, the telemetry contract) reads them from here so
// that a key is spelled in one place.
const (
	KeyName         = "host.service.name"
	KeyManager      = "host.service.manager"
	KeyStartMode    = "host.service.start_mode"
	KeyStartDelayed = "host.service.start_delayed"
	KeyTriggerStart = "host.service.trigger_start"
)

// Values of KeyManager.
const (
	ManagerSystemd    = "systemd"
	ManagerWindowsSCM = "windows_scm"
)

// Values of KeyStartMode written by the Windows probe. The systemd probe
// reports the unit file state as systemd spells it.
const (
	StartModeBoot   = "boot_start"
	StartModeSystem = "system_start"
	StartModeAuto   = "auto_start"
	StartModeDemand = "demand_start"
	StartModeOff    = "disabled"
)

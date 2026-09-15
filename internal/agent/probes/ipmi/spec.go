package ipmi

import "senhub-agent.go/internal/agent/probes"

func init() {
	probes.RegisterProbeSpec(probes.ProbeSpec{
		Type: "ipmi", DisplayName: "IPMI Sensors", Category: "host",
		Summary:  "Temperature, fan, voltage and status sensors of the host's BMC, or of a remote BMC over LAN, through ipmitool.",
		DocsPath: "docs/user-guide/docs/probes/ipmi.md", MultiInstance: false, DefaultInterval: 60, Platforms: []string{"linux"},
		Params: []probes.ParamSpec{
			{Key: "mode", Kind: probes.KindString, Default: "local", Enum: []string{"local", "remote"}, Group: "connection", Description: "local reads the host's own BMC; remote polls a BMC over LAN"},
			{Key: "remote", Kind: probes.KindBlock, EssentialWhen: []probes.Condition{{Key: "mode", Values: []string{"remote"}}}, Group: "connection", Description: "Remote BMC access, used with mode remote", Fields: []probes.ParamSpec{
				{Key: "host", Kind: probes.KindString, Description: "BMC address or hostname; required with mode remote"},
				{Key: "username", Kind: probes.KindString, Description: "IPMI user"},
				{Key: "password", Kind: probes.KindString, Secret: true, Description: "IPMI user's password"},
				{Key: "interface", Kind: probes.KindString, Default: "lanplus", Description: "ipmitool interface; lanplus for IPMI 2.0, lan for IPMI 1.5"},
			}},
			{Key: "sensors", Kind: probes.KindBlock, Group: "filter", Description: "Sensor selection", Fields: []probes.ParamSpec{
				{Key: "include_types", Kind: probes.KindStringList, Description: "Only these sensor types; empty means all", Example: "Temperature, Fan"},
				{Key: "exclude_names", Kind: probes.KindStringList, Description: "Regular expressions of sensor names to skip"},
			}},
			{Key: "ipmitool_path", Kind: probes.KindString, Default: "ipmitool", Group: "advanced", Description: "Path of the ipmitool binary when it is not on the PATH", Example: "/usr/bin/ipmitool"},
			{Key: "interval", Kind: probes.KindInt, Default: 60, Group: "collection", Description: "Seconds between collections"},
			{Key: "exec_timeout", Kind: probes.KindInt, Default: 10, Group: "collection", Description: "Seconds an ipmitool run may take before it is killed"},
		},
	})
}

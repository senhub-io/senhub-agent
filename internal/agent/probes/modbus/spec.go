package modbus

import "senhub-agent.go/internal/agent/probes"

func init() {
	probes.RegisterProbeSpec(probes.ProbeSpec{
		Type: "modbus", DisplayName: "Modbus TCP", Category: "network",
		Summary:  "Decoded holding registers of one Modbus TCP device; one instance per device.",
		DocsPath: "docs/user-guide/docs/probes/modbus.md", MultiInstance: true, DefaultInterval: 30,
		Params: []probes.ParamSpec{
			{Key: "host", Kind: probes.KindString, Required: true, Group: "connection", Description: "Device address or hostname", Example: "192.168.1.100"},
			{Key: "port", Kind: probes.KindInt, Default: 502, Group: "connection", Description: "Modbus TCP port"},
			{Key: "unit_id", Kind: probes.KindInt, Default: 1, Group: "connection", Description: "Modbus unit (slave) identifier"},
			{Key: "timeout", Kind: probes.KindDuration, Default: "10s", Group: "collection", Description: "Per-read timeout, seconds or a duration"},
			{Key: "interval", Kind: probes.KindInt, Default: 30, Group: "collection", Description: "Seconds between collections"},
			{Key: "registers", Kind: probes.KindBlockList, Required: true, Group: "metrics", Description: "Registers to read and how to decode them", Fields: []probes.ParamSpec{
				{Key: "name", Kind: probes.KindString, Required: true, Description: "Register name, the register.name attribute of the value"},
				{Key: "address", Kind: probes.KindInt, Required: true, Description: "Holding-register address, Modicon 1-based (40001) or 0-based on non-standard devices"},
				{Key: "type", Kind: probes.KindString, Required: true, Enum: []string{"uint16", "int16", "uint32", "int32", "float32_abcd", "float32_cdab"}, Description: "How the raw bytes are decoded"},
				{Key: "scale", Kind: probes.KindFloat, Default: 1.0, Description: "Multiplier applied after decoding; 0 counts as 1"},
				{Key: "unit", Kind: probes.KindString, Default: "1", Description: "OTel unit of the value", Example: "Cel"},
				{Key: "description", Kind: probes.KindString, Description: "Human-readable label for dashboards"},
			}},
		},
	})
}

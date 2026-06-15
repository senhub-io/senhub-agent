package modbus

import (
	"fmt"
	"time"
)

// Register type identifiers accepted in YAML config.
const (
	typeUint16     = "uint16"
	typeInt16      = "int16"
	typeUint32     = "uint32"
	typeInt32      = "int32"
	typeFloat32ABCD = "float32_abcd"
	typeFloat32CDAB = "float32_cdab"
)

// registerConfig holds the per-register reading parameters.
type registerConfig struct {
	// Name is the OTel attribute value for register.name and the discriminant
	// in multi_instance_labels.
	Name string
	// Address is the Modicon 1-based holding-register address (40001–49999)
	// or a 0-based address for non-standard devices.
	Address uint16
	// Type controls how the raw bytes are decoded.
	Type string
	// Scale is a multiplier applied after decoding (default 1.0).
	Scale float32
	// Unit is the OTel unit string emitted in the transformer (e.g. "Cel", "1", "%").
	Unit string
	// Description is a human-readable label for dashboards.
	Description string
}

// probeConfig is the validated config for one Modbus probe instance.
type probeConfig struct {
	Host      string
	Port      int
	UnitID    int
	Timeout   time.Duration
	Interval  time.Duration
	Registers []registerConfig
}

func parseConfig(raw map[string]interface{}) (probeConfig, error) {
	cfg := probeConfig{
		Port:     defaultPort,
		UnitID:   defaultUnitID,
		Timeout:  defaultTimeout,
		Interval: defaultInterval,
	}

	host, ok := raw["host"].(string)
	if !ok || host == "" {
		return cfg, fmt.Errorf("modbus: host is required")
	}
	cfg.Host = host

	if v, ok := raw["port"].(int); ok && v > 0 {
		cfg.Port = v
	}
	if v, ok := raw["unit_id"].(int); ok && v >= 0 {
		cfg.UnitID = v
	}
	if v, ok := raw["timeout"].(string); ok && v != "" {
		d, err := time.ParseDuration(v)
		if err != nil {
			return cfg, fmt.Errorf("modbus: invalid timeout %q: %w", v, err)
		}
		cfg.Timeout = d
	} else if v, ok := raw["timeout"].(int); ok && v > 0 {
		cfg.Timeout = time.Duration(v) * time.Second
	}
	if v, ok := raw["interval"].(int); ok && v > 0 {
		cfg.Interval = time.Duration(v) * time.Second
	}

	rawRegs, ok := raw["registers"]
	if !ok {
		return cfg, fmt.Errorf("modbus: registers list is required")
	}
	regs, ok := rawRegs.([]interface{})
	if !ok {
		return cfg, fmt.Errorf("modbus: registers must be a list")
	}
	if len(regs) == 0 {
		return cfg, fmt.Errorf("modbus: registers list must not be empty")
	}

	for i, item := range regs {
		m, ok := item.(map[string]interface{})
		if !ok {
			return cfg, fmt.Errorf("modbus: registers[%d]: must be a mapping", i)
		}
		reg, err := parseRegister(i, m)
		if err != nil {
			return cfg, err
		}
		cfg.Registers = append(cfg.Registers, reg)
	}
	return cfg, nil
}

func parseRegister(i int, m map[string]interface{}) (registerConfig, error) {
	reg := registerConfig{
		Scale: 1.0,
		Unit:  "1",
	}

	name, ok := m["name"].(string)
	if !ok || name == "" {
		return reg, fmt.Errorf("modbus: registers[%d]: name is required", i)
	}
	reg.Name = name

	addrRaw, ok := m["address"]
	if !ok {
		return reg, fmt.Errorf("modbus: registers[%d] (%s): address is required", i, name)
	}
	switch v := addrRaw.(type) {
	case int:
		if v < 0 || v > 65535 {
			return reg, fmt.Errorf("modbus: registers[%d] (%s): address %d out of range [0, 65535]", i, name, v)
		}
		reg.Address = uint16(v) // #nosec G115 - bounds checked above
	default:
		return reg, fmt.Errorf("modbus: registers[%d] (%s): address must be an integer", i, name)
	}

	typ, ok := m["type"].(string)
	if !ok || typ == "" {
		return reg, fmt.Errorf("modbus: registers[%d] (%s): type is required", i, name)
	}
	if !validRegisterType(typ) {
		return reg, fmt.Errorf("modbus: registers[%d] (%s): unsupported type %q (valid: uint16, int16, uint32, int32, float32_abcd, float32_cdab)", i, name, typ)
	}
	reg.Type = typ

	if v, ok := m["scale"].(float64); ok {
		reg.Scale = float32(v)
	} else if v, ok := m["scale"].(int); ok {
		reg.Scale = float32(v)
	}
	if reg.Scale == 0 {
		reg.Scale = 1.0
	}

	if v, ok := m["unit"].(string); ok && v != "" {
		reg.Unit = v
	}
	if v, ok := m["description"].(string); ok {
		reg.Description = v
	}

	return reg, nil
}

func validRegisterType(t string) bool {
	switch t {
	case typeUint16, typeInt16, typeUint32, typeInt32, typeFloat32ABCD, typeFloat32CDAB:
		return true
	}
	return false
}

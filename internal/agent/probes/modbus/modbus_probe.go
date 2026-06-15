// Package modbus implements the free modbus probe: Modbus TCP register
// polling for IT/OT convergence — PLCs, industrial sensors, smart-building
// controllers. One metric per configured register (modbus.register.value),
// discriminated by register.name and register.address so a single probe
// instance covers a whole device.
package modbus

import (
	"context"
	"encoding/binary"
	"fmt"
	"math"
	"strconv"
	"time"

	modbuslib "github.com/goburrow/modbus"

	"senhub-agent.go/internal/agent/probes/types"
	"senhub-agent.go/internal/agent/services/data_store"
	"senhub-agent.go/internal/agent/services/entity"
	"senhub-agent.go/internal/agent/services/logger"
	"senhub-agent.go/internal/agent/tags"
)

// ProbeType is the stable technical identifier used in probe YAML config.
const ProbeType = "modbus"

const (
	defaultTimeout  = 10 * time.Second
	defaultInterval = 30 * time.Second
	defaultPort     = 502
	defaultUnitID   = 1
)

// ModbusProbe polls one Modbus TCP slave per cycle.
type ModbusProbe struct {
	*types.BaseProbe
	cfg          probeConfig
	instance     string
	moduleLogger *logger.ModuleLogger
	entitySource *modbusEntitySource

	unregisterEntitySource func()

	// newClient is overridable in tests.
	newClient func(cfg probeConfig) modbusClient
}

// NewModbusProbe builds a modbus probe from its raw params block.
func NewModbusProbe(rawConfig map[string]interface{}, baseLogger *logger.Logger) (types.Probe, error) {
	moduleLogger := logger.NewModuleLogger(baseLogger, "probe.modbus")

	cfg, err := parseConfig(rawConfig)
	if err != nil {
		return nil, err
	}

	instance := fmt.Sprintf("modbus://%s:%d", cfg.Host, cfg.Port)

	probe := &ModbusProbe{
		BaseProbe:    &types.BaseProbe{},
		cfg:          cfg,
		instance:     instance,
		moduleLogger: moduleLogger,
		entitySource: newModbusEntitySource(instance),
		newClient:    newGoburrowClient,
	}
	probe.SetProbeType(ProbeType)
	return probe, nil
}

func (p *ModbusProbe) ShouldStart() bool          { return true }
func (p *ModbusProbe) GetInterval() time.Duration  { return p.cfg.Interval }
func (p *ModbusProbe) GetTargetStrategies() []string {
	return []string{"senhub", "prtg", "http", "otlp"}
}

// OnStart registers the entity source so Toise discovers the Modbus device.
func (p *ModbusProbe) OnStart(_ chan struct{}) error {
	p.unregisterEntitySource = entity.RegisterSource(p.entitySource)
	p.moduleLogger.Info().
		Str("host", p.cfg.Host).
		Int("port", p.cfg.Port).
		Int("unit_id", p.cfg.UnitID).
		Int("registers", len(p.cfg.Registers)).
		Msg("Modbus TCP probe started")
	return nil
}

// OnShutdown unregisters the entity source.
func (p *ModbusProbe) OnShutdown(_ context.Context) error {
	if p.unregisterEntitySource != nil {
		p.unregisterEntitySource()
	}
	return nil
}

// Collect reads all configured holding registers in one TCP session and
// emits one datapoint per register. A connection or read failure sets
// modbus.up=0 — the outage is always visible, never a collection error.
func (p *ModbusProbe) Collect() ([]data_store.DataPoint, error) {
	now := time.Now()
	client := p.newClient(p.cfg)

	upTags := []tags.Tag{
		{Key: "host", Value: p.cfg.Host},
		{Key: "modbus.unit_id", Value: strconv.Itoa(p.cfg.UnitID)},
		{Key: "metric_type", Value: "status"},
	}

	if err := client.Connect(); err != nil {
		p.moduleLogger.Warn().Err(err).Str("instance", p.instance).Msg("Modbus connect failed")
		dp := data_store.DataPoint{
			Name: "modbus.up", Value: 0, Timestamp: now, Tags: upTags,
		}
		return p.BaseProbe.EnrichDataPointsWithProbeName([]data_store.DataPoint{dp}, p.GetName()), nil
	}
	defer func() {
		if err := client.Close(); err != nil {
			p.moduleLogger.Warn().Err(err).Msg("error closing Modbus connection")
		}
	}()

	var points []data_store.DataPoint
	allOK := true

	for _, reg := range p.cfg.Registers {
		val, err := readRegister(client, reg)
		if err != nil {
			p.moduleLogger.Warn().
				Err(err).
				Str("register", reg.Name).
				Uint16("address", reg.Address).
				Msg("Modbus register read failed")
			allOK = false
			continue
		}

		regTags := []tags.Tag{
			{Key: "register.name", Value: reg.Name},
			{Key: "register.address", Value: strconv.Itoa(int(reg.Address))},
			{Key: "modbus.unit_id", Value: strconv.Itoa(p.cfg.UnitID)},
			{Key: "metric_type", Value: "sensor"},
		}
		points = append(points, data_store.DataPoint{
			Name:      "modbus.register.value",
			Value:     val,
			Timestamp: now,
			Tags:      regTags,
		})
	}

	up := float32(1)
	if !allOK {
		up = 0
	}
	points = append(points, data_store.DataPoint{
		Name: "modbus.up", Value: up, Timestamp: now, Tags: upTags,
	})

	// Mark entity as live after a successful connection cycle.
	p.entitySource.markLive()

	return p.BaseProbe.EnrichDataPointsWithProbeName(points, p.GetName()), nil
}

// readRegister reads one or two holding registers and decodes the value
// according to the register's configured type.
//
// Modbus holding-register addresses in config are 1-based Modicon notation
// (40001 → holding register 0). We convert to 0-based before the read.
func readRegister(client modbusClient, reg registerConfig) (float32, error) {
	addr := zeroBasedAddress(reg.Address)

	var raw []byte
	var err error

	switch reg.Type {
	case typeUint16, typeInt16:
		raw, err = client.ReadHoldingRegisters(addr, 1)
	case typeUint32, typeInt32, typeFloat32ABCD, typeFloat32CDAB:
		raw, err = client.ReadHoldingRegisters(addr, 2)
	default:
		return 0, fmt.Errorf("unsupported register type %q", reg.Type)
	}
	if err != nil {
		return 0, fmt.Errorf("ReadHoldingRegisters addr=%d: %w", addr, err)
	}

	decoded, err := decodeBytes(raw, reg.Type)
	if err != nil {
		return 0, err
	}

	return float32(decoded * float64(reg.Scale)), nil
}

// zeroBasedAddress converts a Modicon 1-based Holding Register address
// (40001–49999) to the 0-based address used on the wire. If the address
// is already 0-based (< 40001) it is returned unchanged.
func zeroBasedAddress(addr uint16) uint16 {
	if addr >= 40001 {
		return addr - 40001
	}
	return addr
}

// decodeBytes converts the raw bytes returned by the Modbus library
// (big-endian on the wire, as per Modbus spec) into a float64.
func decodeBytes(raw []byte, typ string) (float64, error) {
	switch typ {
	case typeUint16:
		if len(raw) < 2 {
			return 0, fmt.Errorf("uint16 decode: need 2 bytes, got %d", len(raw))
		}
		return float64(binary.BigEndian.Uint16(raw)), nil

	case typeInt16:
		if len(raw) < 2 {
			return 0, fmt.Errorf("int16 decode: need 2 bytes, got %d", len(raw))
		}
		return float64(int16(binary.BigEndian.Uint16(raw))), nil

	case typeUint32:
		if len(raw) < 4 {
			return 0, fmt.Errorf("uint32 decode: need 4 bytes, got %d", len(raw))
		}
		return float64(binary.BigEndian.Uint32(raw)), nil

	case typeInt32:
		if len(raw) < 4 {
			return 0, fmt.Errorf("int32 decode: need 4 bytes, got %d", len(raw))
		}
		return float64(int32(binary.BigEndian.Uint32(raw))), nil

	case typeFloat32ABCD:
		// Standard big-endian float32: bytes ABCD in wire order.
		if len(raw) < 4 {
			return 0, fmt.Errorf("float32_abcd decode: need 4 bytes, got %d", len(raw))
		}
		bits := binary.BigEndian.Uint32(raw)
		return float64(math.Float32frombits(bits)), nil

	case typeFloat32CDAB:
		// Middle-endian "CDAB" swapped word order: words are big-endian
		// individually but their order is reversed.
		if len(raw) < 4 {
			return 0, fmt.Errorf("float32_cdab decode: need 4 bytes, got %d", len(raw))
		}
		// raw = [C D A B] → swap to [A B C D]
		swapped := []byte{raw[2], raw[3], raw[0], raw[1]}
		bits := binary.BigEndian.Uint32(swapped)
		return float64(math.Float32frombits(bits)), nil

	default:
		return 0, fmt.Errorf("unknown register type %q", typ)
	}
}

// modbusClient is the interface we use for the Modbus connection, allowing
// test injection without a real TCP server.
type modbusClient interface {
	Connect() error
	Close() error
	ReadHoldingRegisters(address, quantity uint16) ([]byte, error)
}

// goburrowClientWrapper adapts the goburrow/modbus library to our interface.
type goburrowClientWrapper struct {
	handler *modbuslib.TCPClientHandler
	client  modbuslib.Client
}

func newGoburrowClient(cfg probeConfig) modbusClient {
	addr := fmt.Sprintf("%s:%d", cfg.Host, cfg.Port)
	handler := modbuslib.NewTCPClientHandler(addr)
	handler.Timeout = cfg.Timeout
	handler.SlaveId = byte(cfg.UnitID) // #nosec G115 - UnitID is 1-247 by spec
	return &goburrowClientWrapper{
		handler: handler,
		client:  modbuslib.NewClient(handler),
	}
}

func (w *goburrowClientWrapper) Connect() error {
	return w.handler.Connect()
}

func (w *goburrowClientWrapper) Close() error {
	return w.handler.Close()
}

func (w *goburrowClientWrapper) ReadHoldingRegisters(address, quantity uint16) ([]byte, error) {
	return w.client.ReadHoldingRegisters(address, quantity)
}

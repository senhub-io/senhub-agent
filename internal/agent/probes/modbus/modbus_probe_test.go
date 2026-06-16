package modbus

import (
	"fmt"
	"testing"
	"time"

	"senhub-agent.go/internal/agent/cliArgs"
	"senhub-agent.go/internal/agent/services/logger"
)

// fakeClient is a test double for the Modbus TCP connection.
type fakeClient struct {
	connectErr error
	readErr    error
	// responses maps address→raw bytes returned by ReadHoldingRegisters.
	responses map[uint16][]byte
}

func (f *fakeClient) Connect() error { return f.connectErr }
func (f *fakeClient) Close() error   { return nil }
func (f *fakeClient) ReadHoldingRegisters(address, _ uint16) ([]byte, error) {
	if f.readErr != nil {
		return nil, f.readErr
	}
	b, ok := f.responses[address]
	if !ok {
		return nil, fmt.Errorf("no stub for address %d", address)
	}
	return b, nil
}

func testLogger(t *testing.T) *logger.Logger {
	t.Helper()
	return logger.NewLogger(&cliArgs.ParsedArgs{Env: "test"})
}

func TestParseConfig_MissingHost(t *testing.T) {
	_, err := NewModbusProbe(map[string]interface{}{}, testLogger(t))
	if err == nil {
		t.Fatal("expected error for missing host")
	}
}

func TestParseConfig_MissingRegisters(t *testing.T) {
	_, err := NewModbusProbe(map[string]interface{}{"host": "192.168.1.1"}, testLogger(t))
	if err == nil {
		t.Fatal("expected error for missing registers")
	}
}

func TestParseConfig_InvalidRegisterType(t *testing.T) {
	_, err := NewModbusProbe(map[string]interface{}{
		"host": "192.168.1.1",
		"registers": []interface{}{
			map[string]interface{}{
				"name":    "foo",
				"address": 40001,
				"type":    "bad_type",
			},
		},
	}, testLogger(t))
	if err == nil {
		t.Fatal("expected error for invalid register type")
	}
}

func TestParseConfig_Valid(t *testing.T) {
	probe, err := NewModbusProbe(map[string]interface{}{
		"host":     "192.168.1.100",
		"port":     502,
		"unit_id":  1,
		"timeout":  "10s",
		"interval": 30,
		"registers": []interface{}{
			map[string]interface{}{
				"name":    "temperature_zone1",
				"address": 40001,
				"type":    "float32_abcd",
				"scale":   0.1,
				"unit":    "Cel",
			},
		},
	}, testLogger(t))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	p := probe.(*ModbusProbe)
	if p.cfg.Host != "192.168.1.100" {
		t.Errorf("host = %q, want 192.168.1.100", p.cfg.Host)
	}
	if len(p.cfg.Registers) != 1 {
		t.Errorf("registers len = %d, want 1", len(p.cfg.Registers))
	}
	if p.cfg.Timeout != 10*time.Second {
		t.Errorf("timeout = %v, want 10s", p.cfg.Timeout)
	}
}

func TestCollect_ConnectError(t *testing.T) {
	probe, err := NewModbusProbe(map[string]interface{}{
		"host": "192.168.1.100",
		"registers": []interface{}{
			map[string]interface{}{"name": "r1", "address": 40001, "type": "uint16"},
		},
	}, testLogger(t))
	if err != nil {
		t.Fatalf("NewModbusProbe: %v", err)
	}
	p := probe.(*ModbusProbe)
	p.SetName("modbus-test")
	p.newClient = func(_ probeConfig) modbusClient {
		return &fakeClient{connectErr: fmt.Errorf("connection refused")}
	}

	points, err := p.Collect()
	if err != nil {
		t.Fatalf("Collect returned error: %v", err)
	}

	var upVal *float64
	for _, dp := range points {
		if dp.Name == "modbus.up" {
			v := dp.Value
			upVal = &v
		}
	}
	if upVal == nil {
		t.Fatal("modbus.up not found in points")
	}
	if *upVal != 0 {
		t.Errorf("modbus.up = %v, want 0 on connect error", *upVal)
	}
}

func TestCollect_Success_Uint16(t *testing.T) {
	probe, err := NewModbusProbe(map[string]interface{}{
		"host": "192.168.1.100",
		"registers": []interface{}{
			map[string]interface{}{
				"name":    "pump_status",
				"address": 40010,
				"type":    "uint16",
				"scale":   1,
			},
		},
	}, testLogger(t))
	if err != nil {
		t.Fatalf("NewModbusProbe: %v", err)
	}
	p := probe.(*ModbusProbe)
	p.SetName("modbus-test")
	p.newClient = func(_ probeConfig) modbusClient {
		return &fakeClient{
			responses: map[uint16][]byte{
				// address 40010 → 0-based = 9; value = 42 = 0x002A
				9: {0x00, 0x2A},
			},
		}
	}

	points, err := p.Collect()
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}

	var got *float64
	for _, dp := range points {
		if dp.Name == "modbus.register.value" {
			v := dp.Value
			got = &v
		}
	}
	if got == nil {
		t.Fatal("modbus.register.value not found")
	}
	if *got != 42 {
		t.Errorf("register value = %v, want 42", *got)
	}
}

func TestCollect_Success_Float32ABCD(t *testing.T) {
	// IEEE 754 big-endian representation of 23.5
	// math.Float32bits(23.5) = 0x41BC0000
	raw := []byte{0x41, 0xBC, 0x00, 0x00}

	probe, err := NewModbusProbe(map[string]interface{}{
		"host": "192.168.1.100",
		"registers": []interface{}{
			map[string]interface{}{
				"name":    "temperature",
				"address": 40001,
				"type":    "float32_abcd",
				"scale":   1,
			},
		},
	}, testLogger(t))
	if err != nil {
		t.Fatalf("NewModbusProbe: %v", err)
	}
	p := probe.(*ModbusProbe)
	p.SetName("modbus-test")
	p.newClient = func(_ probeConfig) modbusClient {
		return &fakeClient{
			responses: map[uint16][]byte{0: raw},
		}
	}

	points, err := p.Collect()
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}

	var got *float64
	for _, dp := range points {
		if dp.Name == "modbus.register.value" {
			v := dp.Value
			got = &v
		}
	}
	if got == nil {
		t.Fatal("modbus.register.value not found")
	}
	const want = float64(23.5)
	const eps = float64(0.001)
	if *got < want-eps || *got > want+eps {
		t.Errorf("float32_abcd value = %v, want ~23.5", *got)
	}
}

func TestCollect_Success_Scale(t *testing.T) {
	// Raw uint16 value = 235; scale = 0.1 → expected 23.5
	probe, err := NewModbusProbe(map[string]interface{}{
		"host": "192.168.1.100",
		"registers": []interface{}{
			map[string]interface{}{
				"name":    "temperature",
				"address": 40001,
				"type":    "uint16",
				"scale":   0.1,
			},
		},
	}, testLogger(t))
	if err != nil {
		t.Fatalf("NewModbusProbe: %v", err)
	}
	p := probe.(*ModbusProbe)
	p.SetName("modbus-test")
	p.newClient = func(_ probeConfig) modbusClient {
		return &fakeClient{
			// 235 = 0x00EB
			responses: map[uint16][]byte{0: {0x00, 0xEB}},
		}
	}

	points, err := p.Collect()
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}

	var got *float64
	for _, dp := range points {
		if dp.Name == "modbus.register.value" {
			v := dp.Value
			got = &v
		}
	}
	if got == nil {
		t.Fatal("modbus.register.value not found")
	}
	const want = float64(23.5)
	const eps = float64(0.001)
	if *got < want-eps || *got > want+eps {
		t.Errorf("scaled value = %v, want ~23.5", *got)
	}
}

func TestDecodeBytes_AllTypes(t *testing.T) {
	cases := []struct {
		name string
		raw  []byte
		typ  string
		want float64
	}{
		{"uint16", []byte{0x00, 0x2A}, typeUint16, 42},
		{"int16 negative", []byte{0xFF, 0xD6}, typeInt16, -42},
		{"uint32", []byte{0x00, 0x01, 0x00, 0x00}, typeUint32, 65536},
		{"int32 negative", []byte{0xFF, 0xFF, 0xFF, 0xD6}, typeInt32, -42},
		// math.Float32bits(23.5) = 0x41BC0000
		{"float32_abcd", []byte{0x41, 0xBC, 0x00, 0x00}, typeFloat32ABCD, 23.5},
		// CDAB = words swapped: [0xBC,0x00] [0x41,0x00] (wrong: let's use correct)
		// ABCD = [0x41,0xBC,0x00,0x00]; CDAB wire order = [0x00,0x00,0x41,0xBC]
		{"float32_cdab", []byte{0x00, 0x00, 0x41, 0xBC}, typeFloat32CDAB, 23.5},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := decodeBytes(tc.raw, tc.typ)
			if err != nil {
				t.Fatalf("decodeBytes: %v", err)
			}
			const eps = 0.001
			if got < tc.want-eps || got > tc.want+eps {
				t.Errorf("decodeBytes(%s) = %v, want %v", tc.typ, got, tc.want)
			}
		})
	}
}

func TestZeroBasedAddress(t *testing.T) {
	cases := []struct {
		addr uint16
		want uint16
	}{
		{40001, 0},
		{40010, 9},
		{40100, 99},
		{0, 0},       // already 0-based
		{1000, 1000}, // also already 0-based
	}
	for _, tc := range cases {
		got := zeroBasedAddress(tc.addr)
		if got != tc.want {
			t.Errorf("zeroBasedAddress(%d) = %d, want %d", tc.addr, got, tc.want)
		}
	}
}

func TestEntitySource_InitiallyNotLive(t *testing.T) {
	src := newModbusEntitySource("modbus://192.168.1.100:502")
	_, ok := src.Observe()
	if ok {
		t.Error("Observe should return ok=false before first successful connect")
	}
}

func TestEntitySource_BecomesLiveAfterMarkLive(t *testing.T) {
	src := newModbusEntitySource("modbus://192.168.1.100:502")
	src.markLive()
	obs, ok := src.Observe()
	if !ok {
		t.Error("Observe should return ok=true after markLive")
	}
	if len(obs.Entities) != 1 {
		t.Fatalf("expected 1 entity, got %d", len(obs.Entities))
	}
	ent := obs.Entities[0]
	if ent.Type != "service.instance" {
		t.Errorf("entity type = %q, want service.instance", ent.Type)
	}
	id, _ := ent.ID["service.instance.id"].(string)
	if id != "modbus://192.168.1.100:502" {
		t.Errorf("entity id = %q, want modbus://192.168.1.100:502", id)
	}
}

package zabbix

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"strings"
	"testing"
)

func TestFrameRoundTrip(t *testing.T) {
	var buf bytes.Buffer
	payload := []byte(`{"request":"active checks","host":"web-01"}`)
	if err := writeFrame(&buf, payload); err != nil {
		t.Fatal(err)
	}
	raw := buf.Bytes()
	if string(raw[:4]) != "ZBXD" || raw[4] != flagProtocol {
		t.Fatalf("header = %q %#x", raw[:4], raw[4])
	}
	if got := binary.LittleEndian.Uint32(raw[5:9]); got != uint32(len(payload)) {
		t.Fatalf("length = %d, want %d", got, len(payload))
	}
	back, err := readFrame(&buf)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(back, payload) {
		t.Fatalf("payload = %q", back)
	}
}

func TestReadFrameInflatesACompressedReply(t *testing.T) {
	body := []byte(`{"response":"success","data":[]}`)
	var z bytes.Buffer
	w := zlib.NewWriter(&z)
	_, _ = w.Write(body)
	_ = w.Close()
	frame := []byte("ZBXD")
	frame = append(frame, flagProtocol|flagCompressed)
	frame = binary.LittleEndian.AppendUint32(frame, uint32(z.Len()))
	frame = binary.LittleEndian.AppendUint32(frame, uint32(len(body)))
	frame = append(frame, z.Bytes()...)

	got, err := readFrame(bytes.NewReader(frame))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, body) {
		t.Fatalf("inflated = %q", got)
	}
}

func TestReadFrameRejectsWhatIsNotZabbix(t *testing.T) {
	_, err := readFrame(strings.NewReader("HTTP/1.1 400 Bad Request\r\n\r\n"))
	if err == nil || !strings.Contains(err.Error(), "not a Zabbix frame") {
		t.Fatalf("err = %v", err)
	}
}

func TestReadFrameRefusesAnOversizedLength(t *testing.T) {
	frame := []byte("ZBXD")
	frame = append(frame, flagProtocol)
	frame = binary.LittleEndian.AppendUint32(frame, maxFrameBytes+1)
	frame = binary.LittleEndian.AppendUint32(frame, 0)
	_, err := readFrame(bytes.NewReader(frame))
	if err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("err = %v", err)
	}
}

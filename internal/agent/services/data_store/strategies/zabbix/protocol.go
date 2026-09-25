package zabbix

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"fmt"
	"io"
)

// Every Zabbix TCP exchange is one frame: "ZBXD", a flags byte, the
// payload length and a reserved field, then the payload. The length and
// the reserved field are 4 bytes each, or 8 each when the large-packet
// flag is set. A compressed frame carries the compressed length and, in
// the reserved field, the uncompressed one.
const (
	frameMagic       = "ZBXD"
	flagProtocol     = 0x01
	flagCompressed   = 0x02
	flagLargePacket  = 0x04
	maxFrameBytes    = 64 << 20
	headerFixedBytes = 5
)

func writeFrame(w io.Writer, payload []byte) error {
	// The reader refuses a frame past maxFrameBytes, so writing one
	// larger would put a packet on the wire that our own side would
	// reject. Checking here also makes the 32-bit length conversion
	// below provably in range.
	if len(payload) > maxFrameBytes {
		return fmt.Errorf("zabbix: frame of %d bytes exceeds the %d limit", len(payload), maxFrameBytes)
	}
	buf := make([]byte, 0, headerFixedBytes+8+len(payload))
	buf = append(buf, frameMagic...)
	buf = append(buf, flagProtocol)
	buf = binary.LittleEndian.AppendUint32(buf, uint32(len(payload))) // #nosec G115 - bounded by maxFrameBytes just above
	buf = binary.LittleEndian.AppendUint32(buf, 0)
	buf = append(buf, payload...)
	_, err := w.Write(buf)
	return err
}

// readFrame reads one frame and returns its payload, inflated when the
// sender compressed it.
func readFrame(r io.Reader) ([]byte, error) {
	head := make([]byte, headerFixedBytes)
	if _, err := io.ReadFull(r, head); err != nil {
		return nil, fmt.Errorf("reading frame header: %w", err)
	}
	if string(head[:4]) != frameMagic {
		return nil, fmt.Errorf("not a Zabbix frame (header %q)", head[:4])
	}
	flags := head[4]
	if flags&flagProtocol == 0 {
		return nil, fmt.Errorf("unsupported frame flags %#x", flags)
	}

	var length, reserved uint64
	if flags&flagLargePacket != 0 {
		sizes := make([]byte, 16)
		if _, err := io.ReadFull(r, sizes); err != nil {
			return nil, fmt.Errorf("reading frame sizes: %w", err)
		}
		length = binary.LittleEndian.Uint64(sizes[:8])
		reserved = binary.LittleEndian.Uint64(sizes[8:])
	} else {
		sizes := make([]byte, 8)
		if _, err := io.ReadFull(r, sizes); err != nil {
			return nil, fmt.Errorf("reading frame sizes: %w", err)
		}
		length = uint64(binary.LittleEndian.Uint32(sizes[:4]))
		reserved = uint64(binary.LittleEndian.Uint32(sizes[4:]))
	}
	if length > maxFrameBytes || reserved > maxFrameBytes {
		return nil, fmt.Errorf("frame of %d bytes exceeds the %d limit", length, maxFrameBytes)
	}

	payload := make([]byte, length)
	if _, err := io.ReadFull(r, payload); err != nil {
		return nil, fmt.Errorf("reading frame payload: %w", err)
	}
	if flags&flagCompressed == 0 {
		return payload, nil
	}
	zr, err := zlib.NewReader(bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("inflating frame: %w", err)
	}
	defer zr.Close()
	inflated, err := io.ReadAll(io.LimitReader(zr, maxFrameBytes+1))
	if err != nil {
		return nil, fmt.Errorf("inflating frame: %w", err)
	}
	if len(inflated) > maxFrameBytes {
		return nil, fmt.Errorf("inflated frame exceeds the %d limit", maxFrameBytes)
	}
	return inflated, nil
}

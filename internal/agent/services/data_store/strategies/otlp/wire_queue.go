package otlp

import (
	"bytes"
	"compress/gzip"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"senhub-agent.go/internal/agent/services/agentstate"
	"senhub-agent.go/internal/agent/services/logger"
)

// On-disk durability for the OTLP metrics and traces signals.
//
// Each export request that could not be delivered is kept as the
// serialized OTLP protobuf the exporter built for it, with its own
// timestamps, in one file. Replay hands the very same bytes back to the
// exporter, oldest file first, and the file leaves the disk only after the
// backend acknowledged that request: at-least-once, like the logs queue.
//
// File layout, big endian:
//
//	0   "SHWQ"            magic
//	4   version (1 byte)
//	5   3 reserved bytes
//	8   saved-at, unix nanoseconds (int64)
//	16  units (uint32)    data points (metrics) or spans (traces)
//	20  gzip(protobuf request)
//
// The header carries what the queue needs without reading the payload: the
// age, and the unit count the depth gauges and the drop counter use. The
// payload is compressed because a metrics request repeats the same names
// and attributes at every point: it shrinks about tenfold, which is what
// lets a size cap in the hundreds of MiB cover hours of outage.

const (
	wireQueueFileVersion = 1
	wireQueueHeaderSize  = 20
	wireQueueExt         = ".q"

	metricsQueueDirName = "metricqueue"
	tracesQueueDirName  = "tracequeue"

	// defaultMetricsQueueMaxBytes caps the metrics queue. A push is a full
	// snapshot of the store, so a busy host writes one large request per
	// interval; 256 MiB keeps hours of a typical host and cannot fill a disk.
	defaultMetricsQueueMaxBytes int64 = 256 << 20
	// defaultTracesQueueMaxBytes caps the traces queue. Spans are smaller
	// and sampled; 64 MiB holds hours of a lightly traced agent.
	defaultTracesQueueMaxBytes int64 = 64 << 20
)

var wireQueueMagic = [4]byte{'S', 'H', 'W', 'Q'}

// wireQueue is the on-disk store of serialized OTLP requests of one signal.
type wireQueue struct {
	diskQueue
	signal string
}

// wireQueueKind describes one signal's queue.
type wireQueueKind struct {
	signal     string
	dirName    string
	prefix     string
	unit       string
	fullReason string
	ageReason  string
	defaultMax int64
}

var (
	metricsQueueKind = wireQueueKind{
		signal: "metrics", dirName: metricsQueueDirName, prefix: "metric", unit: "data points",
		fullReason: "metrics_queue_full", ageReason: "metrics_dropped_by_age", defaultMax: defaultMetricsQueueMaxBytes,
	}
	tracesQueueKind = wireQueueKind{
		signal: "traces", dirName: tracesQueueDirName, prefix: "trace", unit: "spans",
		fullReason: "traces_queue_full", ageReason: "traces_dropped_by_age", defaultMax: defaultTracesQueueMaxBytes,
	}
)

func newWireQueue(kind wireQueueKind, path string, maxBytes int64, log *logger.ModuleLogger) *wireQueue {
	if maxBytes <= 0 {
		maxBytes = kind.defaultMax
	}
	q := &wireQueue{signal: kind.signal}
	q.dir = filepath.Join(path, kind.dirName)
	q.prefix = kind.prefix
	q.ext = wireQueueExt
	q.label = "OTLP " + kind.signal + " queue"
	q.unit = kind.unit
	q.fullReason = kind.fullReason
	q.ageReason = kind.ageReason
	q.maxBytes = maxBytes
	q.logger = log
	q.now = time.Now
	q.inspect = readWireHeader
	signal := kind.signal
	q.onQueued = func(n int) { agentstate.IncrementOTLPQueued(signal, n) }
	q.onSize = func(units int, bytes int64) { agentstate.RecordOTLPQueueSize(signal, units, bytes) }
	q.recover()
	return q
}

func (q *wireQueue) announce() {
	q.diskQueue.announce("OTLP " + q.signal + " queue active: export requests the collector did not take are kept on disk and replayed in order when it answers")
}

// readWireHeader reads the fixed header of a queue file.
func readWireHeader(path string) (time.Time, int, error) {
	f, err := os.Open(path) // #nosec G304 - path built from queue dir
	if err != nil {
		return time.Time{}, 0, err
	}
	defer f.Close()
	var h [wireQueueHeaderSize]byte
	if _, err := io.ReadFull(f, h[:]); err != nil {
		return time.Time{}, 0, fmt.Errorf("queue file header: %w", err)
	}
	return parseWireHeader(h[:])
}

func parseWireHeader(h []byte) (time.Time, int, error) {
	if !bytes.Equal(h[:4], wireQueueMagic[:]) {
		return time.Time{}, 0, errors.New("not a queue file")
	}
	if v := h[4]; v > wireQueueFileVersion {
		return time.Time{}, 0, fmt.Errorf("queue file version %d newer than supported %d", v, wireQueueFileVersion)
	}
	savedAt := time.Unix(0, int64(binary.BigEndian.Uint64(h[8:16])))
	return savedAt, int(binary.BigEndian.Uint32(h[16:20])), nil
}

// encodeWireFile builds a queue file around one serialized request.
func encodeWireFile(savedAt time.Time, units int, request []byte) ([]byte, error) {
	var buf bytes.Buffer
	var h [wireQueueHeaderSize]byte
	copy(h[:4], wireQueueMagic[:])
	h[4] = wireQueueFileVersion
	binary.BigEndian.PutUint64(h[8:16], uint64(savedAt.UnixNano()))
	binary.BigEndian.PutUint32(h[16:20], uint32(units)) // #nosec G115 - bounded by the request size
	buf.Write(h[:])
	zw, err := gzip.NewWriterLevel(&buf, gzip.BestSpeed)
	if err != nil {
		return nil, err
	}
	if _, err := zw.Write(request); err != nil {
		return nil, err
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// enqueue persists one serialized OTLP request carrying units data points
// or spans.
func (q *wireQueue) enqueue(request []byte, units int) error {
	if len(request) == 0 {
		return nil
	}
	data, err := encodeWireFile(q.now(), units, request)
	if err != nil {
		return fmt.Errorf("encode %s request: %w", q.signal, err)
	}
	return q.commit(data, units)
}

// readRequest returns the serialized request held by a queue file.
func (q *wireQueue) readRequest(name string) (request []byte, units int, err error) {
	data, err := os.ReadFile(filepath.Join(q.dir, name)) // #nosec G304 - path built from queue dir
	if err != nil {
		return nil, 0, err
	}
	if len(data) < wireQueueHeaderSize {
		return nil, 0, errors.New("queue file truncated")
	}
	_, units, err = parseWireHeader(data[:wireQueueHeaderSize])
	if err != nil {
		return nil, 0, err
	}
	zr, err := gzip.NewReader(bytes.NewReader(data[wireQueueHeaderSize:]))
	if err != nil {
		return nil, 0, fmt.Errorf("queue file payload: %w", err)
	}
	defer zr.Close()
	request, err = io.ReadAll(zr)
	if err != nil {
		return nil, 0, fmt.Errorf("queue file payload: %w", err)
	}
	return request, units, nil
}

// drainAcked sends every queued request in FIFO order and removes a file
// only once send returned nil, which is the backend's acknowledgement. The
// first failure stops the drain and leaves that file and every later one on
// disk. Returns the number of units acknowledged.
func (q *wireQueue) drainAcked(send func(request []byte, units int) error) (int, error) {
	replayed := 0
	for _, name := range q.files() {
		request, units, err := q.readRequest(name)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue // evicted by the size cap or the age sweep meanwhile
			}
			// Corrupt or too-new file: drop it so it can't wedge the queue.
			q.removeFile(name)
			if q.logger != nil {
				q.logger.Warn().Err(err).Str("file", name).Msg(q.label + ": dropping unreadable file")
			}
			continue
		}
		switch sendErr := send(request, units); {
		case sendErr == nil:
			replayed += units
			agentstate.IncrementOTLPReplayed(q.signal, units)
			q.removeFile(name)
		case errors.Is(sendErr, errBatchDiscarded):
			q.removeFile(name)
		default:
			return replayed, sendErr
		}
	}
	return replayed, nil
}

package otlp

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"go.opentelemetry.io/otel/sdk/resource"

	"senhub-agent.go/internal/agent/services/agentstate"
)

// The dead-letter queue is for a failed export only. With a healthy
// endpoint it must leave nothing on disk, however many records pass and
// however often the replayer looks.
func TestLogsQueue_HealthyEndpointWritesNothing(t *testing.T) {
	dir := t.TempDir()
	exp := &controllableExporter{}
	q := newLogsQueue(dir, 0, testModuleLogger(t))
	ple := newPersistentLogExporter(exp, q, testModuleLogger(t))

	cfg := LogsSignal{BufferSize: 100, BatchSize: 1, BatchTimeout: time.Hour}
	pipe := buildLogsPipeline(ple, resource.NewSchemaless(), cfg, "test")
	replayer := newLogsReplayer(q, pipe, testModuleLogger(t))

	ctx := context.Background()
	for i := 0; i < 20; i++ {
		pipe.emit(ctx, agentstate.LogRecord{
			Timestamp:         time.Unix(1700000000+int64(i), 0),
			Severity:          9,
			SeverityText:      "INFO",
			Body:              "healthy",
			ProducerProbeName: "syslog",
		})
		_ = pipe.provider.ForceFlush(ctx)
	}
	replayer.replay()
	q.sweepAged()

	if exp.captured() != 20 {
		t.Fatalf("exported %d records, want 20", exp.captured())
	}
	if n, _ := q.pending(); n != 0 {
		t.Errorf("%d records pending in the queue of a healthy endpoint", n)
	}
	var written []string
	_ = filepath.WalkDir(filepath.Join(dir, logsQueueDirName), func(path string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			written = append(written, path)
		}
		return nil
	})
	if len(written) != 0 {
		t.Errorf("a healthy endpoint left files in the queue directory: %v", written)
	}
}

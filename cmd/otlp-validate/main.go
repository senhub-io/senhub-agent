// Command otlp-validate is a one-off validation tool that ships a few
// log records over OTLP/gRPC to verify the wire path
//
//	agent host → SDK → otlploggrpc → collector → VictoriaLogs
//
// independent of the running agent.
//
// Used in the OTLP feature's Phase 4 live validation against a sha901
// otel-collector reached through an SSH forward tunnel:
//
//	ssh -i ~/.ssh/sfadmin_rsa -p 5511 -L 4317:127.0.0.1:4317 \
//	    sfadmin@51.255.49.247 -N &
//	go run ./cmd/otlp-validate -endpoint=127.0.0.1:4317
//
// Then query the receiver:
//
//	curl -G http://victorialogs.host:9428/select/logsql/query \
//	     --data-urlencode 'query=service.name:"senhub-agent-otlp-test"'
//
// Not part of the production binary; not published in any release tar.
// Kept in tree as documentation of how to reproduce the validation.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploggrpc"
	"go.opentelemetry.io/otel/log"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	"go.opentelemetry.io/otel/sdk/resource"
)

func main() {
	endpoint := flag.String("endpoint", "127.0.0.1:4317", "OTLP/gRPC endpoint")
	count := flag.Int("count", 3, "number of test log records to publish")
	service := flag.String("service", "senhub-agent-otlp-test", "service.name resource attribute")
	flag.Parse()

	ctx := context.Background()
	exp, err := otlploggrpc.New(ctx,
		otlploggrpc.WithEndpoint(*endpoint),
		otlploggrpc.WithInsecure(),
		otlploggrpc.WithTimeout(5*time.Second),
	)
	if err != nil {
		fmt.Fprintf(os.Stderr, "build exporter: %v\n", err)
		os.Exit(1)
	}

	res := resource.NewSchemaless(
		attribute.String("service.name", *service),
		attribute.String("deployment.environment", "validation"),
	)

	processor := sdklog.NewBatchProcessor(exp,
		sdklog.WithExportInterval(500*time.Millisecond),
	)
	provider := sdklog.NewLoggerProvider(
		sdklog.WithResource(res),
		sdklog.WithProcessor(processor),
	)
	logger := provider.Logger("phase4-validate", log.WithInstrumentationVersion("0.1.88-beta"))

	for i := 0; i < *count; i++ {
		var rec log.Record
		now := time.Now()
		rec.SetTimestamp(now)
		rec.SetObservedTimestamp(now)
		rec.SetSeverity(log.SeverityWarn1)
		rec.SetSeverityText("WARN")
		rec.SetBody(log.StringValue(fmt.Sprintf("Phase4 OTLP validation log #%d", i+1)))
		rec.AddAttributes(
			log.String("validation.phase", "4"),
			log.String("validation.idx", fmt.Sprintf("%d", i+1)),
			log.String("senhub.probe.name", "phase4-validate"),
		)
		logger.Emit(ctx, rec)
	}

	shutCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := provider.Shutdown(shutCtx); err != nil {
		fmt.Fprintf(os.Stderr, "shutdown: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("ok")
}

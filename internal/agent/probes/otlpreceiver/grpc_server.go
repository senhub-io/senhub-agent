package otlpreceiver

import (
	"context"
	"fmt"
	"net"

	"google.golang.org/grpc"

	collectormetricspb "go.opentelemetry.io/proto/otlp/collector/metrics/v1"
)

// metricsServiceServer implements the OTLP MetricsService.Export RPC by
// flattening the incoming metrics and handing them to the probe's
// ingest path.
type metricsServiceServer struct {
	collectormetricspb.UnimplementedMetricsServiceServer
	probe *OTLPReceiverProbe
}

func (s *metricsServiceServer) Export(
	ctx context.Context,
	req *collectormetricspb.ExportMetricsServiceRequest,
) (*collectormetricspb.ExportMetricsServiceResponse, error) {
	points, dropped := flattenResourceMetrics(req.GetResourceMetrics())
	if err := s.probe.ingest(points, dropped); err != nil {
		return nil, err
	}

	resp := &collectormetricspb.ExportMetricsServiceResponse{}
	if dropped > 0 {
		resp.PartialSuccess = &collectormetricspb.ExportMetricsPartialSuccess{
			RejectedDataPoints: int64(dropped),
			ErrorMessage:       "non-scalar metric types (histogram/summary) are not ingested by senhub-agent",
		}
	}
	return resp, nil
}

func (p *OTLPReceiverProbe) startGRPC(quitChannel chan struct{}) error {
	lis, err := net.Listen("tcp", p.config.Address)
	if err != nil {
		return fmt.Errorf("listening on %s: %w", p.config.Address, err)
	}

	server := grpc.NewServer(grpc.MaxRecvMsgSize(maxRecvMsgBytes))
	collectormetricspb.RegisterMetricsServiceServer(server, &metricsServiceServer{probe: p})

	p.mu.Lock()
	p.grpcServer = server
	p.listener = lis
	p.mu.Unlock()

	go func() {
		if serveErr := server.Serve(lis); serveErr != nil && serveErr != grpc.ErrServerStopped {
			p.moduleLogger.Error().Err(serveErr).Msg("OTLP gRPC server stopped with error")
		}
	}()

	go func() {
		<-quitChannel
		p.moduleLogger.Info().Msg("Received quit signal, stopping OTLP gRPC receiver")
		server.GracefulStop()
	}()

	p.moduleLogger.Info().Str("address", p.config.Address).Msg("OTLP gRPC receiver started")
	return nil
}

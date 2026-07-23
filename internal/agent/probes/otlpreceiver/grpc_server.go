package otlpreceiver

import (
	"context"
	"errors"
	"fmt"
	"net"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"

	collectorlogspb "go.opentelemetry.io/proto/otlp/collector/logs/v1"
	collectormetricspb "go.opentelemetry.io/proto/otlp/collector/metrics/v1"
	collectortracepb "go.opentelemetry.io/proto/otlp/collector/trace/v1"
)

// metricsServiceServer implements the OTLP MetricsService.Export RPC by
// flattening the incoming metrics and handing them to the probe's
// ingest path.
type metricsServiceServer struct {
	collectormetricspb.UnimplementedMetricsServiceServer
	probe *OTLPReceiverProbe
}

// logsServiceServer implements the OTLP LogsService.Export RPC by
// converting the received records into the agent's internal log envelope
// and publishing them on the agent log channel for relay.
type logsServiceServer struct {
	collectorlogspb.UnimplementedLogsServiceServer
	probe *OTLPReceiverProbe
}

// tracesServiceServer implements the OTLP TracesService.Export RPC by
// publishing the received ResourceSpans verbatim on the agent span
// channel — spans have no internal model, so no decode step exists.
type tracesServiceServer struct {
	collectortracepb.UnimplementedTraceServiceServer
	probe *OTLPReceiverProbe
}

func (s *logsServiceServer) Export(
	_ context.Context,
	req *collectorlogspb.ExportLogsServiceRequest,
) (*collectorlogspb.ExportLogsServiceResponse, error) {
	s.probe.ingestLogs(flattenResourceLogs(req.GetResourceLogs(), s.probe.GetName()))
	return &collectorlogspb.ExportLogsServiceResponse{}, nil
}

func (s *tracesServiceServer) Export(
	_ context.Context,
	req *collectortracepb.ExportTraceServiceRequest,
) (*collectortracepb.ExportTraceServiceResponse, error) {
	s.probe.ingestSpans(req.GetResourceSpans())
	return &collectortracepb.ExportTraceServiceResponse{}, nil
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
			ErrorMessage:       "unrecognized or unset OTLP metric data type not ingested by senhub-agent",
		}
	}
	return resp, nil
}

// guardInterceptor enforces the optional ingress protections (bearer
// token, CIDR allow-list, rate limit) before any RPC handler runs.
func (p *OTLPReceiverProbe) guardInterceptor(
	ctx context.Context,
	req interface{},
	info *grpc.UnaryServerInfo,
	handler grpc.UnaryHandler,
) (interface{}, error) {
	if p.guard != nil {
		remote := ""
		if pr, ok := peer.FromContext(ctx); ok && pr.Addr != nil {
			remote = pr.Addr.String()
		}
		auth := ""
		if md, ok := metadata.FromIncomingContext(ctx); ok {
			if vals := md.Get("authorization"); len(vals) > 0 {
				auth = vals[0]
			}
		}
		if err := p.guard.allow(remote, auth); err != nil {
			p.logRejection(remote, err)
			switch {
			case errors.Is(err, errUnauthorized):
				return nil, status.Error(codes.Unauthenticated, err.Error())
			case errors.Is(err, errForbidden):
				return nil, status.Error(codes.PermissionDenied, err.Error())
			case errors.Is(err, errRateLimited):
				return nil, status.Error(codes.ResourceExhausted, err.Error())
			default:
				return nil, status.Error(codes.Internal, "ingress guard error")
			}
		}
	}
	return handler(ctx, req)
}

func (p *OTLPReceiverProbe) startGRPC(quitChannel chan struct{}) error {
	lis, err := net.Listen("tcp", p.config.Address)
	if err != nil {
		return fmt.Errorf("listening on %s: %w", p.config.Address, err)
	}

	server := grpc.NewServer(
		grpc.MaxRecvMsgSize(maxRecvMsgBytes),
		grpc.UnaryInterceptor(p.guardInterceptor),
	)
	if p.config.Signals.Metrics {
		collectormetricspb.RegisterMetricsServiceServer(server, &metricsServiceServer{probe: p})
	}
	if p.config.Signals.Logs {
		collectorlogspb.RegisterLogsServiceServer(server, &logsServiceServer{probe: p})
	}
	if p.config.Signals.Traces {
		collectortracepb.RegisterTraceServiceServer(server, &tracesServiceServer{probe: p})
	}

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

	p.moduleLogger.Info().
		Str("address", p.config.Address).
		Strs("signals", p.config.Signals.names()).
		Msg("OTLP gRPC receiver started")
	return nil
}

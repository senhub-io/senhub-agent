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

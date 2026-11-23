package otlp

import (
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync"

	"google.golang.org/grpc"
	"google.golang.org/protobuf/proto"
)

// The SDK exporters keep their transport private: endpoint failover, TLS,
// headers, compression and the OTLP encoding are all behind them. The
// metrics and traces queues need the serialized request an export produced
// (to keep it) and a way to send it again unchanged (to replay it). Rather
// than rebuild the SDK's encoding or run a second transport next to it, a
// small hook sits at the bottom of the exporter's own transport (a gRPC
// client interceptor, an http.RoundTripper) and acts on a wireSlot found in
// the export's context:
//
//   - capture: remember the request about to be sent, so a failed export can
//     be written to disk byte for byte;
//   - dry: remember it and refuse to send it (errWireCaptured), which turns
//     the exporter into an encoder for a batch that must go to disk without
//     a network attempt;
//   - replay: swap the request the exporter built for the stored one, so the
//     send takes the exporter's own path to whichever endpoint is up.
//
// Without a slot in the context the hook is a pass-through.

// errWireCaptured is returned to the exporter in dry mode. It is not a
// failure of the backend, and no health state is derived from it.
var errWireCaptured = errors.New("request captured, not sent")

type wireSlotKey struct{}

// wireSlot carries one export's request between the decorator and the
// transport hook. It is per export call, so concurrent exports of one push
// each have their own.
type wireSlot struct {
	mu sync.Mutex
	// dry makes the hook capture without sending.
	dry bool
	// replay, when set, is the serialized request to send in place of the one
	// the exporter built.
	replay []byte

	msg      proto.Message
	body     func() (io.ReadCloser, error)
	bodyGzip bool
}

func withWireSlot(ctx context.Context, s *wireSlot) context.Context {
	return context.WithValue(ctx, wireSlotKey{}, s)
}

func wireSlotFrom(ctx context.Context) *wireSlot {
	s, _ := ctx.Value(wireSlotKey{}).(*wireSlot)
	return s
}

func (s *wireSlot) setMessage(m proto.Message) {
	s.mu.Lock()
	s.msg = m
	s.mu.Unlock()
}

func (s *wireSlot) setBody(body func() (io.ReadCloser, error), gz bool) {
	s.mu.Lock()
	s.body, s.bodyGzip = body, gz
	s.mu.Unlock()
}

// captured reports whether the hook saw a request.
func (s *wireSlot) captured() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.msg != nil || s.body != nil
}

// request returns the serialized, uncompressed OTLP request the exporter
// built for this export.
func (s *wireSlot) request() ([]byte, error) {
	s.mu.Lock()
	msg, body, gz := s.msg, s.body, s.bodyGzip
	s.mu.Unlock()
	switch {
	case msg != nil:
		return proto.Marshal(msg)
	case body != nil:
		rc, err := body()
		if err != nil {
			return nil, fmt.Errorf("re-read request body: %w", err)
		}
		defer rc.Close()
		var r io.Reader = rc
		if gz {
			zr, err := gzip.NewReader(rc)
			if err != nil {
				return nil, fmt.Errorf("request body: %w", err)
			}
			defer zr.Close()
			r = zr
		}
		return io.ReadAll(r)
	}
	return nil, errors.New("no request captured")
}

// wireUnaryInterceptor is the gRPC half of the hook.
func wireUnaryInterceptor() grpc.UnaryClientInterceptor {
	return func(ctx context.Context, method string, req, reply any, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
		s := wireSlotFrom(ctx)
		if s == nil {
			return invoker(ctx, method, req, reply, cc, opts...)
		}
		m, ok := req.(proto.Message)
		if !ok {
			return invoker(ctx, method, req, reply, cc, opts...)
		}
		if s.replay != nil {
			proto.Reset(m)
			if err := proto.Unmarshal(s.replay, m); err != nil {
				return fmt.Errorf("stored request: %w", err)
			}
			return invoker(ctx, method, req, reply, cc, opts...)
		}
		s.setMessage(m)
		if s.dry {
			return errWireCaptured
		}
		return invoker(ctx, method, req, reply, cc, opts...)
	}
}

// wireRoundTripper is the HTTP half of the hook.
type wireRoundTripper struct {
	next http.RoundTripper
}

func (t *wireRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	s := wireSlotFrom(req.Context())
	if s == nil {
		return t.next.RoundTrip(req)
	}
	if s.replay != nil {
		var buf bytes.Buffer
		zw := gzip.NewWriter(&buf)
		if _, err := zw.Write(s.replay); err != nil {
			closeBody(req)
			return nil, err
		}
		if err := zw.Close(); err != nil {
			closeBody(req)
			return nil, err
		}
		closeBody(req)
		raw := buf.Bytes()
		out := req.Clone(req.Context())
		out.Body = io.NopCloser(bytes.NewReader(raw))
		out.GetBody = func() (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(raw)), nil }
		out.ContentLength = int64(len(raw))
		out.Header.Set("Content-Encoding", "gzip")
		return t.next.RoundTrip(out)
	}
	s.setBody(req.GetBody, req.Header.Get("Content-Encoding") == "gzip")
	if s.dry {
		closeBody(req)
		return nil, errWireCaptured
	}
	return t.next.RoundTrip(req)
}

func closeBody(req *http.Request) {
	if req.Body != nil {
		_ = req.Body.Close()
	}
}

package otlp

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc"
	"google.golang.org/grpc/status"
)

// A backend that is up but overloaded says so: HTTP 429 or 503 with a
// Retry-After header, gRPC RESOURCE_EXHAUSTED or UNAVAILABLE with a
// google.rpc.RetryInfo detail. A signal with a disk queue runs with the SDK
// retry off, so the queue's own clock decides when to try again; this file
// is how the server's hint reaches that clock.

// maxThrottleHint caps a server's hint. A hostile or buggy server answering
// "retry in a week" must not stall an export path for good; past the cap the
// agent tries again anyway.
const maxThrottleHint = 10 * time.Minute

// maxHintSeconds bounds the parsed value so the conversion to a duration
// cannot overflow; the cap is applied later.
const maxHintSeconds = 366 * 24 * 3600

// parseRetryAfter reads a Retry-After header: delta-seconds or an HTTP-date.
// It reports false when the value is absent, unreadable, zero or already in
// the past, none of which asks for any wait.
func parseRetryAfter(v string, now time.Time) (time.Duration, bool) {
	v = strings.TrimSpace(v)
	if v == "" {
		return 0, false
	}
	if secs, err := strconv.ParseInt(v, 10, 64); err == nil {
		if secs <= 0 {
			return 0, false
		}
		if secs > maxHintSeconds {
			secs = maxHintSeconds
		}
		return time.Duration(secs) * time.Second, true
	}
	t, err := http.ParseTime(v)
	if err != nil {
		return 0, false
	}
	d := t.Sub(now)
	if d <= 0 {
		return 0, false
	}
	return d, true
}

// retryInfoDelay reads the retry_delay of a google.rpc.RetryInfo detail.
func retryInfoDelay(err error) (time.Duration, bool) {
	st, ok := status.FromError(err)
	if !ok {
		return 0, false
	}
	for _, d := range st.Details() {
		if ri, ok := d.(*errdetails.RetryInfo); ok {
			if delay := ri.GetRetryDelay().AsDuration(); delay > 0 {
				return delay, true
			}
		}
	}
	return 0, false
}

// throttledError is an export failure that carried a throttle hint. The
// relays' own HTTP forwarders build it from the response header; the queue
// channels wrap the error they hand back with it, so callers can tell a
// throttle from an outage.
type throttledError struct {
	err  error
	wait time.Duration
}

func (e *throttledError) Error() string { return e.err.Error() }
func (e *throttledError) Unwrap() error { return e.err }

func asThrottled(err error) (*throttledError, bool) {
	var te *throttledError
	if errors.As(err, &te) {
		return te, true
	}
	return nil, false
}

// hintSink collects the throttle hint seen by a transport. The SDK HTTP
// exporters turn a 429 into an error that no longer carries the header, so
// the transport records it as the response goes by and the exporter's health
// reads it once the failure surfaces. Absolute time, so a hint nobody
// collected expires on its own instead of lingering into a later failure.
type hintSink struct {
	untilNs atomic.Int64
}

func (s *hintSink) observe(d time.Duration) {
	if s == nil || d <= 0 {
		return
	}
	until := time.Now().Add(d).UnixNano()
	for {
		cur := s.untilNs.Load()
		if until <= cur || s.untilNs.CompareAndSwap(cur, until) {
			return
		}
	}
}

// remaining returns what is left of the observed hint at now.
func (s *hintSink) remaining(now time.Time) time.Duration {
	if s == nil {
		return 0
	}
	if d := time.Duration(s.untilNs.Load() - now.UnixNano()); d > 0 {
		return d
	}
	return 0
}

// observeResponse records the hint of a throttling HTTP response.
func (s *hintSink) observeResponse(resp *http.Response) {
	if s == nil || resp == nil {
		return
	}
	if resp.StatusCode != http.StatusTooManyRequests && resp.StatusCode != http.StatusServiceUnavailable {
		return
	}
	if d, ok := parseRetryAfter(resp.Header.Get("Retry-After"), time.Now()); ok {
		s.observe(d)
	}
}

// observeError records the RetryInfo of a gRPC status error.
func (s *hintSink) observeError(err error) {
	if err == nil || s == nil {
		return
	}
	if d, ok := retryInfoDelay(err); ok {
		s.observe(d)
	}
}

// hintSinks holds one sink per signal.
type hintSinks struct {
	metrics, logs, traces *hintSink
}

func newHintSinks() *hintSinks {
	return &hintSinks{metrics: &hintSink{}, logs: &hintSink{}, traces: &hintSink{}}
}

type hintSinksKey struct{}

// withHintSinks hands the sinks to the exporter builders, which attach the
// matching one to each signal's transport.
func withHintSinks(ctx context.Context, h *hintSinks) context.Context {
	return context.WithValue(ctx, hintSinksKey{}, h)
}

func hintSinkFor(ctx context.Context, signal string) *hintSink {
	h, _ := ctx.Value(hintSinksKey{}).(*hintSinks)
	if h == nil {
		return nil
	}
	switch signal {
	case "metrics":
		return h.metrics
	case "logs":
		return h.logs
	case "traces":
		return h.traces
	}
	return nil
}

// hintInterceptor is the gRPC side for a signal that has no wire hook.
func hintInterceptor(s *hintSink) grpc.UnaryClientInterceptor {
	return func(ctx context.Context, method string, req, reply any, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
		err := invoker(ctx, method, req, reply, cc, opts...)
		s.observeError(err)
		return err
	}
}

// hintRoundTripper is the HTTP side for a signal that has no wire hook.
type hintRoundTripper struct {
	next  http.RoundTripper
	hints *hintSink
}

func (t *hintRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	resp, err := t.next.RoundTrip(req)
	t.hints.observeResponse(resp)
	return resp, err
}

// relayStatusError is the error of a relay forwarder for a non-2xx answer.
// A throttling answer with a usable Retry-After carries it, so the queue
// clock can honour it.
func relayStatusError(url string, resp *http.Response) error {
	err := fmt.Errorf("POST %s: unexpected status %d", url, resp.StatusCode)
	if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode == http.StatusServiceUnavailable {
		if d, ok := parseRetryAfter(resp.Header.Get("Retry-After"), time.Now()); ok {
			return &throttledError{err: err, wait: d}
		}
	}
	return err
}

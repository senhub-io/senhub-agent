package otlpreceiver

import (
	"crypto/subtle"
	"errors"
	"math"
	"net"
	"strings"
	"sync"
	"time"
)

// Sentinel rejection reasons, mapped to transport-specific responses
// by the gRPC interceptor (Unauthenticated / PermissionDenied /
// ResourceExhausted) and the HTTP handler (401 / 403 / 429).
var (
	errUnauthorized = errors.New("invalid or missing bearer token")
	errForbidden    = errors.New("source address not in allowed_cidrs")
	errRateLimited  = errors.New("ingress rate limit exceeded")
)

// ingressGuard applies the optional protections of the OTLP ingest
// path (#278 lot 2): bearer-token auth, source-CIDR allow-list and a
// token-bucket rate limit. A nil guard (no protection configured)
// allows everything; each check is independently optional.
//
// The source address check uses the transport-level peer address only
// — proxy headers (X-Forwarded-For) are deliberately not trusted: an
// agent-side allow-list is a last line of defense, not a substitute
// for an authenticating reverse proxy.
type ingressGuard struct {
	token  string
	cidrs  []*net.IPNet
	bucket *tokenBucket
}

func newIngressGuard(cfg receiverConfig) *ingressGuard {
	if cfg.BearerToken == "" && len(cfg.AllowedCIDRs) == 0 && cfg.RateLimitRPS <= 0 {
		return nil
	}
	g := &ingressGuard{
		token: cfg.BearerToken,
		cidrs: cfg.AllowedCIDRs,
	}
	if cfg.RateLimitRPS > 0 {
		g.bucket = newTokenBucket(cfg.RateLimitRPS, float64(cfg.RateLimitBurst))
	}
	return g
}

// allow validates one request. remoteAddr is the transport peer
// (host:port); authHeader is the raw Authorization header value (or
// gRPC `authorization` metadata). Check order — identity first, rate
// last — so an unauthenticated flood cannot consume the budget of
// legitimate senders.
func (g *ingressGuard) allow(remoteAddr, authHeader string) error {
	if g == nil {
		return nil
	}

	if g.token != "" {
		presented, ok := strings.CutPrefix(authHeader, "Bearer ")
		if !ok || subtle.ConstantTimeCompare([]byte(presented), []byte(g.token)) != 1 {
			return errUnauthorized
		}
	}

	if len(g.cidrs) > 0 {
		host, _, err := net.SplitHostPort(remoteAddr)
		if err != nil {
			host = remoteAddr
		}
		ip := net.ParseIP(host)
		if ip == nil {
			return errForbidden
		}
		allowed := false
		for _, cidr := range g.cidrs {
			if cidr.Contains(ip) {
				allowed = true
				break
			}
		}
		if !allowed {
			return errForbidden
		}
	}

	if g.bucket != nil && !g.bucket.take() {
		return errRateLimited
	}

	return nil
}

// maxRetryAfterSeconds caps the Retry-After advice: a sender told to
// wait a quarter of an hour by a very low rate limit would hold its
// batches that long, and one that retries sooner only gets another 429.
const maxRetryAfterSeconds = 60

// retryAfterSeconds is how long a throttled sender should wait before
// trying again: the time the bucket needs to earn one token, rounded up
// to whole seconds as the Retry-After header requires.
func (g *ingressGuard) retryAfterSeconds() int {
	if g == nil || g.bucket == nil || g.bucket.rate <= 0 {
		return 1
	}
	secs := int(math.Ceil(1 / g.bucket.rate))
	if secs < 1 {
		return 1
	}
	if secs > maxRetryAfterSeconds {
		return maxRetryAfterSeconds
	}
	return secs
}

// tokenBucket is a minimal thread-safe token bucket. Hand-rolled
// rather than pulling golang.org/x/time: the dependency would need
// mirroring in the enterprise module for ~30 lines of code.
type tokenBucket struct {
	mu     sync.Mutex
	tokens float64
	max    float64
	rate   float64 // tokens per second
	last   time.Time
	now    func() time.Time // injectable for tests
}

func newTokenBucket(rate, burst float64) *tokenBucket {
	if burst < 1 {
		burst = 1
	}
	return &tokenBucket{
		tokens: burst,
		max:    burst,
		rate:   rate,
		now:    time.Now,
	}
}

func (b *tokenBucket) take() bool {
	b.mu.Lock()
	defer b.mu.Unlock()

	now := b.now()
	if !b.last.IsZero() {
		b.tokens += now.Sub(b.last).Seconds() * b.rate
		if b.tokens > b.max {
			b.tokens = b.max
		}
	}
	b.last = now

	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

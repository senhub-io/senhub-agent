package otlpreceiver

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	collectortracepb "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	resourcepb "go.opentelemetry.io/proto/otlp/resource/v1"

	"senhub-agent.go/internal/agent/services/agentstate"
)

var testHost = hostStamp{id: "6a6d1121-4a85-4e64-a222-746f7bc9c04c", name: "sha901"}

func resourceWith(kv ...string) *resourcepb.Resource {
	r := &resourcepb.Resource{}
	for i := 0; i+1 < len(kv); i += 2 {
		r.Attributes = append(r.Attributes, stringAttr(kv[i], kv[i+1]))
	}
	return r
}

func hostAttrs(r *resourcepb.Resource) (id, name string) {
	return resourceString(r, attrHostID), resourceString(r, attrHostName)
}

// The contract agreed with the topology consumer: a local sender gets
// this host's identity, a remote one never, and host.* is one set.
func TestTheSenderHostIsStatedOnlyWhenItIsKnown(t *testing.T) {
	cases := []struct {
		name     string
		origin   string
		res      *resourcepb.Resource
		wantID   string
		wantName string
	}{
		{"unix socket, nothing stated", originUDS, resourceWith("service.name", "intake"), testHost.id, testHost.name},
		{"loopback, nothing stated", originTCPLoopback, resourceWith("service.name", "intake"), testHost.id, testHost.name},
		{"remote sender is never stamped", originRemote, resourceWith("service.name", "intake"), "", ""},
		// A pod name beside a machine id would give one record two
		// contradicting identities: the set is all or nothing.
		{"a partial host set is left alone", originUDS, resourceWith("host.name", "squash-ima-lmlqv"), "", "squash-ima-lmlqv"},
		{"the sender's own id wins", originUDS, resourceWith("host.id", "abc"), "abc", ""},
		// A loopback peer that is itself a relay is not the origin.
		{"a relaying proxy on loopback", originTCPLoopback, resourceWith("telemetry.relay.host.id", "x"), "", ""},
		// On the socket the sender is local by construction, relayed or not.
		{"a relay on the unix socket", originUDS, resourceWith("telemetry.relay.host.id", "x"), testHost.id, testHost.name},
	}
	for _, c := range cases {
		stampResource(c.res, c.origin, testHost)
		if id, name := hostAttrs(c.res); id != c.wantID || name != c.wantName {
			t.Errorf("%s: host.id=%q host.name=%q, want %q %q", c.name, id, name, c.wantID, c.wantName)
		}
	}
}

func TestOriginIsTakenFromTheTransport(t *testing.T) {
	tcp := &OTLPReceiverProbe{}
	for addr, want := range map[string]string{
		"127.0.0.1:51234": originTCPLoopback,
		"[::1]:51234":     originTCPLoopback,
		"172.17.0.2:5123": originRemote,
		"":                originRemote,
	} {
		if got := tcp.origin(addr); got != want {
			t.Errorf("origin(%q) = %s, want %s", addr, got, want)
		}
	}
	uds := &OTLPReceiverProbe{config: receiverConfig{UnixPath: "/run/x.sock"}}
	if got := uds.origin("@"); got != originUDS {
		t.Errorf("a unix socket peer reads as %s", got)
	}
}

// End to end over a real Unix socket: a span sent with no host identity
// leaves the receiver carrying this host's, and the coverage counters
// record it on the uds path.
func TestASpanSentOverTheUnixSocketCarriesThisHost(t *testing.T) {
	sub := agentstate.SubscribeSpans(16)
	defer agentstate.UnsubscribeSpans(sub)

	dir, err := os.MkdirTemp("", "otlp")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	sock := filepath.Join(dir, "otlp.sock")
	newTestProbe(t, map[string]interface{}{
		"protocol": "grpc", "address": "unix:" + sock,
		"signals": []interface{}{"traces"},
	}, &captureCallback{})

	// "unix:" + path, not "unix://": a Windows path starts with a drive
	// letter, which the authority form reads as host:port.
	conn, err := grpc.NewClient("unix:"+sock, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	req := sampleTraceRequest("over-uds")
	req.ResourceSpans[0].Resource = &resourcepb.Resource{Attributes: []*commonpb.KeyValue{stringAttr("service.name", "uds-test-service")}}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if _, err := collectortracepb.NewTraceServiceClient(conn).Export(ctx, req); err != nil {
		t.Fatalf("Export over the unix socket: %v", err)
	}

	select {
	case got := <-sub:
		if id, _ := hostAttrs(got[0].Resource); id == "" || id != ownHost().id {
			t.Errorf("relayed span host.id = %q, want this host's %q", id, ownHost().id)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for the relayed span")
	}
	c := agentstate.GetOTLPReceiverCoverage()[agentstate.OTLPReceiverCoverageKey{Signal: signalTraces, Origin: originUDS, Service: "uds-test-service"}]
	if c.Received != 1 || (ownHost().id != "" && c.WithoutHostID != 0) {
		t.Errorf("coverage = %+v, want 1 received and none without host.id", c)
	}
}

func TestAUnixSocketRefusesAnAddressFilter(t *testing.T) {
	if _, err := parseReceiverConfig(map[string]interface{}{
		"address": "unix:/run/x.sock", "allowed_cidrs": []interface{}{"10.0.0.0/8"},
	}); err == nil {
		t.Error("allowed_cidrs was accepted on a unix socket, where no source address exists")
	}
}

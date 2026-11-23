//go:build linux

// Command senhub-ebpf is the privileged helper of the SenHub agent. It loads
// two kernel programs that report TCP connections and retransmissions, keeps
// them in bounded tables and serves them to the unprivileged agent over a unix
// socket. It needs CAP_BPF and CAP_PERFMON and nothing else.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"runtime/debug"
	"strings"
	"syscall"
	"time"

	"github.com/cilium/ebpf"
	"github.com/cilium/ebpf/link"
	"github.com/cilium/ebpf/ringbuf"
	"github.com/cilium/ebpf/rlimit"
	"github.com/rs/zerolog"
	"golang.org/x/sys/unix"

	"senhub-agent.go/internal/ebpfhelper"
	"senhub-agent.go/internal/ebpfproto"
)

var version = "dev"

// memoryBudget keeps the Go heap well under the unit's MemoryMax.
const memoryBudget = 36 << 20

func main() {
	os.Exit(run())
}

func run() int {
	socket := flag.String("socket", ebpfproto.DefaultSocket, "unix socket the agent connects to")
	maxConns := flag.Int("max-conns", ebpfhelper.DefaultMaxConns, "hard cap on tracked connections")
	closedTTL := flag.Duration("closed-ttl", ebpfhelper.DefaultClosedTTL, "how long a closed connection stays available to the agent")
	showVersion := flag.Bool("version", false, "print the version and exit")
	flag.Parse()
	if *showVersion {
		fmt.Println(version)
		return 0
	}

	log := zerolog.New(os.Stderr).With().Timestamp().Str("module", "senhub-ebpf").Logger()

	kernel, err := ebpfhelper.Preflight(hostChecks())
	if err != nil {
		var ref *ebpfhelper.RefusalError
		if errors.As(err, &ref) {
			log.Error().Str("kernel", kernel).Int("exit_code", ref.Code).Msg("senhub-ebpf refuses to start: " + ref.Reason)
			return ref.Code
		}
		log.Error().Err(err).Msg("start-up check failed")
		return ebpfhelper.ExitFailed
	}

	debug.SetMemoryLimit(memoryBudget)
	if err := rlimit.RemoveMemlock(); err != nil {
		log.Warn().Err(err).Msg("could not lift the locked-memory limit; loading may fail on this kernel")
	}

	var objs connObjects
	if err := loadConnObjects(&objs, nil); err != nil {
		log.Error().Err(err).Msg("loading the kernel programs failed")
		return ebpfhelper.ExitFailed
	}
	defer objs.Close()

	var links []link.Link
	defer func() {
		for _, l := range links {
			_ = l.Close()
		}
	}()
	for name, prog := range map[string]*ebpf.Program{
		"inet_sock_set_state": objs.HandleSetState,
		"tcp_retransmit_skb":  objs.HandleRetransmit,
	} {
		l, err := link.AttachTracing(link.TracingOptions{Program: prog})
		if err != nil {
			log.Error().Err(err).Str("tracepoint", name).Msg("attaching the kernel program failed")
			return ebpfhelper.ExitFailed
		}
		links = append(links, l)
	}

	reader, err := ringbuf.NewReader(objs.Events)
	if err != nil {
		log.Error().Err(err).Msg("opening the event ring buffer failed")
		return ebpfhelper.ExitFailed
	}
	defer reader.Close()

	tracker := ebpfhelper.NewTracker(*maxConns, *closedTTL)
	server := ebpfhelper.NewServer(tracker, ebpfhelper.Info{
		HelperVersion: version,
		Kernel:        kernel,
		Programs:      []string{"inet_sock_set_state", "tcp_retransmit_skb"},
		Started:       time.Now(),
	}, func() uint64 {
		var n uint64
		if err := objs.Dropped.Lookup(uint32(0), &n); err != nil {
			return 0
		}
		return n
	}, log)
	if err := server.Listen(*socket); err != nil {
		log.Error().Err(err).Msg("opening the agent socket failed")
		return ebpfhelper.ExitFailed
	}
	defer os.Remove(*socket)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		_ = reader.Close()
	}()
	go func() {
		if err := server.Serve(ctx); err != nil {
			log.Error().Err(err).Msg("serving the agent failed")
			stop()
		}
	}()

	log.Info().Str("version", version).Str("kernel", kernel).Str("socket", *socket).
		Int("max_conns", *maxConns).Msg("senhub-ebpf running")

	for {
		rec, err := reader.Read()
		if err != nil {
			if errors.Is(err, ringbuf.ErrClosed) {
				log.Info().Msg("senhub-ebpf stopping")
				return 0
			}
			log.Warn().Err(err).Msg("reading the event ring buffer")
			continue
		}
		ev, err := ebpfhelper.DecodeEvent(rec.RawSample)
		if err != nil {
			log.Warn().Err(err).Msg("discarding a malformed event")
			continue
		}
		tracker.Apply(ev)
	}
}

func hostChecks() ebpfhelper.Host {
	return ebpfhelper.Host{
		KernelRelease: func() (string, error) {
			var u unix.Utsname
			if err := unix.Uname(&u); err != nil {
				return "", err
			}
			return strings.TrimRight(string(u.Release[:]), "\x00"), nil
		},
		HasBTF: func() bool {
			_, err := os.Stat("/sys/kernel/btf/vmlinux")
			return err == nil
		},
		EffectiveCaps: func() (uint64, error) {
			b, err := os.ReadFile("/proc/self/status")
			if err != nil {
				return 0, err
			}
			return ebpfhelper.ParseCapEff(string(b))
		},
	}
}

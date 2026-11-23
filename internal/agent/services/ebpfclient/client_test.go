package ebpfclient

import (
	"bufio"
	"errors"
	"net"
	"os"
	"path/filepath"
	"testing"

	"senhub-agent.go/internal/ebpfproto"
)

// fakeHelper answers like the helper does, from canned responses.
func fakeHelper(t *testing.T, answer func(ebpfproto.Request) ebpfproto.Response) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "ebc")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	path := filepath.Join(dir, "e.sock")
	ln, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer conn.Close()
				r := bufio.NewReader(conn)
				req, err := ebpfproto.ReadRequest(r)
				if err != nil {
					return
				}
				_ = ebpfproto.Write(conn, answer(req))
			}()
		}
	}()
	return path
}

func TestConnectionsReturnsSnapshot(t *testing.T) {
	path := fakeHelper(t, func(req ebpfproto.Request) ebpfproto.Response {
		if req.Op != ebpfproto.OpSnapshot {
			t.Errorf("op %q", req.Op)
		}
		return ebpfproto.Response{V: ebpfproto.Version, OK: true, Conns: []ebpfproto.Conn{
			{PID: 7, Comm: "psql", Family: 4, LocalAddr: "10.0.0.5", RemoteAddr: "10.0.0.9", LocalPort: 4000, RemotePort: 5432, Retransmits: 3},
		}}
	})
	conns, err := New(path).Connections()
	if err != nil || len(conns) != 1 || conns[0].PID != 7 || conns[0].Retransmits != 3 {
		t.Fatalf("%+v %v", conns, err)
	}
}

func TestAbsentSocketFailsFastAsAbsent(t *testing.T) {
	c := New(filepath.Join(t.TempDir(), "none.sock"))
	if c.Present() {
		t.Fatal("present")
	}
	_, err := c.Connections()
	if !IsAbsent(err) {
		t.Fatalf("want an absence, got %v", err)
	}
}

func TestStaleSocketFileIsAFailureNotAbsence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "stale.sock")
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := New(path).Connections()
	if err == nil || IsAbsent(err) {
		t.Fatalf("a dead socket file must be reported as a failure: %v", err)
	}
}

func TestRefusedAndForeignVersionAreErrors(t *testing.T) {
	path := fakeHelper(t, func(ebpfproto.Request) ebpfproto.Response {
		return ebpfproto.Response{V: ebpfproto.Version, Error: "boom"}
	})
	if _, err := New(path).Connections(); err == nil {
		t.Fatal("a refusal must be an error")
	}
	path = fakeHelper(t, func(ebpfproto.Request) ebpfproto.Response {
		return ebpfproto.Response{V: 9, OK: true}
	})
	if _, err := New(path).Connections(); !errors.Is(err, ebpfproto.ErrVersion) {
		t.Fatalf("%v", err)
	}
}

func facts(kernel string, installed bool, present bool, hello func() (helloAnswer, error)) hostFacts {
	return hostFacts{
		kernel: func() string { return kernel }, btf: func() bool { return true },
		installed: func() bool { return installed }, present: func() bool { return present }, hello: hello,
	}
}

func TestInspectStates(t *testing.T) {
	ok := func() (helloAnswer, error) { return helloAnswer{version: "0.7.2", dropped: 4, tracked: 9}, nil }
	bad := func() (helloAnswer, error) { return helloAnswer{}, errors.New("connection refused") }

	cases := []struct {
		name string
		h    hostFacts
		want State
	}{
		{"nothing installed", facts("6.8.0", false, false, nil), NotInstalled},
		{"installed, not started", facts("6.8.0", true, false, nil), NotRunning},
		{"running", facts("6.8.0", true, true, ok), Running},
		{"socket without helper", facts("6.8.0", true, true, bad), NotRunning},
		{"RHEL 8 kernel", facts("4.18.0-553.el8.x86_64", true, false, nil), Unsupported},
	}
	for _, tc := range cases {
		st := inspect(tc.h)
		if st.State != tc.want {
			t.Errorf("%s: %s (%s), want %s", tc.name, st.State, st.Detail, tc.want)
		}
	}
	st := inspect(facts("6.8.0", true, true, ok))
	if st.HelperVersion != "0.7.2" || st.Dropped != 4 || st.TrackedConns != 9 || st.Kernel != "6.8.0" || !st.BTF {
		t.Fatalf("%+v", st)
	}
}

func TestKernelTooOld(t *testing.T) {
	for rel, want := range map[string]bool{"4.18.0-1": true, "5.4.0": true, "5.7.9": true, "5.8.0": false, "6.8.0-142-generic": false, "weird": false} {
		if kernelTooOld(rel) != want {
			t.Errorf("%s", rel)
		}
	}
}

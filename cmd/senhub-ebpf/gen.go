//go:build linux

package main

// The programs are compiled with clang on a development machine and the
// objects are committed, so no compiler is needed to build or to run the helper.
// Regenerate with `go generate ./cmd/senhub-ebpf` where clang and libbpf-dev
// are installed.
//
//go:generate go run github.com/cilium/ebpf/cmd/bpf2go -tags linux -target bpfel -cc clang conn ./bpf/conn.c -- -I/usr/include/bpf -O2 -g -Wall -Werror -D__TARGET_ARCH_x86

//go:build linux && !arm64 && !riscv64 && !loong64

package process

// The record ends with a session id and a timestamp whose width follows
// the architecture: glibc keeps them at 32 bits wherever a 64-bit build
// had to stay compatible with its 32-bit predecessor
// (__WORDSIZE_TIME64_COMPAT32), which is the case here and on every
// 32-bit architecture.
const utmpRecordSize = 384

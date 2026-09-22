//go:build linux && (arm64 || riscv64 || loong64)

package process

// These architectures had no 32-bit predecessor to stay compatible
// with, so glibc gives the record a 64-bit session id and a 64-bit
// timestamp: sixteen bytes more than elsewhere. Measured on an Ubuntu
// 24.04 aarch64 machine, whose file is an exact multiple of 400 and not
// of 384.
const utmpRecordSize = 400

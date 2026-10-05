//go:build !linux

package process

// hostSecuritySignals has no source outside Linux: the account database
// and the file-handle table it reads are Linux files. Nothing is emitted.
func hostSecuritySignals() (signals []hostSignal, errs []error) {
	return nil, nil
}

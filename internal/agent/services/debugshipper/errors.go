package debugshipper

import "errors"

// Error definitions for DebugLogShipper
var (
	// ErrMissingEndpoint is returned when no endpoint URL is provided
	ErrMissingEndpoint = errors.New("missing endpoint URL")

	// ErrShipperClosed is returned when trying to write to a closed shipper
	ErrShipperClosed = errors.New("debug log shipper is closed")

	// ErrRemoteEndpointError is returned when the remote endpoint returns an error
	ErrRemoteEndpointError = errors.New("remote endpoint returned error")
)

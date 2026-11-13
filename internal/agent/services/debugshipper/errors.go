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

	// ErrInsecureEndpoint is returned when a non-loopback endpoint uses plain http
	ErrInsecureEndpoint = errors.New("plain http is only allowed for a loopback endpoint, use https")

	// ErrInvalidEndpoint is returned when the endpoint URL cannot be used
	ErrInvalidEndpoint = errors.New("invalid endpoint URL")
)

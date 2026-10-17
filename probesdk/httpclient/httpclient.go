// Package httpclient is the public mirror of the shared probe HTTP client
// builder (senhub-agent.go/internal/agent/probes/httpclient).
package httpclient

import (
	"net/http"

	ihttpclient "senhub-agent.go/internal/agent/probes/httpclient"
)

type Options = ihttpclient.Options

func NewTransport(o Options) (*http.Transport, error) { return ihttpclient.NewTransport(o) }

func NewClient(o Options) (*http.Client, error) { return ihttpclient.NewClient(o) }

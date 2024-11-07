package senhub_server

import (
	"bytes"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/url"

	"github.com/ybbus/httpretry"
)

type SenhubServer interface {
	Get(string) (*http.Response, error)
	Post(string, any) (*http.Response, error)
}

type senhubServer struct {
	// API key to authenticate with the server
	authenticationKey string
	// URL of the server to send data to
	url string
	// HTTP client to use for requests
	http *http.Client
}

func NewSenhubServer(authenticationKey string, url string) SenhubServer {
	http := httpretry.NewDefaultClient(
		// retry up to 3 times
		httpretry.WithMaxRetryCount(3),
	)

	return &senhubServer{
		authenticationKey: authenticationKey,
		url:               url,
		http:              http,
	}
}

func (s senhubServer) NewRequest(method string, url string, body io.Reader) (*http.Request, error) {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-AGENT-KEY", s.authenticationKey)

	return req, nil
}

func (s senhubServer) Get(urlPath string) (*http.Response, error) {
	fullUrl, err := url.JoinPath(
		s.url,
		urlPath,
	)
	if err != nil {
		return nil, err
	}
	req, err := s.NewRequest("GET", fullUrl, nil)
	if err != nil {
		return nil, err
	}

	return s.http.Do(req)
}

func (s senhubServer) Post(urlPath string, data any) (*http.Response, error) {
	fullUrl, err := url.JoinPath(
		s.url,
		urlPath,
	)
	if err != nil {
		return nil, err
	}
	requestBody, err := json.Marshal(data)
	if err != nil {
		log.Printf("error encoding data. Err: %v", err)
		return nil, err
	}

	req, err := s.NewRequest("POST", fullUrl, bytes.NewReader(requestBody))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	return s.http.Do(req)
}

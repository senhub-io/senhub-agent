// Inspired from https://gist.github.com/var23rav/13dc201f77565454da7acb53aa6721ad
package testUtils

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"

	"github.com/rs/zerolog"
)

var logger = zerolog.New(os.Stderr)

type MockServerRequest struct {
	BodyStr  []byte
	BodyJson map[string]interface{}
	Req      *http.Request
}

type MockServer struct {
	Server *httptest.Server // Test server instance
	URL    string           // URL of the test server

	// mu guards last. The handler runs on the server's goroutine and the
	// assertions run on the test's; publishing through an exported field
	// meant every assertion read what another goroutine wrote, with
	// nothing ordering the two. It is test-only code, but a data race in
	// the harness makes -race findings in the code under test
	// untrustworthy, which is the whole point of running it (#297).
	mu   sync.Mutex
	last MockServerRequest
}

// record publishes the request the handler just read.
func (m *MockServer) record(req *http.Request, bodyStr []byte, bodyJSON map[string]interface{}) {
	m.mu.Lock()
	m.last = MockServerRequest{Req: req, BodyStr: bodyStr, BodyJson: bodyJSON}
	m.mu.Unlock()
}

// LastRequest returns a snapshot of the most recent request the server
// handled. Safe to call while the server is still running: the returned
// value is a copy, so a request arriving mid-assertion cannot change it
// under the caller.
//
// Before this was a method it was a field, and reading it raced the
// handler goroutine.
func (m *MockServer) LastRequest() MockServerRequest {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.last
}

func GetTestHTTPServer(expectedResponse string, resCode int) *MockServer {
	return getHTTPServer(expectedResponse, resCode, false)
}

func GetTestHTTPSServer(expectedResponse string, resCode int) *MockServer {
	return getHTTPServer(expectedResponse, resCode, true)
}

// getHTTPServer create a test server for mocking response for any REST operation
func getHTTPServer(expectedResponse string, resCode int, enableHtts bool) *MockServer {
	mock := &MockServer{}

	handlerFunc := http.HandlerFunc(func(res http.ResponseWriter, req *http.Request) {
		bodyStr, err := io.ReadAll(req.Body)
		if err != nil {
			logger.Error().
				Err(err).
				Msg("TestHTTPServer was unable to get request body.")
		}

		var bodyJSON map[string]interface{}
		if err := json.Unmarshal(bodyStr, &bodyJSON); err != nil {
			logger.Error().
				Err(err).
				Msg("TestHTTPServer was unable to parse request body as JSON.")
		}
		mock.record(req, bodyStr, bodyJSON)

		res.WriteHeader(resCode)
		_, err = res.Write([]byte(expectedResponse))
		if err != nil {
			logger.Error().
				Err(err).
				Any("Response", expectedResponse).
				Msg("TestHTTPServer failed to write response.")
		}
	})

	var testServer *httptest.Server
	if enableHtts {
		testServer = httptest.NewTLSServer(handlerFunc)
	} else {
		testServer = httptest.NewServer(handlerFunc)
	}

	mock.Server = testServer
	mock.URL = testServer.URL
	return mock
}

type TestHTTPServerURLConf struct {
	URLPath    string
	Method     string
	Body       string
	StatusCode int
}

func GetTestHTTPServerWithURLPath(urlPathConfList []TestHTTPServerURLConf) *MockServer {
	return getHTTPServerWithURLPath(urlPathConfList, false)
}

func GetTestHTTPSServerWithURLPath(urlPathConfList []TestHTTPServerURLConf) *MockServer {
	return getHTTPServerWithURLPath(urlPathConfList, true)
}

// getHTTPServerWithURLPath create a test server for mocking response by URL Path config
func getHTTPServerWithURLPath(urlPathConfList []TestHTTPServerURLConf, enableHtts bool) *MockServer {
	mock := &MockServer{}

	handlerFunc := http.HandlerFunc(func(res http.ResponseWriter, req *http.Request) {
		bodyStr, err := io.ReadAll(req.Body)
		if err != nil {
			logger.Error().
				Err(err).
				Msg("TestHTTPServer was unable to get request body.")
		}

		var bodyJSON map[string]interface{}
		if err := json.Unmarshal(bodyStr, &bodyJSON); err != nil {
			logger.Error().
				Err(err).
				Msg("TestHTTPServer was unable to parse request body as JSON.")
		}
		mock.record(req, bodyStr, bodyJSON)

		var matchedURLPathConf TestHTTPServerURLConf
		var doesReqURLMatched, doesReqMethodMatched bool
		for _, urlPathConf := range urlPathConfList {
			if urlPathConf.URLPath == req.URL.Path {
				doesReqURLMatched = true
				if urlPathConf.Method == "" || urlPathConf.Method == req.Method {
					doesReqMethodMatched = true
					matchedURLPathConf = urlPathConf
					break
				}
			}
		}

		if !doesReqURLMatched {
			matchedURLPathConf.Body = fmt.Sprintf("Path Not Found for requested URL '%s' !", req.URL.Path)
			matchedURLPathConf.StatusCode = 404
		} else if !doesReqMethodMatched {
			matchedURLPathConf.Body = fmt.Sprintf("Method '%s' Not Allowed for requested URL Path '%s'!", req.Method, req.URL.Path)
			matchedURLPathConf.StatusCode = 405
		}
		if matchedURLPathConf.StatusCode == 0 {
			matchedURLPathConf.StatusCode = 200
		}

		res.WriteHeader(matchedURLPathConf.StatusCode)
		_, err = res.Write([]byte(matchedURLPathConf.Body))
		if err != nil {
			logger.Error().
				Err(err).
				Any("Response", matchedURLPathConf.Body).
				Msg("TestHTTPServer failed to write response.")
		}
	})

	var testServer *httptest.Server
	if enableHtts {
		testServer = httptest.NewTLSServer(handlerFunc)
	} else {
		testServer = httptest.NewServer(handlerFunc)
	}
	mock.Server = testServer
	mock.URL = testServer.URL
	return mock
}

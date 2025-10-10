// Inspired from https://gist.github.com/var23rav/13dc201f77565454da7acb53aa6721ad
package testUtils

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"

	"github.com/rs/zerolog"
)

var logger = zerolog.New(os.Stderr)

type MockServerRequest struct {
	BodyStr  []byte
	BodyJson map[string]interface{}
	Req      *http.Request
}

type MockServer struct {
	Server      *httptest.Server   // Test server instance
	URL         string             // URL of the test server
	LastRequest *MockServerRequest // Last request received by the server
}

func GetTestHTTPServer(expectedResponse string, resCode int) *MockServer {
	return getHTTPServer(expectedResponse, resCode, false)
}

func GetTestHTTPSServer(expectedResponse string, resCode int) *MockServer {
	return getHTTPServer(expectedResponse, resCode, true)
}

// getHTTPServer create a test server for mocking response for any REST operation
func getHTTPServer(expectedResponse string, resCode int, enableHtts bool) *MockServer {
	lastRequest := &MockServerRequest{}

	handlerFunc := http.HandlerFunc(func(res http.ResponseWriter, req *http.Request) {
		bodyStr, err := io.ReadAll(req.Body)
		if err != nil {
			logger.Error().
				Err(err).
				Msg("TestHTTPServer was unable to get request body.")
		}

		lastRequest.Req = req
		lastRequest.BodyStr = bodyStr
		err = json.Unmarshal(bodyStr, &lastRequest.BodyJson)
		if err != nil {
			logger.Error().
				Err(err).
				Msg("TestHTTPServer was unable to parse request body as JSON.")
		}

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

	return &MockServer{
		Server:      testServer,
		URL:         testServer.URL,
		LastRequest: lastRequest,
	}
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
	lastRequest := &MockServerRequest{}

	handlerFunc := http.HandlerFunc(func(res http.ResponseWriter, req *http.Request) {
		bodyStr, err := io.ReadAll(req.Body)
		if err != nil {
			logger.Error().
				Err(err).
				Msg("TestHTTPServer was unable to get request body.")
		}

		lastRequest.Req = req
		lastRequest.BodyStr = bodyStr
		err = json.Unmarshal(bodyStr, &lastRequest.BodyJson)
		if err != nil {
			logger.Error().
				Err(err).
				Msg("TestHTTPServer was unable to parse request body as JSON.")
		}

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
	return &MockServer{
		Server:      testServer,
		URL:         testServer.URL,
		LastRequest: lastRequest,
	}
}

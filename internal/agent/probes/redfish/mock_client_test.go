// Package redfish provides monitoring capabilities for hardware systems via Redfish API
package redfish

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/stretchr/testify/mock"
)

// MockRedfishClient is a mock implementation of the RedfishClientInterface for testing
type MockRedfishClient struct {
	mock.Mock
	responseMocks map[string]string
}

// Ensure MockRedfishClient implements RedfishClientInterface
var _ RedfishClientInterface = &MockRedfishClient{}

func NewMockRedfishClient() *MockRedfishClient {
	return &MockRedfishClient{
		responseMocks: make(map[string]string),
	}
}

// AddMockResponse adds a mock response for a specific path
func (m *MockRedfishClient) AddMockResponse(path, jsonResponse string) {
	// Normalize path by removing any starting slashes
	path = strings.TrimPrefix(path, "/")
	m.responseMocks[path] = jsonResponse
}

// Connect mocks the Connect method
func (m *MockRedfishClient) Connect(ctx context.Context) error {
	args := m.Called(ctx)
	return args.Error(0)
}

// Disconnect mocks the Disconnect method
func (m *MockRedfishClient) Disconnect(ctx context.Context) error {
	args := m.Called(ctx)
	return args.Error(0)
}

// Get mocks the Get method
func (m *MockRedfishClient) Get(ctx context.Context, path string) (*RedfishResponse, error) {
	// Remove any leading slashes or "redfish/v1/" prefix to normalize the path
	path = strings.TrimPrefix(path, "/")
	path = strings.TrimPrefix(path, "redfish/v1/")

	// Check if we have a mock response for this path
	if jsonResponse, ok := m.responseMocks[path]; ok {
		resp, err := createMockRedfishResponse(jsonResponse)
		if err != nil {
			return nil, err
		}
		return resp, nil
	}

	// If we don't have a mock response, return the result from the mock expectations
	args := m.Called(ctx, path)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*RedfishResponse), args.Error(1)
}

// GetRaw mocks the GetRaw method
func (m *MockRedfishClient) GetRaw(ctx context.Context, path string) ([]byte, error) {
	args := m.Called(ctx, path)
	return args.Get(0).([]byte), args.Error(1)
}

// DetectRedfishVersions mocks the DetectRedfishVersions method
func (m *MockRedfishClient) DetectRedfishVersions(ctx context.Context) (*RedfishVersionInfo, error) {
	args := m.Called(ctx)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*RedfishVersionInfo), args.Error(1)
}

// createMockRedfishResponse creates a mock Redfish response for testing
func createMockRedfishResponse(jsonData string) (*RedfishResponse, error) {
	var resp RedfishResponse
	raw := []byte(jsonData)
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, err
	}
	// Store the raw data
	resp.Raw = raw
	return &resp, nil
}

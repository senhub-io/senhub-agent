package configuration

import (
	"encoding/json"
	"fmt"

	"senhub-agent.go/internal/testUtils"
)

func NewConfigurationMockServerRoute(statusCode int, body string, validateBody bool) testUtils.TestHTTPServerURLConf {
	// Validate that the configuration is in the expected format
	if validateBody {
		err := json.Unmarshal([]byte(body), &RemoteConfigurationData{})
		if err != nil {
			fmt.Printf("Invalid configuration mock: %v\n", err)
			panic(err)
		}
	}

	return testUtils.TestHTTPServerURLConf{
		URLPath:    "/configs",
		Method:     "GET",
		StatusCode: statusCode,
		Body:       body,
	}
}

var ConfigurationOk = NewConfigurationMockServerRoute(
	200,
	`{
		"storage": [
			{
				"name": "senhub",
				"params": {
					"interval": 10
				}
			}
		],
		"porbes": [
			{ "name": "memory", "params": {} }
		]
	}`,
	true,
)

var ConfigurationUnauthenticated = NewConfigurationMockServerRoute(401, `{}`, false)

var ConfigurationUnauthorized = NewConfigurationMockServerRoute(403, `{}`, false)

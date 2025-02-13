package auto_update

import (
	"encoding/json"
	"fmt"

	"senhub-agent.go/internal/testUtils"
)

func NewAutoUpdateVersionRoutes(
	versions [][2]string,
) []testUtils.TestHTTPServerURLConf {
	routes := make([]testUtils.TestHTTPServerURLConf, len(versions)+1)

	for i, version := range versions {
		alias := version[0]
		version := version[1]

		routes[i] = NewAutoUpdateVersionMetadataRoute(alias, version)
	}

	routes[len(versions)] = NewAutoUpdateVersionListRoute(versions)

	return routes
}

func NewAutoUpdateVersionMetadataRoute(
	alias string,
	args ...string,
) testUtils.TestHTTPServerURLConf {
	versionNumber := alias
	if len(args) > 0 && args[0] != "" {
		versionNumber = args[0]
	}

	body := `{"version": "` + versionNumber + `", "project_name": "senhub-agent"}`

	// Validate that the configuration is in the expected format
	err := json.Unmarshal([]byte(body), &VersionMetadata{})
	if err != nil {
		fmt.Printf("Invalid configuration mock: %v\n", err)
		panic(err)
	}

	return testUtils.TestHTTPServerURLConf{
		URLPath:    fmt.Sprintf(VERSION_METADATA_PATH, alias),
		Method:     "GET",
		StatusCode: 200,
		Body:       body,
	}
}

func NewAutoUpdateVersionListRoute(
	versions [][2]string,
) testUtils.TestHTTPServerURLConf {
	body := "["
	for i, version := range versions {
		alias := version[0]
		version := version[1]

		body += `{"version": "` + alias + `", "project_name": "` + version + `"}`
		if i < len(versions)-1 {
			body += ","
		}
	}
	body += "]"

	return testUtils.TestHTTPServerURLConf{
		URLPath:    VERSION_METADATA_LIST_PATH,
		Method:     "GET",
		StatusCode: 200,
		Body:       body,
	}
}

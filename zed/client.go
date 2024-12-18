package zed

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"

	"zedex/utils"
)

type Client struct {
	host             string
	maxSchemaVersion int
}

func NewZedClient(maxSchemaVersion int) Client {
	return Client{
		maxSchemaVersion: maxSchemaVersion,
		host:             utils.EnvWithFallback("ZED_HOST", "https://api.zed.dev"),
	}
}

// GetExtensionsIndex retrieves the list of available extensions from the Zed API.
//
// This function takes a maximum schema version as an argument and returns a list of
// extensions that are compatible with that version.
//
// Args:
//
//	maxSchemaVersion (int): The maximum schema version that the extensions should be compatible with.
//
// Returns:
//
//	Extensions: A list of extensions that match the provided schema version.
//	error: Any error that occurs during the retrieval process.
func (c *Client) GetExtensionsIndex() (Extensions, error) {
	u := fmt.Sprintf("%s/extensions?max_schema_version=%d", c.host, c.maxSchemaVersion)
	if _, err := url.Parse(u); err != nil {
		return Extensions{}, err
	}

	resp, err := http.Get(u)
	if err != nil {
		return Extensions{}, err
	}
	defer resp.Body.Close()

	var exResp struct {
		Data []Extension `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&exResp); err != nil {
		return Extensions{}, err
	}

	return exResp.Data, nil
}

// DownloadExtensionArchive downloads the bytes of a tarball that contains the extension.
//
// This function takes an extension and version constraints as arguments and returns
// the bytes of the tarball containing the extension. The version constraints are used
// to filter the extensions that match the provided schema and WASM API versions.
//
// Args:
//
//	extension (Extension): The extension to download.
//	minSchemaVersion (int): The minimum schema version that the extension should be compatible with.
//	minWasmAPIVersion (string): The minimum WASM API version that the extension should be compatible with.
//	maxWasmAPIVersion (string): The maximum WASM API version that the extension should be compatible with.
//
// Returns:
//
//	[]byte: The bytes of the tarball containing the extension.
//	error: Any error that occurs during the download process.
func (c *Client) DownloadExtensionArchive(extension Extension, minSchemaVersion int, minWasmAPIVersion string, maxWasmAPIVersion string) ([]byte, error) {
	u := fmt.Sprintf(
		"%s/extensions/%s/download?min_schema_version=%d&max_schema_version=%d&min_wasm_api_version=%s&max_wasm_api_version=%s",
		c.host,
		extension.ID,
		minSchemaVersion,
		c.maxSchemaVersion,
		minWasmAPIVersion,
		maxWasmAPIVersion,
	)
	if _, err := url.Parse(u); err != nil {
		return []byte{}, err
	}

	resp, err := http.Get(u)
	if err != nil {
		return []byte{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return []byte{}, fmt.Errorf("HTTP request failed with status code %d", resp.StatusCode)
	}

	return io.ReadAll(resp.Body)
}

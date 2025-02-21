package zed

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"runtime"

	"zedex/utils"
)

type Client struct {
	host               string
	maxSchemaVersion   int
	extensionsLocalDir string
}

func NewZedClient(maxSchemaVersion int) Client {
	return Client{
		maxSchemaVersion: maxSchemaVersion,
		host:             utils.EnvWithFallback("ZED_HOST", "https://api.zed.dev"),
	}
}

func (c *Client) WithExtensionsLocalDir(extensionsLocalDir string) *Client {
	c.extensionsLocalDir = extensionsLocalDir
	return c
}

func (c *Client) ensureExtensionsLocalDir() error {
	if c.extensionsLocalDir == "" {
		return nil
	}
	return os.MkdirAll(c.extensionsLocalDir, os.ModePerm)
}

// LoadExtensionIndex loads the extensions index from a local file.
//
// This function takes a file path as an argument, reads the file, and returns a list of extensions.
//
// Args:
//
//	indexFile (string): The path to the file containing the extensions index.
//
// Returns:
//
//	Extensions: A list of extensions read from the file.
//	error: Any error that occurs during the loading process.
func (c *Client) LoadExtensionIndex(indexFile string) (Extensions, error) {
	file, err := os.Open(indexFile)
	if err != nil {
		return Extensions{}, err
	}
	defer file.Close()

	var exResp wrappedExtensions
	if err := json.NewDecoder(file).Decode(&exResp); err != nil {
		return Extensions{}, err
	}

	return exResp.Data, nil
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

	var exResp wrappedExtensions
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

func (c *Client) DownloadExtensionArchiveDefault(extension Extension) ([]byte, error) {
	archiveBytes, err := c.DownloadExtensionArchive(extension, 0, "0.0.0", "100.0.0") // TODO: Fix version "hack"
	if err != nil {
		return []byte{}, err
	}

	if err := c.ensureExtensionsLocalDir(); err != nil {
		return []byte{}, err
	}

	return archiveBytes, nil
}

func (c *Client) LoadExtensionArchive(extension Extension) ([]byte, error) {
	if err := c.ensureExtensionsLocalDir(); err != nil {
		return []byte{}, err
	}

	filePath := fmt.Sprintf("%s/%s.tar.gz", c.extensionsLocalDir, extension.ID)
	file, err := os.Open(filePath)
	if err != nil {
		return []byte{}, err
	}
	defer file.Close()

	return io.ReadAll(file)
}

func (c *Client) GetLatestZedVersion() (Version, error) {
	os := runtime.GOOS
	arch := utils.IfElse(runtime.GOARCH == "amd64", "x86_64", runtime.GOARCH)
	host := "https://zed.dev" // For some reason, releases are on zed.dev/api, not api.zed.dev, so we cant use c.host.
	u := fmt.Sprintf("%s/api/releases/latest?asset=zed&os=%s&arch=%s", host, os, arch)
	resp, err := http.Get(u)
	if err != nil {
		return Version{}, err
	}
	var ver Version
	if err := json.NewDecoder(resp.Body).Decode(&ver); err != nil {
		return Version{}, err
	}

	return ver, nil
}

func (c *Client) LoadLatestZedVersionFromFile(versionFile string) (Version, error) {
	file, err := os.Open(versionFile)
	if err != nil {
		return Version{}, err
	}
	defer file.Close()

	var ver Version
	if err := json.NewDecoder(file).Decode(&ver); err != nil {
		return Version{}, err
	}

	return ver, nil
}

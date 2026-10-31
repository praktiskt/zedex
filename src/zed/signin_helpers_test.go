package zed

import (
	"crypto/sha256"
	"hash"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"
)

// newSHA256 returns the hash the access token is encrypted with.
func newSHA256() hash.Hash { return sha256.New() }

// getNoRedirect performs a request that does not follow the redirects.
func getNoRedirect(t *testing.T, server *httptest.Server, path string) *http.Response {
	t.Helper()

	client := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	response, err := client.Get(server.URL + path)
	require.NoError(t, err)
	t.Cleanup(func() { response.Body.Close() })
	return response
}

// getWithRedirects performs a request that follows the redirects, but stops
// at the local port Zed waits for the callback on.
func getWithRedirects(t *testing.T, server *httptest.Server, path string) *pageResult {
	t.Helper()

	client := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if strings.HasPrefix(req.URL.Host, "127.0.0.1:") {
				return http.ErrUseLastResponse
			}
			return nil
		},
	}
	response, err := client.Get(server.URL + path)
	require.NoError(t, err)
	defer response.Body.Close()

	body, err := io.ReadAll(response.Body)
	require.NoError(t, err)
	return &pageResult{status: response.StatusCode, body: string(body)}
}

// getWithAuth performs a request with an authorization header.
func getWithAuth(t *testing.T, server *httptest.Server, authorization string) *http.Response {
	t.Helper()

	request, err := http.NewRequest(http.MethodGet, server.URL+"/client/users/me", nil)
	require.NoError(t, err)
	request.Header.Set("Authorization", authorization)

	client := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	response, err := client.Do(request)
	require.NoError(t, err)

	t.Cleanup(func() { response.Body.Close() })
	return response
}

// websocketStatus opens a websocket connection with an authorization header
// and reports the status of the handshake.
func websocketStatus(t *testing.T, url string, authorization string) int {
	t.Helper()

	dialer := websocket.Dialer{}
	conn, response, err := dialer.Dial(url, http.Header(map[string][]string{
		"Authorization": {authorization},
	}))
	if err != nil && response == nil {
		return 0
	}
	if conn != nil {
		t.Cleanup(func() { conn.Close() })
	}
	if response == nil {
		return 0
	}
	defer response.Body.Close()
	return response.StatusCode
}

// readBody reads the body of a response.
func readBody(t *testing.T, response *http.Response) string {
	t.Helper()

	body, err := io.ReadAll(response.Body)
	require.NoError(t, err)
	return string(body)
}

package zed

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

var zed = NewZedClient(1)

func TestGetExtensionsList(t *testing.T) {
	extensions, err := zed.GetExtensionsIndex()
	assert.Nil(t, err)
	assert.Greater(t, extensions.Len(), 400)

	htmlExtension := extensions.GetByID("html")
	assert.NotNil(t, htmlExtension)
	assert.Equal(t, htmlExtension.ID, "html")
	assert.Equal(t, htmlExtension.Name, "HTML")
	assert.Greater(t, htmlExtension.DownloadCount, 1000000)
}

func TestDownloadExtensionArchive(t *testing.T) {
	extension := Extension{
		ID:             "html",
		Name:           "HTML",
		Version:        "0.1.4",
		Description:    "HTML support.",
		Authors:        []string{"Isaac Clayton <slightknack@gmail.com>"},
		Repository:     "https://github.com/zed-industries/zed",
		SchemaVersion:  1,
		WasmAPIVersion: "0.1.0",
		PublishedAt:    "2024-11-15T15:20:59Z",
		DownloadCount:  1009996,
	}

	b, err := zed.DownloadExtensionArchive(extension, 0, "0.0.1", extension.WasmAPIVersion)
	assert.Nil(t, err)
	assert.Greater(t, len(b), 0)
}

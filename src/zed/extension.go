package zed

import (
	"encoding/json"
	"slices"
	"sort"
)

type Extension struct {
	ID             string   `json:"id"`
	Name           string   `json:"name"`
	Version        string   `json:"version"`
	Description    string   `json:"description"`
	Authors        []string `json:"authors"`
	Repository     string   `json:"repository"`
	Provides       []string `json:"provides"`
	SchemaVersion  int      `json:"schema_version"`
	WasmAPIVersion string   `json:"wasm_api_version"`
	PublishedAt    string   `json:"published_at"`
	DownloadCount  int      `json:"download_count"`
}

func (e Extension) AsJsonStr() string {
	jsonStr, _ := json.Marshal(e)
	return string(jsonStr)
}

type Extensions []Extension

// wrappedExtension exists only to solve back and forth with the Zed API.
type wrappedExtensions struct {
	Data Extensions `json:"data"`
}

func (ex Extensions) AsWrapped() wrappedExtensions {
	return wrappedExtensions{Data: ex}
}

func (e Extensions) Filter(f func(Extension) bool) Extensions {
	var filtered Extensions
	for _, ext := range e {
		if f(ext) {
			filtered = append(filtered, ext)
		}
	}
	return filtered
}

func (e Extensions) FilterBySchemaVersion(version int) Extensions {
	return e.Filter(func(ext Extension) bool {
		return ext.SchemaVersion == version
	})
}

func (e Extensions) FilterByProvides(t string) Extensions {
	return e.Filter(func(ext Extension) bool {
		return slices.Contains(ext.Provides, t)
	})
}

func (e Extensions) SortByDownloadCount(ascending bool) {
	sort.Slice(e, func(i, j int) bool {
		if ascending {
			return e[i].DownloadCount < e[j].DownloadCount
		}
		return e[i].DownloadCount > e[j].DownloadCount
	})
}

func (e Extensions) Len() int           { return len(e) }
func (e Extensions) Swap(i, j int)      { e[i], e[j] = e[j], e[i] }
func (e Extensions) Less(i, j int) bool { return e[i].DownloadCount > e[j].DownloadCount }

func (e Extensions) GetByID(id string) *Extension {
	for _, ext := range e {
		if ext.ID == id {
			return &ext
		}
	}
	return nil
}

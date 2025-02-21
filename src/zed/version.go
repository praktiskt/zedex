package zed

type Version struct {
	Version string `json:"version"`
	URL     string `json:"url"`
}

func NewVersion(offline bool) Version {
	return Version{}
}

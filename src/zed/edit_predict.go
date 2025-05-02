package zed

import (
	"strings"

	"zedex/llm"
	"zedex/utils"

	"github.com/google/uuid"
	"github.com/sirupsen/logrus"
)

const (
	EDIT_REGION_START = "<|editable_region_start|>"
	EDIT_REGION_END   = "<|editable_region_end|>"
	START_OF_FILE     = "<|start_of_file|>"
)

type EditPredictClient struct {
	OpenAIHost llm.OpenAIHost
	cache      utils.ConcurrentMap[string, string]
}

type EditPredictRequest struct {
	Outline          string `json:"outline"`
	InputEvents      string `json:"input_events"`
	InputExcerpt     string `json:"input_excerpt"`
	SpeculatedOutput string `json:"speculated_output"`
}

type EditPredictResponse struct {
	RequestId     string `json:"request_id"`
	OutputExcerpt string `json:"output_excerpt"`
}

func NewEditPredictClient(openAIHost llm.OpenAIHost) EditPredictClient {
	return EditPredictClient{
		OpenAIHost: openAIHost,
		cache:      utils.NewConcurrentMap[string, string](),
	}
}

func (c *EditPredictClient) HandleRequest(req EditPredictRequest) (EditPredictResponse, error) {
	if c.cache.Exists(req.InputExcerpt) {
		logrus.Info("cache hit")
		epr := EditPredictResponse{
			RequestId:     uuid.New().String(),
			OutputExcerpt: c.cache.Get(req.InputExcerpt),
		}
		return epr, nil
	}

	txt := extractEditableRegion(req.InputExcerpt)
	resp, err := c.OpenAIHost.Chat(txt)
	if err != nil || resp == nil {
		return EditPredictResponse{}, err
	}

	predicted := extractEditableRegion(resp.GetLastResponse())
	response := replaceEditableRegion(req.InputExcerpt, predicted)

	epr := EditPredictResponse{
		RequestId:     uuid.New().String(),
		OutputExcerpt: response,
	}
	c.cache.Set(txt, epr.OutputExcerpt)

	return epr, nil
}

func extractEditableRegion(s string) string {
	startIndex := strings.Index(s, EDIT_REGION_START)
	endIndex := strings.Index(s, EDIT_REGION_END)
	if startIndex != -1 && endIndex != -1 {
		return s[startIndex : endIndex+len(EDIT_REGION_END)]
	}
	return ""
}

func replaceEditableRegion(original, replacement string) string {
	startIndex := strings.Index(original, EDIT_REGION_START)
	endIndex := strings.Index(original, EDIT_REGION_END)
	if startIndex != -1 && endIndex != -1 {
		return original[:startIndex] + replacement + original[endIndex+len(EDIT_REGION_END):]
	}
	return original
}

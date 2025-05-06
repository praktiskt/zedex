package zed

import (
	"regexp"
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
	OpenAIHost         llm.OpenAIHost
	cache              utils.ConcurrentMap[string, string]
	concurrentRequests chan struct{}
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
		OpenAIHost:         openAIHost,
		cache:              utils.NewConcurrentMap[string, string](),
		concurrentRequests: make(chan struct{}, 1),
	}
}

// WithConcurrentLLMRequests controls the number of concurrent in-flight requests made
// towards the backing LLM. Setting it to something low may potentially increase cache
// hit rate.
func (epc *EditPredictClient) WithConcurrentLLMRequests(num int) {
	if num < 1 {
		logrus.Fatal("must set concurrency capacity to at least 1")
	}
	epc.concurrentRequests = make(chan struct{}, num)
}

func (c *EditPredictClient) HandleRequest(req EditPredictRequest) (EditPredictResponse, error) {
	// TODO: This is a naive concurrency control. Zed fires 3x completion request, but
	// it seems like it would only need one. By controlling concurrency we can more
	// efficiently increase cache hit rate.
	c.concurrentRequests <- struct{}{}
	defer func() { <-c.concurrentRequests }()

	if hit := c.cache.Get(req.InputExcerpt); hit != "" {
		logrus.Debugf("cache hit")
		epr := EditPredictResponse{
			RequestId:     uuid.New().String(),
			OutputExcerpt: hit,
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
	response = removeReasoningBlock(response)
	response = removeCodeBlock(response)
	logrus.Debug(response)

	epr := EditPredictResponse{
		RequestId:     uuid.New().String(),
		OutputExcerpt: response,
	}
	c.cache.Set(req.InputExcerpt, epr.OutputExcerpt)

	return epr, nil
}

func extractEditableRegion(s string) string {
	startIndex := strings.Index(s, EDIT_REGION_START)
	endIndex := strings.Index(s, EDIT_REGION_END)
	if startIndex == -1 || endIndex == -1 {
		return ""
	}
	return s[startIndex : endIndex+len(EDIT_REGION_END)]
}

func replaceEditableRegion(original, replacement string) string {
	startIndex := strings.Index(original, EDIT_REGION_START)
	endIndex := strings.Index(original, EDIT_REGION_END)
	if startIndex == -1 || endIndex == -1 {
		return original
	}
	return original[:startIndex] + replacement + original[endIndex+len(EDIT_REGION_END):]
}

func removeReasoningBlock(s string) string {
	return regexp.MustCompile(`^<think>.*?</think>\s+?`).ReplaceAllString(s, "")
}

func removeCodeBlock(s string) string {
	firstLine := strings.SplitN(s, "\n", 2)
	if len(firstLine) == 2 && strings.HasPrefix(firstLine[0], "```") {
		s = firstLine[1]
	}
	if strings.HasSuffix(s, "```") {
		s = s[:len(s)-4]
	}
	if strings.HasPrefix(s, " ") {
		s = s[1:]
	}
	return s
}

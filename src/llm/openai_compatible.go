package llm

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	"zedex/utils"
)

type OpenAIResponse struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	Created int    `json:"created"`
	Model   string `json:"model"`
	Choices []struct {
		Text    string `json:"text"`
		Message struct {
			Content string `json:"content"`
			Role    string `json:"role"`
		} `json:"message"`
	} `json:"choices"`
}

func (o *OpenAIResponse) GetLastResponse() string {
	if len(o.Choices) == 0 {
		return ""
	}
	return o.Choices[len(o.Choices)-1].Message.Content
}

func GetOpenAICompatibleResponse(question string) (*OpenAIResponse, error) {
	host := utils.EnvWithFallback("OPENAI_COMPATIBLE_ENDPOINT", "https://api.groq.com/openai/v1/chat/completions")
	req, err := http.NewRequest("POST", host, nil)
	if err != nil {
		return nil, err
	}

	auth, ok := os.LookupEnv("OPENAI_COMPATIBLE_API_KEY")
	if !ok {
		return nil, fmt.Errorf("OPENAI_COMPATIBLE_API_KEY not set")
	}
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", auth))
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")

	postData := map[string]interface{}{
		"messages": []map[string]string{
			{
				"role":    "system",
				"content": utils.EnvWithFallback("OPENAI_COMPATIBLE_SYSTEM_PROMPT", "You are a helpful assistant"),
			},
			{
				"role":    "user",
				"content": question,
			},
		},
		"model":       utils.EnvWithFallback("OPENAI_COMPATIBLE_MODEL", "llama-3.3-70b-versatile"),
		"temperature": 0.1,
	}
	jsonPostData, err := json.Marshal(postData)
	if err != nil {
		return nil, err
	}
	req.Body = io.NopCloser(strings.NewReader(string(jsonPostData)))

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("server responded with %v: %v", resp.StatusCode, string(body))
	}

	var openAIResponse OpenAIResponse
	err = json.Unmarshal(body, &openAIResponse)
	if err != nil {
		return nil, err
	}

	return &openAIResponse, nil
}

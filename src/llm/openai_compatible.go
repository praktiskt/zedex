package llm

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
)

type OpenAIHost struct {
	Host         string
	EnvName      string
	SystemPrompt string
	Temperature  float64
	Model        string
}

func NewOpenAIHost(host, envName string) *OpenAIHost {
	return &OpenAIHost{
		Host:    host,
		EnvName: envName,
	}
}

func (c *OpenAIHost) WithSystemPrompt(prompt string) *OpenAIHost {
	c.SystemPrompt = prompt
	return c
}

func (c *OpenAIHost) WithTemperature(temperature float64) *OpenAIHost {
	c.Temperature = temperature
	return c
}

func (c *OpenAIHost) WithModel(modelName string) *OpenAIHost {
	c.Model = modelName
	return c
}

func (c OpenAIHost) GetKey() (string, error) {
	key, ok := os.LookupEnv(c.EnvName)
	if !ok {
		return "", fmt.Errorf("env %v is not set", c.EnvName)
	}
	if key == "" {
		return "", fmt.Errorf("env %v is empty", c.EnvName)
	}

	return key, nil
}

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

func (c *OpenAIHost) Chat(question string) (*OpenAIResponse, error) {
	if v, ok := os.LookupEnv("OPENAI_COMPATIBLE_DISABLE"); ok || v != "" {
		return nil, nil
	}

	req, err := http.NewRequest("POST", c.Host, nil)
	if err != nil {
		return nil, err
	}

	auth, err := c.GetKey()
	if err != nil {
		return nil, fmt.Errorf("OPENAI_COMPATIBLE_API_KEY not set")
	}
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", auth))
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")

	postData := map[string]interface{}{
		"messages": []map[string]string{
			{
				"role":    "system",
				"content": c.SystemPrompt,
			},
			{
				"role":    "user",
				"content": question,
			},
		},
		"model":       c.Model,
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

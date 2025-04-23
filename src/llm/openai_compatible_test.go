package llm

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestGroq(t *testing.T) {
	groq := NewOpenAIHost(
		"https://api.groq.com/openai/v1/chat/completions",
		"GROQ_API_KEY",
	).
		WithModel("llama-3.3-70b-versatile").
		WithTemperature(0.1).
		WithSystemPrompt("You are a helpful assistant")

	response, err := groq.Chat("What is 1+1? Answer only with the result and nothing else.")
	if err != nil {
		t.Error(err)
		return
	}

	assert.Equal(t, 1, len(response.Choices))
	assert.Equal(t, "2", response.GetLastResponse())
}

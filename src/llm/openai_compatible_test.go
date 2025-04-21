package llm

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestGroq(t *testing.T) {
	response, err := GetOpenAICompatibleResponse("What is 1+1? Answer only with the result and nothing else.")
	if err != nil {
		t.Error(err)
		return
	}

	assert.Equal(t, 1, len(response.Choices))
	assert.Equal(t, "2", response.GetLastResponse())
}

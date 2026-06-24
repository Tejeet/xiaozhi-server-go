package openai

import (
	"xiaozhi-server-go/src/core/providers/vlllm"
	"xiaozhi-server-go/src/core/utils"
)

// OpenAIVLLMProvider is the OpenAI-type VLLLM provider
type OpenAIVLLMProvider struct {
	*vlllm.Provider
}

// NewProvider creates an OpenAI VLLLM provider instance
func NewProvider(config *vlllm.Config, logger *utils.Logger) (*vlllm.Provider, error) {
	// Use the base VLLLM Provider directly, since it already reuses the LLM architecture.
	// An OpenAI-type VLLLM only needs to make sure the correct model name is used (e.g. glm-4v-flash)
	provider, err := vlllm.NewProvider(config, logger)
	if err != nil {
		return nil, err
	}

	logger.Debug("OpenAI VLLLM Provider created successfully %v", map[string]interface{}{
		"model_name": config.ModelName,
		"base_url":   config.BaseURL,
	})

	return provider, nil
}

// init registers the OpenAI VLLLM provider
func init() {
	vlllm.Register("openai", NewProvider)
}

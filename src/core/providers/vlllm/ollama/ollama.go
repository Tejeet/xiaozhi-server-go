package ollama

import (
	"xiaozhi-server-go/src/core/providers/vlllm"
	"xiaozhi-server-go/src/core/utils"
)

// OllamaVLLMProvider is the Ollama-type VLLLM provider
type OllamaVLLMProvider struct {
	*vlllm.Provider
}

// NewProvider creates an Ollama VLLLM provider instance
func NewProvider(config *vlllm.Config, logger *utils.Logger) (*vlllm.Provider, error) {
	// Use the base VLLLM Provider directly, since it already reuses the LLM architecture.
	// An Ollama-type VLLLM only needs to make sure the correct model name is used (e.g. qwen2-vl:7b)
	provider, err := vlllm.NewProvider(config, logger)
	if err != nil {
		return nil, err
	}

	logger.Debug("Ollama VLLLM Provider created successfully %v", map[string]interface{}{
		"model_name": config.ModelName,
		"base_url":   config.BaseURL,
	})

	return provider, nil
}

// init registers the Ollama VLLLM provider
func init() {
	vlllm.Register("ollama", NewProvider)
}

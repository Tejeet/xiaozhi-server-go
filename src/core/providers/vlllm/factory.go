package vlllm

import (
	"fmt"

	"xiaozhi-server-go/src/configs"
	"xiaozhi-server-go/src/core/utils"
)

// Factory is the VLLLM factory function type
type Factory func(config *Config, logger *utils.Logger) (*Provider, error)

var (
	factories = make(map[string]Factory)
)

// Register registers a VLLLM provider factory
func Register(name string, factory Factory) {
	factories[name] = factory
}

// Create creates a VLLLM provider instance
func Create(name string, vlllmConfig *configs.VLLMConfig, logger *utils.Logger) (*Provider, error) {
	factory, ok := factories[name]
	if !ok {
		return nil, fmt.Errorf("unknown VLLLM provider: %s", name)
	}

	// Convert the config format
	config := &Config{
		Type:        vlllmConfig.Type,
		ModelName:   vlllmConfig.ModelName,
		BaseURL:     vlllmConfig.BaseURL,
		APIKey:      vlllmConfig.APIKey,
		Temperature: vlllmConfig.Temperature,
		MaxTokens:   vlllmConfig.MaxTokens,
		TopP:        vlllmConfig.TopP,
		Security:    vlllmConfig.Security,
		Data:        vlllmConfig.Extra,
	}

	// Create the provider instance
	provider, err := factory(config, logger)
	if err != nil {
		return nil, fmt.Errorf("failed to create VLLLM provider: %v", err)
	}

	// Initialize the provider
	if err := provider.Initialize(); err != nil {
		return nil, fmt.Errorf("failed to initialize VLLLM provider: %v", err)
	}

	logger.Debug("VLLLM provider created successfully %v", map[string]interface{}{
		"name":       name,
		"type":       config.Type,
		"model_name": config.ModelName,
	})

	return provider, nil
}

// GetRegisteredProviders gets the list of registered providers
func GetRegisteredProviders() []string {
	var providers []string
	for name := range factories {
		providers = append(providers, name)
	}
	return providers
}

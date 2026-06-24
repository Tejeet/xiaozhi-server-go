package llm

import (
	"fmt"
	"xiaozhi-server-go/src/core/types"
)

// Config is the LLM configuration struct
type Config struct {
	Name        string                 `yaml:"name"` // LLM provider name
	Type        string                 `yaml:"type"`
	ModelName   string                 `yaml:"model_name"`
	BaseURL     string                 `yaml:"base_url,omitempty"`
	APIKey      string                 `yaml:"api_key,omitempty"`
	Temperature float64                `yaml:"temperature,omitempty"`
	MaxTokens   int                    `yaml:"max_tokens,omitempty"`
	TopP        float64                `yaml:"top_p,omitempty"`
	Extra       map[string]interface{} `yaml:",inline"`
}

// Provider is the LLM provider interface
type Provider interface {
	types.LLMProvider
}

// BaseProvider is the base LLM implementation
type BaseProvider struct {
	config    *Config
	SessionID string // Current session ID
}

// Config gets the configuration
func (p *BaseProvider) Config() *Config {
	return p.config
}

// NewBaseProvider creates a base LLM provider
func NewBaseProvider(config *Config) *BaseProvider {
	return &BaseProvider{
		config: config,
	}
}

// Initialize initializes the provider
func (p *BaseProvider) Initialize() error {
	return nil
}

// Cleanup cleans up resources
func (p *BaseProvider) Cleanup() error {
	return nil
}

func (p *BaseProvider) GetSessionID() string {
	return p.SessionID
}

func (p *BaseProvider) SetIdentityFlag(idType string, flag string) {
	// Default implementation; subclasses can override
}

// Factory is the LLM factory function type
type Factory func(config *Config) (Provider, error)

var factories = make(map[string]Factory)

// Register registers an LLM provider factory
func Register(name string, factory Factory) {
	factories[name] = factory
}

// Create creates an LLM provider instance
func Create(name string, config *Config) (Provider, error) {
	factory, ok := factories[name]
	if !ok {
		return nil, fmt.Errorf("unknown LLM provider: %s", name)
	}

	provider, err := factory(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create LLM provider: %v", err)
	}

	if err := provider.Initialize(); err != nil {
		return nil, fmt.Errorf("failed to initialize LLM provider: %v", err)
	}

	return provider, nil
}

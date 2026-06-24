package tts

import (
	"fmt"
	"os"
	"path/filepath"
	"xiaozhi-server-go/src/configs"
	"xiaozhi-server-go/src/core/providers"
	"xiaozhi-server-go/src/core/utils"
)

// Config is the TTS configuration struct
type Config struct {
	Name            string              `yaml:"name"` // TTS provider name
	Type            string              `yaml:"type"`
	OutputDir       string              `yaml:"output_dir"`
	Voice           string              `yaml:"voice,omitempty"`
	Format          string              `yaml:"format,omitempty"`
	SampleRate      int                 `yaml:"sample_rate,omitempty"`
	AppID           string              `yaml:"appid"`
	Token           string              `yaml:"token"`
	Cluster         string              `yaml:"cluster"`
	SupportedVoices []configs.VoiceInfo `yaml:"supported_voices"` // List of supported voices
}

// Provider is the TTS provider interface
type Provider interface {
	providers.TTSProvider
}

// BaseProvider is the base TTS implementation
type BaseProvider struct {
	config     *Config
	deleteFile bool
}

// Config gets the configuration
func (p *BaseProvider) Config() *Config {
	return p.config
}

// DeleteFile gets the delete-file flag
func (p *BaseProvider) DeleteFile() bool {
	return p.deleteFile
}

// NewBaseProvider creates a base TTS provider
func NewBaseProvider(config *Config, deleteFile bool) *BaseProvider {
	return &BaseProvider{
		config:     config,
		deleteFile: deleteFile,
	}
}

// Initialize initializes the provider
func (p *BaseProvider) Initialize() error {
	if err := os.MkdirAll(p.config.OutputDir, 0o755); err != nil {
		return fmt.Errorf("failed to create output directory: %v", err)
	}
	return nil
}

func IsSupportedVoice(voice string, supportedVoices []configs.VoiceInfo) (bool, string, error) {
	if voice == "" {
		return false, "", fmt.Errorf("voice cannot be empty")
	}
	cnNames := map[string]string{}
	enNames := map[string]string{}
	voiceNames := []string{}
	for _, v := range supportedVoices {
		cnNames[v.DisplayName] = v.Name // Display name
		enNames[v.Name] = v.Name        // English name (actually the timbre name)
		voiceNames = append(voiceNames, v.Name)
	}

	// If it is a display name, convert it to the timbre name
	if enVoice, ok := cnNames[voice]; ok {
		voice = enVoice
	}

	// If it is an English name, convert it to the timbre name
	if enVoice, ok := enNames[voice]; ok {
		voice = enVoice
	}

	// Check whether the voice is in the supported list
	if !utils.IsInArray(voice, voiceNames) {
		return false, "", fmt.Errorf("unsupported voice: %s, available voices: %v", voice, voiceNames)
	}

	return true, voice, nil
}

func (p *BaseProvider) SetVoice(voice string) (error, string) {
	isSupported, newVoice, err := IsSupportedVoice(voice, p.config.SupportedVoices)
	if err != nil {
		return err, ""
	}
	if !isSupported {
		return fmt.Errorf("unsupported voice: %s", voice), ""
	}
	p.Config().Voice = newVoice
	return nil, newVoice
}

// Cleanup cleans up resources
func (p *BaseProvider) Cleanup() error {
	if p.deleteFile {
		// Clean up temporary files in the output directory
		pattern := filepath.Join(p.config.OutputDir, "*.{wav,mp3,opus}")
		matches, err := filepath.Glob(pattern)
		if err != nil {
			return fmt.Errorf("failed to find temporary files: %v", err)
		}
		for _, file := range matches {
			if err := os.Remove(file); err != nil {
				return fmt.Errorf("failed to delete temporary file: %v", err)
			}
		}
	}
	return nil
}

// Factory is the TTS factory function type
type Factory func(config *Config, deleteFile bool) (Provider, error)

var factories = make(map[string]Factory)

// Register registers a TTS provider factory
func Register(name string, factory Factory) {
	factories[name] = factory
}

// Create creates a TTS provider instance
func Create(name string, config *Config, deleteFile bool) (Provider, error) {
	factory, ok := factories[name]
	if !ok {
		return nil, fmt.Errorf("unknown TTS provider: %s", name)
	}

	provider, err := factory(config, deleteFile)
	if err != nil {
		return nil, fmt.Errorf("failed to create TTS provider: %v", err)
	}

	if err := provider.Initialize(); err != nil {
		return nil, fmt.Errorf("failed to initialize TTS provider: %v", err)
	}

	return provider, nil
}

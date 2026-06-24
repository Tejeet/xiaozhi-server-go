package asr

import (
	"bytes"
	"fmt"
	"time"
	"xiaozhi-server-go/src/core/providers"
	"xiaozhi-server-go/src/core/utils"
)

// Config is the ASR configuration struct
type Config struct {
	Name string `yaml:"name"` // ASR provider name
	Type string
	Data map[string]interface{}
}

// Provider is the ASR provider interface
type Provider interface {
	providers.Provider
}

// BaseProvider is the base ASR implementation
type BaseProvider struct {
	config     *Config
	deleteFile bool

	// Audio processing related
	lastChunkTime time.Time
	audioBuffer   *bytes.Buffer

	// Silence detection configuration
	silenceThreshold float64 // Energy threshold
	silenceDuration  int     // Silence duration (ms)

	BEnableSilenceDetection bool      // Whether silence detection is enabled
	StartListenTime         time.Time // Time of the last ASR processing
	SilenceCount            int       // Consecutive silence count

	UserPreferences map[string]interface{}

	listener providers.AsrEventListener
}

func (p *BaseProvider) ResetStartListenTime() {
	p.StartListenTime = time.Now()
}

func (p *BaseProvider) SilenceTime() time.Duration {
	if !p.BEnableSilenceDetection {
		return 0
	}
	if p.StartListenTime.IsZero() {
		return 0
	}
	return time.Since(p.StartListenTime)
}

func (p *BaseProvider) EnableSilenceDetection(bEnable bool) {
	p.BEnableSilenceDetection = bEnable
}

func (p *BaseProvider) GetSilenceCount() int {
	return p.SilenceCount
}

func (p *BaseProvider) ResetSilenceCount() {
	p.SilenceCount = 0
}

// SetListener sets the event listener
func (p *BaseProvider) SetListener(listener providers.AsrEventListener) {
	p.listener = listener
}

// GetListener gets the event listener
func (p *BaseProvider) GetListener() providers.AsrEventListener {
	return p.listener
}

func (p *BaseProvider) SetUserPreferences(preferences map[string]interface{}) error {
	p.UserPreferences = preferences
	return nil
}

// Config gets the configuration
func (p *BaseProvider) Config() *Config {
	return p.config
}

// GetAudioBuffer gets the audio buffer
func (p *BaseProvider) GetAudioBuffer() *bytes.Buffer {
	return p.audioBuffer
}

// GetLastChunkTime gets the time of the last audio chunk
func (p *BaseProvider) GetLastChunkTime() time.Time {
	return p.lastChunkTime
}

// SetLastChunkTime sets the time of the last audio chunk
func (p *BaseProvider) SetLastChunkTime(t time.Time) {
	p.lastChunkTime = t
}

// DeleteFile gets the delete-file flag
func (p *BaseProvider) DeleteFile() bool {
	return p.deleteFile
}

// NewBaseProvider creates a base ASR provider
func NewBaseProvider(config *Config, deleteFile bool) *BaseProvider {
	return &BaseProvider{
		config:     config,
		deleteFile: deleteFile,
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

// Factory is the ASR factory function type
type Factory func(config *Config, deleteFile bool, logger *utils.Logger) (Provider, error)

var factories = make(map[string]Factory)

// Register registers an ASR provider factory
func Register(name string, factory Factory) {
	factories[name] = factory
}

// Create creates an ASR provider instance
func Create(name string, config *Config, deleteFile bool, logger *utils.Logger) (Provider, error) {
	factory, ok := factories[name]
	if !ok {
		return nil, fmt.Errorf("unknown ASR provider: %s", name)
	}

	provider, err := factory(config, deleteFile, logger)
	if err != nil {
		return nil, fmt.Errorf("failed to create ASR provider: %v", err)
	}

	return provider, nil
}

// InitAudioProcessing initializes audio processing
func (p *BaseProvider) InitAudioProcessing() {
	p.audioBuffer = new(bytes.Buffer)
	p.silenceThreshold = 0.01 // Default energy threshold
	p.silenceDuration = 800   // Default silence-detection duration (ms)
}

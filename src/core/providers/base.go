package providers

import (
	"context"
	"xiaozhi-server-go/src/core/types"
)

// Provider is the base interface for all providers
type Provider interface {
	Initialize() error
	Cleanup() error
}

type AsrEventListener interface {
	OnAsrResult(result string, isFinalResult bool) bool
}

// ASRProvider is the speech-recognition provider interface
type ASRProvider interface {
	Provider
	// Directly transcribe audio data
	Transcribe(ctx context.Context, audioData []byte) (string, error)
	// Add audio data to the buffer
	AddAudio(data []byte) error

	// Send the last audio chunk and mark it as the end
	SendLastAudio(data []byte) error

	SetListener(listener AsrEventListener)

	// Set user preferences, such as language
	SetUserPreferences(preferences map[string]interface{}) error

	// Reset the ASR state
	Reset() error

	// Disconnect the long-lived ASR connection
	CloseConnection() error

	// Get the current silence count
	GetSilenceCount() int

	ResetSilenceCount()

	ResetStartListenTime()

	EnableSilenceDetection(bEnable bool)
}

// TTSProvider is the speech-synthesis provider interface
type TTSProvider interface {
	Provider

	// Synthesize audio and return the file path
	ToTTS(text string) (string, error)

	SetVoice(voice string) (error, string)
}

// LLMProvider is the large-language-model provider interface
type LLMProvider interface {
	types.LLMProvider
}

// Message is a conversation message
type Message = types.Message

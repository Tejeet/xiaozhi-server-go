package deepgram

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"time"
	"xiaozhi-server-go/src/core/providers/tts"

	"github.com/gorilla/websocket"
)

// Provider is the Deepgram TTS provider
type Provider struct {
	*tts.BaseProvider
	baseURL string
}

// NewProvider creates a Deepgram TTS provider
func NewProvider(config *tts.Config, deleteFile bool) (*Provider, error) {
	base := tts.NewBaseProvider(config, deleteFile)

	// Build the URL with parameters
	// u := fmt.Sprintf("%v?model=%s&encoding=%s&sample_rate=%d",
	u := fmt.Sprintf("%v?model=%s", config.Cluster, config.Voice)
	return &Provider{
		BaseProvider: base,
		baseURL:      u,
	}, nil
}

// ToTTS implements text-to-speech conversion
func (p *Provider) ToTTS(text string) (string, error) {
	// Create the WebSocket connection
	header := http.Header{"Authorization": []string{fmt.Sprintf("token %s", p.Config().Token)}}
	conn, _, err := websocket.DefaultDialer.Dial(p.baseURL, header)
	if err != nil {
		return "", fmt.Errorf("failed to connect to the Deepgram TTS server: %v", err)
	}
	defer conn.Close()

	// Send the text message
	speakRequest := map[string]string{
		"type": "Speak",
		"text": text,
	}
	requestBytes, err := json.Marshal(speakRequest)
	if err != nil {
		return "", fmt.Errorf("failed to serialize the request: %v", err)
	}

	if err := conn.WriteMessage(websocket.TextMessage, requestBytes); err != nil {
		return "", fmt.Errorf("failed to send the speak request: %v", err)
	}

	// Send a Flush control message to ensure all audio data is returned
	flushRequest := map[string]string{"type": "Flush"}
	if err := conn.WriteJSON(flushRequest); err != nil {
		return "", fmt.Errorf("failed to send the Flush request: %v", err)
	}

	// Create the temp file
	outputDir := p.Config().OutputDir
	if outputDir == "" {
		outputDir = "tmp"
	}
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create output directory: %v", err)
	}

	// ext := getFileExtension(p.Config().Encoding)
	ext := "mp3"
	tempFile := filepath.Join(outputDir, fmt.Sprintf("deepgram_tts_%d.%s", time.Now().UnixNano(), ext))
	// Receive the audio data
	var lastSeqID int
	// Receive the audio data
	var audioBuffer bytes.Buffer
loop:
	for {
		messageType, message, err := conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseNormalClosure) {
				return "", fmt.Errorf("error receiving response: %v", err)
			}
			break // Normal close
		}

		switch messageType {
		case websocket.TextMessage:
			// Handle control message responses
			var response struct {
				Type       string `json:"type"`
				SequenceID int    `json:"sequence_id,omitempty"`
				Error      string `json:"error,omitempty"`
			}

			if err := json.Unmarshal(message, &response); err != nil {
				return "", fmt.Errorf("failed to parse the control message: %v", err)
			}

			switch response.Type {
			case "Flushed":
				// Record the last sequence ID
				lastSeqID = response.SequenceID
				break loop
			case "close":
				// Server confirmed close
				break loop
			case "error":
				return "", fmt.Errorf("Deepgram TTS error: %s", response.Error)
			}
		case websocket.BinaryMessage:
			// Binary audio data
			// Write the binary audio data directly to the file
			// while also buffering it in memory for an integrity check
			audioBuffer.Write(message)
		case websocket.CloseMessage:
			break loop
		}
	}

	// Verify audio integrity (optional)
	// Check whether audio data was received
	if lastSeqID > 0 && audioBuffer.Len() == 0 {
		return "", fmt.Errorf("audio data is incomplete, last received sequence number: %d", lastSeqID)
	}

	// Write the audio file
	if err := os.WriteFile(tempFile, audioBuffer.Bytes(), 0644); err != nil {
		return "", fmt.Errorf("failed to write the audio file: %v", err)
	}

	return tempFile, nil
}

// getFileExtension gets the file extension based on the encoding
func getFileExtension(encoding string) string {
	switch encoding {
	case "linear16":
		return "wav"
	case "opus":
		return "opus"
	case "flac":
		return "flac"
	case "aac":
		return "aac"
	case "alaw", "mulaw":
		return "wav"
	default: // mp3
		return "mp3"
	}
}

func init() {
	tts.Register("deepgram", func(config *tts.Config, deleteFile bool) (tts.Provider, error) {
		return NewProvider(config, deleteFile)
	})
}

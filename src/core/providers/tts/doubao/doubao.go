package doubao

import (
	"bytes"
	"compress/gzip"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"time"

	"xiaozhi-server-go/src/core/providers/tts"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
)

var (
	enumMessageType = map[byte]string{
		11: "audio-only server response",
		12: "frontend server response",
		15: "error message from server",
	}
	enumMessageTypeSpecificFlags = map[byte]string{
		0: "no sequence number",
		1: "sequence number > 0",
		2: "last message from server (seq < 0)",
		3: "sequence number < 0",
	}
)

// Default binary message header
// version: b0001 (4 bits)
// header size: b0001 (4 bits)
// message type: b0001 (Full client request) (4bits)
// message type specific flags: b0000 (none) (4bits)
// message serialization method: b0001 (JSON) (4bits)
// message compression: b0001 (gzip) (4bits)
// reserved data: 0x00 (1 byte)
var defaultHeader = []byte{0x11, 0x10, 0x11, 0x00}

type synResp struct {
	Audio  []byte
	IsLast bool
}

// Provider is the Doubao TTS provider
type Provider struct {
	*tts.BaseProvider
	baseURL string
}

// NewProvider creates a Doubao TTS provider
func NewProvider(config *tts.Config, deleteFile bool) (*Provider, error) {
	base := tts.NewBaseProvider(config, deleteFile)
	u := url.URL{Scheme: "wss", Host: "openspeech.bytedance.com", Path: "/api/v1/tts/ws_binary"}

	return &Provider{
		BaseProvider: base,
		baseURL:      u.String(),
	}, nil
}

// ToTTS implements text-to-speech conversion
func (p *Provider) ToTTS(text string) (string, error) {
	// Create the WebSocket connection
	header := http.Header{"Authorization": []string{fmt.Sprintf("Bearer;%s", p.Config().Token)}}
	conn, _, err := websocket.DefaultDialer.Dial(p.baseURL, header)
	if err != nil {
		return "", fmt.Errorf("failed to connect to the WebSocket server: %v", err)
	}
	defer conn.Close()

	// Prepare the request parameters
	reqParams := map[string]map[string]interface{}{
		"app": {
			"appid":   p.Config().AppID,
			"token":   p.Config().Token,
			"cluster": p.Config().Cluster,
		},
		"user": {
			"uid": "uid",
		},
		"audio": {
			"voice_type":   p.Config().Voice,
			"encoding":     "mp3",
			"speed_ratio":  1.0,
			"volume_ratio": 1.0,
			"pitch_ratio":  1.0,
		},
		"request": {
			"reqid":     uuid.New().String(),
			"text":      text,
			"text_type": "plain",
			"operation": "submit", // Use streaming synthesis
		},
	}

	// Serialize and compress the request parameters
	jsonData, err := json.Marshal(reqParams)
	if err != nil {
		return "", fmt.Errorf("failed to serialize the request parameters: %v", err)
	}

	var b bytes.Buffer
	w := gzip.NewWriter(&b)
	if _, err := w.Write(jsonData); err != nil {
		return "", fmt.Errorf("failed to compress the request data: %v", err)
	}
	w.Close()
	compressed := b.Bytes()

	// Build the complete binary request
	payloadSize := make([]byte, 4)
	binary.BigEndian.PutUint32(payloadSize, uint32(len(compressed)))
	request := make([]byte, len(defaultHeader))
	copy(request, defaultHeader)
	request = append(request, payloadSize...)
	request = append(request, compressed...)

	// Send the request
	if err := conn.WriteMessage(websocket.BinaryMessage, request); err != nil {
		return "", fmt.Errorf("failed to send the request: %v", err)
	}

	// Create the temp file
	outputDir := p.Config().OutputDir
	if outputDir == "" {
		outputDir = "tmp"
	}
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create output directory: %v", err)
	}

	tempFile := filepath.Join(outputDir, fmt.Sprintf("doubao_tts_%d.mp3", time.Now().UnixNano()))
	var audioData []byte

	// Receive the audio data
	for {
		_, message, err := conn.ReadMessage()
		if err != nil {
			return "", fmt.Errorf("failed to receive response: %v", err)
		}

		resp, err := p.parseResponse(message)
		if err != nil {
			return "", fmt.Errorf("failed to parse response: %v", err)
		}

		audioData = append(audioData, resp.Audio...)
		if resp.IsLast {
			break
		}
	}

	// Write the audio file
	if err := os.WriteFile(tempFile, audioData, 0644); err != nil {
		return "", fmt.Errorf("failed to write the audio file: %v", err)
	}

	return tempFile, nil
}

// parseResponse parses the server response
func (p *Provider) parseResponse(res []byte) (resp synResp, err error) {
	if len(res) < 4 {
		return resp, fmt.Errorf("response data is too short")
	}

	messageType := res[1] >> 4
	messageTypeSpecificFlags := res[1] & 0x0f
	headSize := res[0] & 0x0f
	payload := res[headSize*4:]

	switch messageType {
	case 0xb: // audio-only server response
		if messageTypeSpecificFlags != 0 {
			// Response with a sequence number
			if len(payload) < 8 {
				return resp, fmt.Errorf("audio data is too short")
			}
			sequenceNumber := int32(binary.BigEndian.Uint32(payload[0:4]))
			payload = payload[8:]
			resp.Audio = append(resp.Audio, payload...)
			if sequenceNumber < 0 {
				resp.IsLast = true
			}
		}
	case 0xf: // error message
		if len(payload) < 8 {
			return resp, fmt.Errorf("error message data is too short")
		}
		code := int32(binary.BigEndian.Uint32(payload[0:4]))
		errMsg := payload[8:]
		// Always try gzip decompression
		r, gzErr := gzip.NewReader(bytes.NewReader(errMsg))
		if gzErr == nil {
			if errMsg2, err2 := io.ReadAll(r); err2 == nil {
				errMsg = errMsg2
			}
			r.Close()
		}
		return resp, fmt.Errorf("server error [%d]: %s", code, string(errMsg))
	default:
		return resp, fmt.Errorf("unknown message type: %d", messageType)
	}

	return resp, nil
}

func init() {
	tts.Register("doubao", func(config *tts.Config, deleteFile bool) (tts.Provider, error) {
		return NewProvider(config, deleteFile)
	})
}

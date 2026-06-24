package gosherpa

import (
	"context"
	"github.com/gorilla/websocket"
	"time"
	"xiaozhi-server-go/src/core/providers/asr"
	"xiaozhi-server-go/src/core/utils"
)

type Provider struct {
	*asr.BaseProvider
	conn *websocket.Conn
}

func NewProvider(config *asr.Config, deleteFile bool, logger *utils.Logger) (*Provider, error) {
	base := asr.NewBaseProvider(config, deleteFile)

	provider := &Provider{
		BaseProvider: base,
	}
	// Initialize audio processing
	provider.InitAudioProcessing()
	dialer := websocket.Dialer{
		HandshakeTimeout: 10 * time.Second, // Set the handshake timeout
	}
	conn, _, err := dialer.DialContext(context.Background(), config.Data["addr"].(string), map[string][]string{})
	if err != nil {
		return nil, err
	}
	provider.conn = conn
	go func() {
		defer func() {
			if err := recover(); err != nil {
			}
		}()
		for {
			messageType, p, _ := conn.ReadMessage()
			if messageType == websocket.TextMessage {
				if listener := provider.GetListener(); listener != nil {
					if finished := listener.OnAsrResult(string(p), true); finished {
					}
				}
			}
		}
	}()

	return provider, nil
}

func (p *Provider) Transcribe(ctx context.Context, audioData []byte) (string, error) {
	return "", nil
}

// AddAudio adds audio data to the buffer
func (p *Provider) AddAudio(data []byte) error {
	p.conn.WriteMessage(websocket.BinaryMessage, data)

	return nil
}

// Reset resets the ASR state
func (p *Provider) Reset() error {
	return nil
}

func (p *Provider) CloseConnection() error {
	return nil
}

func init() {
	asr.Register("gosherpa", func(config *asr.Config, deleteFile bool, logger *utils.Logger) (asr.Provider, error) {
		return NewProvider(config, deleteFile, logger)
	})
}

package interfaces

// Conn is the WebSocket connection interface
type Conn interface {
	ReadMessage(stopChan <-chan struct{}) (messageType int, p []byte, err error)
	WriteMessage(messageType int, data []byte) error
}

// ConnectionHandler is the connection-handler interface
type ConnectionHandler interface {
	Handle(conn Conn)
	SpeakAndPlay(text string, textIndex int) error
	Close()
}

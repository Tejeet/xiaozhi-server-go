package types

type AudioChunk struct {
	Text      string // Associated text content
	Index     int    // Text index, used to mark the order of text within this round
	Round     int    // Round number, used to mark the conversation round
	Data      []byte // Audio data
	Timestamp int64  // Timestamp, used to mark the order of data chunks
	EOF       bool   // Whether this is the last data chunk
	Encoding  string // Audio encoding format, e.g. "mp3", "wav"
}

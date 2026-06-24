package chat

// MemoryInterface defines the conversation-memory management interface
type MemoryInterface interface {
	// QueryMemory queries relevant memory
	QueryMemory(query string) (string, error)

	// SaveMemory saves conversation memory
	SaveMemory(dialogue []Message) error

	// ClearMemory clears the memory
	ClearMemory() error
}

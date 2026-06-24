package chat

import (
	"encoding/json"

	"xiaozhi-server-go/src/core/types"
	"xiaozhi-server-go/src/core/utils"
)

type Message = types.Message

// DialogueManager manages conversation context and history
type DialogueManager struct {
	logger   *utils.Logger
	dialogue []Message
	memory   MemoryInterface
}

// NewDialogueManager creates a dialogue manager instance
func NewDialogueManager(logger *utils.Logger, memory MemoryInterface) *DialogueManager {
	return &DialogueManager{
		logger:   logger,
		dialogue: make([]Message, 0),
		memory:   memory,
	}
}

func (dm *DialogueManager) SetSystemMessage(systemMessage string) {
	if systemMessage == "" {
		return
	}

	// If the dialogue already has a system message, don't add another one
	if len(dm.dialogue) > 0 && dm.dialogue[0].Role == "system" {
		dm.dialogue[0].Content = systemMessage
		return
	}

	// Add the new system message to the beginning of the dialogue
	dm.dialogue = append([]Message{
		{Role: "system", Content: systemMessage},
	}, dm.dialogue...)
}

func (dm *DialogueManager) RemoveSecondMessageForToolType() {
	// If the second message has the type "role": "tool", remove it
	if len(dm.dialogue) < 2 || dm.dialogue[1].Role != "tool" {
		return
	}
	dm.dialogue = append(dm.dialogue[:1], dm.dialogue[2:]...)
}

// KeepRecentMessages keeps only the most recent few dialogue messages
func (dm *DialogueManager) KeepRecentMessages(maxMessages int) {
	if maxMessages <= 0 || len(dm.dialogue) <= maxMessages {
		return
	}
	// Keep the system message and the most recent maxMessages messages
	if len(dm.dialogue) > 0 && dm.dialogue[0].Role == "system" {
		// Keep the system message
		dm.dialogue = append(dm.dialogue[:1], dm.dialogue[len(dm.dialogue)-maxMessages:]...)
		dm.RemoveSecondMessageForToolType()
		return
	}
	// If there is no system message, just keep the most recent maxMessages messages
	if len(dm.dialogue) > maxMessages {
		dm.dialogue = dm.dialogue[len(dm.dialogue)-maxMessages:]
	}
}

// GetRecentMessages gets the most recent dialogue messages.
// If maxMessages <= 0, it returns all dialogue messages.
func (dm *DialogueManager) GetRecentMessages(maxMessages int) []Message {
	if maxMessages <= 0 || len(dm.dialogue) <= maxMessages {
		return dm.dialogue
	}
	// Keep the system message and the most recent maxMessages messages
	if len(dm.dialogue) > 0 && dm.dialogue[0].Role == "system" {
		// Keep the system message
		return append([]Message{dm.dialogue[0]}, dm.dialogue[len(dm.dialogue)-maxMessages:]...)
	}
	return dm.dialogue
}

// Put adds a new message to the dialogue
func (dm *DialogueManager) Put(message Message) {
	// If the last message is a user message and the current one is also a user message, insert an empty assistant message
	if len(dm.dialogue) > 0 && dm.dialogue[len(dm.dialogue)-1].Role == "user" && message.Role == "user" {
		dm.dialogue = append(dm.dialogue, Message{Role: "assistant", Content: "..."})
	}
	dm.dialogue = append(dm.dialogue, message)
}

func (dm *DialogueManager) GetLastTwoMessages() []Message {
	if len(dm.dialogue) < 2 {
		return nil
	}
	return dm.dialogue[len(dm.dialogue)-2:]
}

// GetLLMDialogue gets the full conversation history
func (dm *DialogueManager) GetLLMDialogue() []Message {
	return dm.dialogue
}

// GetLLMDialogueWithMemory gets the conversation with memory included
func (dm *DialogueManager) GetLLMDialogueWithMemory(memoryStr string) []Message {
	if memoryStr == "" {
		return dm.GetLLMDialogue()
	}

	memoryMsg := Message{
		Role:    "system",
		Content: memoryStr,
	}

	dialogue := make([]Message, 0, len(dm.dialogue)+1)
	dialogue = append(dialogue, memoryMsg)
	dialogue = append(dialogue, dm.dialogue...)

	return dialogue
}

// Clear clears the conversation history
func (dm *DialogueManager) Clear() {
	dm.dialogue = make([]Message, 0)
}

func (dm *DialogueManager) Length() int {
	return len(dm.dialogue)
}

// ToJSON converts the conversation history to a JSON string
func (dm *DialogueManager) ToJSON(keepSystemPrompt bool) (string, error) {
	dialogue := dm.dialogue
	if !keepSystemPrompt && len(dialogue) > 0 && dialogue[0].Role == "system" {
		// If not keeping the system message, remove the first message
		dialogue = dialogue[1:]
	}
	bytes, err := json.Marshal(dialogue)
	if err != nil {
		return "", err
	}
	return string(bytes), nil
}

// LoadFromJSON loads the conversation history from a JSON string
func (dm *DialogueManager) LoadFromJSON(jsonStr string) error {
	return json.Unmarshal([]byte(jsonStr), &dm.dialogue)
}

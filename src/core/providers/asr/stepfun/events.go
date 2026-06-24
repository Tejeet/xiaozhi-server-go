package stepfun

// BaseEvent holds common event fields
type BaseEvent struct {
	EventID string `json:"event_id,omitempty"`
	Type    string `json:"type"`
}

// Error event
type ErrorDetail struct {
	Type    string `json:"type"`
	Code    string `json:"code,omitempty"`
	Message string `json:"message"`
	EventID string `json:"event_id,omitempty"`
}

type ErrorEvent struct {
	BaseEvent
	Error ErrorDetail `json:"error"`
}

// Session is the session object
type Session struct {
	ID                      string   `json:"id,omitempty"`
	Object                  string   `json:"object,omitempty"`
	Model                   string   `json:"model,omitempty"`
	Modalities              []string `json:"modalities,omitempty"`
	Instructions            string   `json:"instructions,omitempty"`
	Voice                   string   `json:"voice,omitempty"`
	InputAudioFormat        string   `json:"input_audio_format,omitempty"`
	OutputAudioFormat       string   `json:"output_audio_format,omitempty"`
	MaxResponseOutputTokens string   `json:"max_response_output_tokens,omitempty"`
}

// Session-related events
type SessionCreatedEvent struct {
	BaseEvent
	Session Session `json:"session"`
}

type SessionUpdatedEvent struct {
	BaseEvent
	Session Session `json:"session"`
}

// VAD events
type SpeechStartedEvent struct {
	BaseEvent
	AudioStartMS int64  `json:"audio_start_ms,omitempty"`
	ItemID       string `json:"item_id,omitempty"`
}

type SpeechStoppedEvent struct {
	BaseEvent
	AudioEndMS int64  `json:"audio_end_ms,omitempty"`
	ItemID     string `json:"item_id,omitempty"`
	ResponseID string `json:"response_id,omitempty"`
}

// Streaming audio content events
type ResponseAudioDeltaEvent struct {
	BaseEvent
	ResponseID  string `json:"response_id,omitempty"`
	ItemID      string `json:"item_id,omitempty"`
	OutputIndex int    `json:"output_index,omitempty"`
	Delta       string `json:"delta"`
}

type ResponseAudioDoneEvent struct {
	BaseEvent
	ResponseID string `json:"response_id,omitempty"`
	ItemID     string `json:"item_id,omitempty"`
}

// Streaming audio transcription events
type ResponseAudioTranscriptDeltaEvent struct {
	BaseEvent
	ResponseID  string `json:"response_id,omitempty"`
	ItemID      string `json:"item_id,omitempty"`
	OutputIndex int    `json:"output_index,omitempty"`
	Delta       string `json:"delta"`
}

type ResponseAudioTranscriptDoneEvent struct {
	BaseEvent
	ResponseID   string `json:"response_id,omitempty"`
	ItemID       string `json:"item_id,omitempty"`
	OutputIndex  int    `json:"output_index,omitempty"`
	ContentIndex int    `json:"content_index,omitempty"`
	Transcript   string `json:"transcript"`
}

// Conversation message structures
type MessageContentPart struct {
	Type       string `json:"type"`
	Text       string `json:"text,omitempty"`
	Audio      string `json:"audio,omitempty"`
	Transcript string `json:"transcript,omitempty"`
}

type MessageItem struct {
	ID      string               `json:"id,omitempty"`
	Object  string               `json:"object,omitempty"`
	Type    string               `json:"type"`
	Status  string               `json:"status,omitempty"`
	Role    string               `json:"role,omitempty"`
	Content []MessageContentPart `json:"content,omitempty"`
}

// Conversation message events
type ConversationItemCreatedEvent struct {
	BaseEvent
	PreviousItemID string      `json:"previous_item_id,omitempty"`
	Item           MessageItem `json:"item"`
}

type ConversationItemDeletedEvent struct {
	BaseEvent
	ItemID string `json:"item_id"`
}

type ConversationItemInputAudioTranscriptionCompletedEvent struct {
	BaseEvent
	ItemID       string `json:"item_id"`
	ContentIndex int    `json:"content_index"`
	Transcript   string `json:"transcript"`
}

// Input audio buffer events
type InputAudioBufferCommittedEvent struct {
	BaseEvent
	PreviousItemID string `json:"previous_item_id,omitempty"`
	ItemID         string `json:"item_id"`
}

type InputAudioBufferClearedEvent struct {
	BaseEvent
}

// Inference output item events
type ResponseOutputItemAddedEvent struct {
	BaseEvent
	ResponseID  string      `json:"response_id,omitempty"`
	OutputIndex int         `json:"output_index"`
	Item        MessageItem `json:"item"`
}

type ResponseOutputItemDoneEvent struct {
	BaseEvent
	ResponseID  string      `json:"response_id,omitempty"`
	OutputIndex int         `json:"output_index"`
	Item        MessageItem `json:"item"`
}

type ResponseContentPartAddedEvent struct {
	BaseEvent
	ResponseID   string             `json:"response_id,omitempty"`
	ItemID       string             `json:"item_id,omitempty"`
	OutputIndex  int                `json:"output_index,omitempty"`
	ContentIndex int                `json:"content_index"`
	Part         MessageContentPart `json:"part"`
}

type ResponseContentPartDoneEvent struct {
	BaseEvent
	ResponseID   string             `json:"response_id,omitempty"`
	ItemID       string             `json:"item_id,omitempty"`
	OutputIndex  int                `json:"output_index,omitempty"`
	ContentIndex int                `json:"content_index"`
	Part         MessageContentPart `json:"part"`
}

// Response object and events
type Response struct {
	ID            string        `json:"id"`
	Object        string        `json:"object"`
	Status        string        `json:"status"`
	StatusDetails interface{}   `json:"status_details"`
	Output        []MessageItem `json:"output"`
}

type ResponseCreatedEvent struct {
	BaseEvent
	Response Response `json:"response,omitempty"`
}

type ResponseDoneEvent struct {
	BaseEvent
	Response Response `json:"response"`
}

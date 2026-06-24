package types

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/sashabaranov/go-openai"
)

// ToolType represents the type of tool operation.
type ToolType int

const (
	ToolNone            ToolType = iota + 1 // 1
	ToolWait                                // 2
	ToolChangeSysPrompt                     // 3
	ToolSystemCtl                           // 4
	ToolIotCtl                              // 5
	ToolMcpClient                           // 6
)

var ToolTypeMessages = map[ToolType]string{
	ToolNone:            "Do nothing else after calling the tool",
	ToolWait:            "Call the tool and wait for the function to return",
	ToolChangeSysPrompt: "Modify the system prompt, switching the role's personality or responsibilities",
	ToolSystemCtl:       "System control that affects the normal conversation flow, such as exiting or playing music; requires passing the conn parameter",
	ToolIotCtl:          "IoT device control; requires passing the conn parameter",
	ToolMcpClient:       "MCP client",
}

// Action represents the type of action.
type Action int

const (
	ActionTypeError       Action = -1
	ActionTypeNotFound    Action = 0
	ActionTypeNone        Action = 1
	ActionTypeResponse    Action = 2
	ActionTypeReqLLM      Action = 3
	ActionTypeCallHandler Action = 4
)

var ActionDesc = map[Action]string{
	ActionTypeError:    "Error",
	ActionTypeNotFound: "Function not found",
	ActionTypeNone:     "Do nothing",
	ActionTypeResponse: "Reply directly",
	ActionTypeReqLLM:   "Call the function, then request the LLM to generate a reply",
}

// ActionResponse holds the result of an action.
type ActionResponse struct {
	Action   Action      // Action type
	Result   interface{} // Result produced by the action
	Response interface{} // Content of a direct reply
}

type ActionResponseCall struct {
	FuncName string      // Function name
	Args     interface{} // Function arguments
}

// Message is the conversation message structure
type Message struct {
	Role       string     `json:"role"`
	Content    string     `json:"content"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
}

func (m *Message) Print() {
	// Convert to a JSON string
	jsonStr, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		fmt.Println("json marshal error:", err)
		return
	}
	// fmt.Println("Message:")
	fmt.Println(string(jsonStr))
}

// ToolCall is the tool-call structure
type ToolCall struct {
	ID       string       `json:"id"`
	Type     string       `json:"type"`
	Function FunctionCall `json:"function"`
	Index    int          `json:"index"`
}

// FunctionCall is the function-call result
type FunctionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// Response is the LLM response structure
type Response struct {
	Content              string     `json:"content,omitempty"`
	ReasonContent        string     `json:"reasoning_content,omitempty"`
	UpdateConversationID string     `json:"update_conversation_id,omitempty"`
	ToolCalls            []ToolCall `json:"tool_calls,omitempty"`
	StopReason           string     `json:"stop_reason,omitempty"`
	Error                string     `json:"error,omitempty"`
}

// Provider is the base provider interface
type Provider interface {
	Initialize() error
	Cleanup() error
}

type FunctionRegistryInterface interface {
	RegisterFunction(name string, function openai.Tool) error
	GetFunction(name string) (openai.Tool, error)
	GetAllFunctions() []openai.Tool
	UnregisterFunction(name string) error
	UnregisterAllFunctions() error
	FunctionExists(name string) bool
}

// LLMProvider is the large-language-model provider interface
type LLMProvider interface {
	Provider
	Response(ctx context.Context, sessionID string, messages []Message) (<-chan string, error)
	ResponseWithFunctions(
		ctx context.Context,
		sessionID string,
		messages []Message,
		tools []openai.Tool,
	) (<-chan Response, error)
	GetSessionID() string                       // Get the current session ID
	SetIdentityFlag(idType string, flag string) // Set the identity flag
}

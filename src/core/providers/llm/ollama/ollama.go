package ollama

import (
	"context"
	"fmt"
	"strings"
	"xiaozhi-server-go/src/core/providers/llm"
	"xiaozhi-server-go/src/core/types"

	"github.com/sashabaranov/go-openai"
)

// Provider is the Ollama LLM provider
type Provider struct {
	*llm.BaseProvider
	client    *openai.Client
	modelName string
	isQwen3   bool
}

// Register the provider
func init() {
	llm.Register("ollama", NewProvider)
}

// NewProvider creates an Ollama provider
func NewProvider(config *llm.Config) (llm.Provider, error) {
	base := llm.NewBaseProvider(config)
	provider := &Provider{
		BaseProvider: base,
		modelName:    config.ModelName,
	}

	// Check whether it is a qwen3 model
	provider.isQwen3 = config.ModelName != "" && strings.HasPrefix(strings.ToLower(config.ModelName), "qwen3")

	return provider, nil
}

// Initialize initializes the provider
func (p *Provider) Initialize() error {
	config := p.Config()
	baseURL := config.BaseURL
	if baseURL == "" {
		// Try to get it from the url field
		if url, ok := config.Extra["url"].(string); ok {
			baseURL = url
		}
	}
	if baseURL == "" {
		return fmt.Errorf("missing Ollama base URL configuration")
	}

	// Make sure the URL ends with /v1
	if !strings.HasSuffix(baseURL, "/v1") {
		baseURL = baseURL + "/v1"
	}

	// Ollama doesn't need a real API key, but the openai client requires a value
	clientConfig := openai.DefaultConfig("ollama")
	clientConfig.BaseURL = baseURL

	p.client = openai.NewClientWithConfig(clientConfig)
	return nil
}

// Cleanup cleans up resources
func (p *Provider) Cleanup() error {
	return nil
}

// Response implements the types.LLMProvider interface
func (p *Provider) Response(ctx context.Context, sessionID string, messages []types.Message) (<-chan string, error) {
	responseChan := make(chan string, 10)

	go func() {
		defer close(responseChan)

		// For qwen3 models, add the /no_think directive to the user's last message
		if p.isQwen3 {
			messages = p.addNoThinkDirective(messages)
		}

		// Convert the message format
		chatMessages := make([]openai.ChatCompletionMessage, len(messages))
		for i, msg := range messages {
			chatMessages[i] = openai.ChatCompletionMessage{
				Role:    msg.Role,
				Content: msg.Content,
			}
		}

		stream, err := p.client.CreateChatCompletionStream(
			ctx,
			openai.ChatCompletionRequest{
				Model:    p.modelName,
				Messages: chatMessages,
				Stream:   true,
			},
		)
		if err != nil {
			responseChan <- fmt.Sprintf("[Ollama service response error: %v]", err)
			return
		}
		defer stream.Close()

		isActive := true
		buffer := ""

		for {
			response, err := stream.Recv()
			if err != nil {
				break
			}

			if len(response.Choices) > 0 {
				content := response.Choices[0].Delta.Content
				if content != "" {
					// Add the content to the buffer
					buffer += content

					// Handle the tags in the buffer
					buffer, isActive = p.handleThinkTagsWithBuffer(buffer, isActive)

					// If currently active and the buffer has content, output it
					if isActive && buffer != "" {
						responseChan <- buffer
						buffer = ""
					}
				}
			}
		}
	}()

	return responseChan, nil
}

// ResponseWithFunctions implements the types.LLMProvider interface
func (p *Provider) ResponseWithFunctions(ctx context.Context, sessionID string, messages []types.Message, tools []openai.Tool) (<-chan types.Response, error) {
	responseChan := make(chan types.Response, 10)

	go func() {
		defer close(responseChan)

		// For qwen3 models, add the /no_think directive to the user's last message
		if p.isQwen3 {
			messages = p.addNoThinkDirective(messages)
		}

		// Convert the message format
		chatMessages := make([]openai.ChatCompletionMessage, len(messages))
		for i, msg := range messages {
			chatMessage := openai.ChatCompletionMessage{
				Role:    msg.Role,
				Content: msg.Content,
			}

			// Handle the tool_call_id field (required for tool messages)
			if msg.ToolCallID != "" {
				chatMessage.ToolCallID = msg.ToolCallID
			}

			// Handle the tool_calls field (tool calls in assistant messages)
			if len(msg.ToolCalls) > 0 {
				openaiToolCalls := make([]openai.ToolCall, len(msg.ToolCalls))
				for j, tc := range msg.ToolCalls {
					openaiToolCalls[j] = openai.ToolCall{
						ID:   tc.ID,
						Type: openai.ToolType(tc.Type),
						Function: openai.FunctionCall{
							Name:      tc.Function.Name,
							Arguments: tc.Function.Arguments,
						},
					}
				}
				chatMessage.ToolCalls = openaiToolCalls
			}

			chatMessages[i] = chatMessage
		}

		stream, err := p.client.CreateChatCompletionStream(
			ctx,
			openai.ChatCompletionRequest{
				Model:    p.modelName,
				Messages: chatMessages,
				Tools:    tools,
				Stream:   true,
			},
		)
		if err != nil {
			responseChan <- types.Response{
				Content: fmt.Sprintf("[Ollama service response error: %v]", err),
				Error:   err.Error(),
			}
			return
		}
		defer stream.Close()

		isActive := true
		buffer := ""

		for {
			response, err := stream.Recv()
			if err != nil {
				break
			}

			if len(response.Choices) > 0 {
				delta := response.Choices[0].Delta

				// Handle the tool calls
				if len(delta.ToolCalls) > 0 {
					toolCalls := make([]types.ToolCall, len(delta.ToolCalls))
					for i, tc := range delta.ToolCalls {
						toolCalls[i] = types.ToolCall{
							ID:   tc.ID,
							Type: string(tc.Type),
							Function: types.FunctionCall{
								Name:      tc.Function.Name,
								Arguments: tc.Function.Arguments,
							},
						}
					}
					responseChan <- types.Response{
						ToolCalls: toolCalls,
					}
					continue
				}

				// Handle the text content
				if delta.Content != "" {
					// Add the content to the buffer
					buffer += delta.Content

					// Handle the tags in the buffer
					buffer, isActive = p.handleThinkTagsWithBuffer(buffer, isActive)

					// If currently active and the buffer has content, output it
					if isActive && buffer != "" {
						responseChan <- types.Response{
							Content: buffer,
						}
						buffer = ""
					}
				}
			}
		}
	}()

	return responseChan, nil
}

// addNoThinkDirective adds the /no_think directive to the user's last message for qwen3 models
func (p *Provider) addNoThinkDirective(messages []types.Message) []types.Message {
	// Copy the message list
	messagesCopy := make([]types.Message, len(messages))
	copy(messagesCopy, messages)

	// Find the last user message
	for i := len(messagesCopy) - 1; i >= 0; i-- {
		if messagesCopy[i].Role == "user" {
			// Prepend the /no_think directive to the user message
			messagesCopy[i].Content = "/no_think " + messagesCopy[i].Content
			break
		}
	}

	return messagesCopy
}

// handleThinkTagsWithBuffer handles think tags and returns the processed buffer and active state
func (p *Provider) handleThinkTagsWithBuffer(buffer string, isActive bool) (string, bool) {
	if buffer == "" {
		return buffer, isActive
	}

	// Handle complete <think></think> tags
	for strings.Contains(buffer, "<think>") && strings.Contains(buffer, "</think>") {
		parts := strings.SplitN(buffer, "<think>", 2)
		pre := parts[0]
		parts = strings.SplitN(parts[1], "</think>", 2)
		post := parts[1]
		buffer = pre + post
	}

	// Handle the case where there is only an opening tag
	if strings.Contains(buffer, "<think>") {
		parts := strings.SplitN(buffer, "<think>", 2)
		buffer = parts[0]
		isActive = false
	}

	// Handle the case where there is only a closing tag
	if strings.Contains(buffer, "</think>") {
		parts := strings.SplitN(buffer, "</think>", 2)
		buffer = parts[1]
		isActive = true
	}

	return buffer, isActive
}

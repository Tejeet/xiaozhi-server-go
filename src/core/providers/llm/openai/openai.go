package openai

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"xiaozhi-server-go/src/core/providers/llm"
	"xiaozhi-server-go/src/core/types"

	"github.com/sashabaranov/go-openai"
)

// Provider is the OpenAI LLM provider
type Provider struct {
	*llm.BaseProvider
	client    *openai.Client
	maxTokens int
}

// Register the provider
func init() {
	llm.Register("openai", NewProvider)
}

// NewProvider creates an OpenAI provider
func NewProvider(config *llm.Config) (llm.Provider, error) {
	base := llm.NewBaseProvider(config)
	provider := &Provider{
		BaseProvider: base,
		maxTokens:    config.MaxTokens,
	}
	if provider.maxTokens <= 0 {
		provider.maxTokens = 500
	}

	return provider, nil
}

// Initialize initializes the provider
func (p *Provider) Initialize() error {
	config := p.Config()
	if config.APIKey == "" {
		return fmt.Errorf("missing OpenAI API key")
	}

	clientConfig := openai.DefaultConfig(config.APIKey)
	if config.BaseURL != "" {
		clientConfig.BaseURL = config.BaseURL
	}

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
				Model:     p.Config().ModelName,
				Messages:  chatMessages,
				Stream:    true,
				MaxTokens: p.maxTokens,
			},
		)
		if err != nil {
			responseChan <- p.formatProviderError("create stream", err)
			return
		}
		defer stream.Close()

		isActive := true
		for {
			response, err := stream.Recv()
			if err != nil {
				if !errors.Is(err, io.EOF) {
					responseChan <- p.formatProviderError("receive stream", err)
				}
				break
			}

			if len(response.Choices) == 0 {
				continue
			}

			content := response.Choices[0].Delta.Content
			if content == "" {
				continue
			}

			if content, isActive = handleThinkTags(content, isActive); content != "" {
				responseChan <- content
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
				Model:    p.Config().ModelName,
				Messages: chatMessages,
				Tools:    tools,
				Stream:   true,
			},
		)
		if err != nil {
			formattedErr := p.formatProviderError("create stream", err)
			responseChan <- types.Response{
				Content: formattedErr,
				Error:   formattedErr,
			}
			return
		}
		defer stream.Close()

		for {
			response, err := stream.Recv()
			if err != nil {
				if !errors.Is(err, io.EOF) {
					formattedErr := p.formatProviderError("receive stream", err)
					responseChan <- types.Response{
						Content: formattedErr,
						Error:   formattedErr,
					}
				}
				break
			}

			if len(response.Choices) == 0 {
				continue
			}

			delta := response.Choices[0].Delta
			chunk := types.Response{
				Content: delta.Content,
			}

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
				chunk.ToolCalls = toolCalls
			}

			responseChan <- chunk
		}
	}()

	return responseChan, nil
}

func (p *Provider) formatProviderError(stage string, err error) string {
	message := fmt.Sprintf("OpenAI-compatible LLM %s error: %v", stage, err)

	config := p.Config()
	baseURL := ""
	if config != nil {
		baseURL = config.BaseURL
	}

	if strings.Contains(baseURL, "xf-yun.com") || strings.Contains(message, "xf-yun.com") {
		message += "; xfyun hint: confirm the selected model version and APIPassword belong to the same Spark HTTP service authorization"
		if strings.Contains(message, "AppIdNoAuthError") || strings.Contains(message, "11200") {
			message += "; AppIdNoAuthError/11200 usually means this app has not enabled the model, or the APIPassword does not match the model version"
		}
		if strings.Contains(message, "HMAC secret key does not match") {
			message += "; HMAC secret key does not match usually means an ASR/TTS APIKey/APISecret was used instead of the Spark HTTP APIPassword"
		}
	}

	return message
}

func handleThinkTags(content string, isActive bool) (string, bool) {
	if content == "" {
		return "", isActive
	}

	if content == "<think>" {
		return "", false
	}
	if content == "</think>" {
		return "", true
	}

	if !isActive {
		return "", isActive
	}

	return content, isActive
}

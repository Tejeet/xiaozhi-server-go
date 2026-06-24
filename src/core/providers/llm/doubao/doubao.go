package doubao

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"xiaozhi-server-go/src/core/providers/llm"
	"xiaozhi-server-go/src/core/types"

	"github.com/sashabaranov/go-openai"
)

// Provider is the Doubao LLM provider
type Provider struct {
	*llm.BaseProvider
	client       *http.Client
	apiKey       string
	baseURL      string
	maxTokens    int
	thinkingType string // thinking type: "disabled" (thinking off) or "enabled" (thinking on)
}

// doubaoRequest is a custom request struct that supports the thinking parameter
type doubaoRequest struct {
	Model       string                   `json:"model"`
	Messages    []map[string]interface{} `json:"messages"`
	Stream      bool                     `json:"stream"`
	MaxTokens   int                      `json:"max_tokens,omitempty"`
	Temperature float64                  `json:"temperature,omitempty"`
	TopP        float64                  `json:"top_p,omitempty"`
	Tools       []openai.Tool            `json:"tools,omitempty"`
	Thinking    map[string]string        `json:"thinking,omitempty"` // Supports the thinking parameter
}

// doubaoStreamResponse is the SSE response struct
type doubaoStreamResponse struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	Created int64  `json:"created"`
	Model   string `json:"model"`
	Choices []struct {
		Index int `json:"index"`
		Delta struct {
			Role      string            `json:"role,omitempty"`
			Content   string            `json:"content,omitempty"`
			ToolCalls []openai.ToolCall `json:"tool_calls,omitempty"`
		} `json:"delta"`
		FinishReason string `json:"finish_reason,omitempty"`
	} `json:"choices"`
}

// Register the provider
func init() {
	llm.Register("doubao", NewProvider)
}

// NewProvider creates a Doubao provider
func NewProvider(config *llm.Config) (llm.Provider, error) {
	base := llm.NewBaseProvider(config)
	provider := &Provider{
		BaseProvider: base,
		maxTokens:    config.MaxTokens,
		thinkingType: "disabled", // Thinking off by default
	}
	if provider.maxTokens <= 0 {
		provider.maxTokens = 500
	}

	// Read the thinking config from the Extra field
	if config.Extra != nil {
		if thinking, ok := config.Extra["thinking"].(string); ok {
			provider.thinkingType = thinking
		}
	}

	return provider, nil
}

// Initialize initializes the provider
func (p *Provider) Initialize() error {
	config := p.Config()
	if config.APIKey == "" {
		return fmt.Errorf("missing Doubao API key")
	}

	p.apiKey = config.APIKey
	p.client = &http.Client{}

	// Doubao uses the Volcano Engine API address
	if config.BaseURL != "" {
		p.baseURL = config.BaseURL
	} else {
		p.baseURL = "https://ark.cn-beijing.volces.com/api/v3"
	}

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
		reqMessages := make([]map[string]interface{}, len(messages))
		for i, msg := range messages {
			reqMessages[i] = map[string]interface{}{
				"role":    msg.Role,
				"content": msg.Content,
			}
		}

		// Build the custom request
		reqBody := doubaoRequest{
			Model:     p.Config().ModelName,
			Messages:  reqMessages,
			Stream:    true,
			MaxTokens: p.maxTokens,
		}

		// Add the thinking parameter
		if p.thinkingType != "" {
			reqBody.Thinking = map[string]string{
				"type": p.thinkingType,
			}
		}

		// Serialize the request
		jsonData, err := json.Marshal(reqBody)
		if err != nil {
			responseChan <- fmt.Sprintf("[request serialization failed: %v]", err)
			return
		}

		// Debug: print the request body (optional, for debugging)
		// fmt.Printf("Doubao request body: %s\n", string(jsonData))

		// Create the HTTP request
		url := fmt.Sprintf("%s/chat/completions", p.baseURL)
		req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(jsonData))
		if err != nil {
			responseChan <- fmt.Sprintf("[failed to create request: %v]", err)
			return
		}

		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", p.apiKey))

		// Send the request
		resp, err := p.client.Do(req)
		if err != nil {
			responseChan <- fmt.Sprintf("[Doubao service response error: %v]", err)
			return
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			responseChan <- fmt.Sprintf("[Doubao service error %d: %s]", resp.StatusCode, string(body))
			return
		}

		// Read the SSE stream
		reader := bufio.NewReader(resp.Body)
		for {
			line, err := reader.ReadBytes('\n')
			if err != nil {
				if err != io.EOF {
					responseChan <- fmt.Sprintf("[failed to read response: %v]", err)
				}
				break
			}

			line = bytes.TrimSpace(line)
			if len(line) == 0 {
				continue
			}

			// SSE format: "data: {...}"
			if bytes.HasPrefix(line, []byte("data: ")) {
				data := bytes.TrimPrefix(line, []byte("data: "))

				// Check whether it is the end marker
				if string(data) == "[DONE]" {
					break
				}

				// Parse the JSON
				var streamResp doubaoStreamResponse
				if err := json.Unmarshal(data, &streamResp); err != nil {
					continue
				}

				// Extract the content
				if len(streamResp.Choices) > 0 {
					content := streamResp.Choices[0].Delta.Content
					if content != "" {
						responseChan <- content
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

		// Convert the message format
		reqMessages := make([]map[string]interface{}, len(messages))
		for i, msg := range messages {
			msgMap := map[string]interface{}{
				"role":    msg.Role,
				"content": msg.Content,
			}

			// Handle the tool_call_id field (required for tool messages)
			if msg.ToolCallID != "" {
				msgMap["tool_call_id"] = msg.ToolCallID
			}

			// Handle the tool_calls field (tool calls in assistant messages)
			if len(msg.ToolCalls) > 0 {
				toolCalls := make([]map[string]interface{}, len(msg.ToolCalls))
				for j, tc := range msg.ToolCalls {
					toolCalls[j] = map[string]interface{}{
						"id":   tc.ID,
						"type": tc.Type,
						"function": map[string]interface{}{
							"name":      tc.Function.Name,
							"arguments": tc.Function.Arguments,
						},
					}
				}
				msgMap["tool_calls"] = toolCalls
			}

			reqMessages[i] = msgMap
		}

		// Build the custom request
		reqBody := doubaoRequest{
			Model:    p.Config().ModelName,
			Messages: reqMessages,
			Tools:    tools,
			Stream:   true,
		}

		// Add the thinking parameter
		if p.thinkingType != "" {
			reqBody.Thinking = map[string]string{
				"type": p.thinkingType,
			}
		}

		// Serialize the request
		jsonData, err := json.Marshal(reqBody)
		if err != nil {
			responseChan <- types.Response{
				Content: fmt.Sprintf("[request serialization failed: %v]", err),
				Error:   err.Error(),
			}
			return
		}

		// Debug: print the request body (optional, for debugging)
		// fmt.Printf("Doubao request body (with tools): %s\n", string(jsonData))

		// Create the HTTP request
		url := fmt.Sprintf("%s/chat/completions", p.baseURL)
		req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(jsonData))
		if err != nil {
			responseChan <- types.Response{
				Content: fmt.Sprintf("[failed to create request: %v]", err),
				Error:   err.Error(),
			}
			return
		}

		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", p.apiKey))

		// Send the request
		resp, err := p.client.Do(req)
		if err != nil {
			responseChan <- types.Response{
				Content: fmt.Sprintf("[Doubao service response error: %v]", err),
				Error:   err.Error(),
			}
			return
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			responseChan <- types.Response{
				Content: fmt.Sprintf("[Doubao service error %d: %s]", resp.StatusCode, string(body)),
				Error:   fmt.Sprintf("HTTP %d", resp.StatusCode),
			}
			return
		}

		// Read the SSE stream
		reader := bufio.NewReader(resp.Body)
		for {
			line, err := reader.ReadBytes('\n')
			if err != nil {
				if err != io.EOF {
					responseChan <- types.Response{
						Content: fmt.Sprintf("[failed to read response: %v]", err),
						Error:   err.Error(),
					}
				}
				break
			}

			line = bytes.TrimSpace(line)
			if len(line) == 0 {
				continue
			}

			// SSE format: "data: {...}"
			if bytes.HasPrefix(line, []byte("data: ")) {
				data := bytes.TrimPrefix(line, []byte("data: "))

				// Check whether it is the end marker
				if string(data) == "[DONE]" {
					break
				}

				// Parse the JSON
				var streamResp doubaoStreamResponse
				if err := json.Unmarshal(data, &streamResp); err != nil {
					continue
				}

				// Extract the content and tool calls
				if len(streamResp.Choices) > 0 {
					delta := streamResp.Choices[0].Delta

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
						// Output the raw content for now, without filtering
						responseChan <- types.Response{
							Content: delta.Content,
						}
					}
				}
			}
		}
	}()

	return responseChan, nil
}

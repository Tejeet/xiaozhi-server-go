package coze

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/coze-dev/coze-go"
	"github.com/sashabaranov/go-openai"
	"io"
	"sync"
	"xiaozhi-server-go/src/core/providers/llm"
	"xiaozhi-server-go/src/core/types"
)

type Provider struct {
	*llm.BaseProvider

	botID                  string
	userID                 string
	accessToken            string
	clientId               string
	publicKey              string
	privateKey             string
	client                 coze.CozeAPI
	sessionConversationMap sync.Map
}

func init() {
	llm.Register("coze", NewProvider)
}

// NewProvider creates a Coze provider
func NewProvider(config *llm.Config) (llm.Provider, error) {
	base := llm.NewBaseProvider(config)

	provider := &Provider{
		BaseProvider: base,
	}
	botId, ok := config.Extra["bot_id"]
	if ok {
		provider.botID = botId.(string)
	}
	userID, ok := config.Extra["user_id"]
	if ok {
		provider.userID = userID.(string)
	}
	clientId, ok := config.Extra["client_id"]
	if ok {
		provider.clientId = clientId.(string)
	}
	publicKey, ok := config.Extra["public_key"]
	if ok {
		provider.publicKey = publicKey.(string)
	}
	privateKey, ok := config.Extra["private_key"]
	if ok {
		provider.privateKey = privateKey.(string)
	}
	accessToken, ok := config.Extra["personal_access_token"]
	if ok {
		provider.accessToken = accessToken.(string)
	}
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
		return fmt.Errorf("missing Coze base URL configuration")
	}

	var authCli coze.Auth
	if p.clientId != "" && p.publicKey != "" && p.privateKey != "" {
		// Production environment
		client, err := coze.NewJWTOAuthClient(coze.NewJWTOAuthClientParam{
			ClientID:      p.clientId,
			PublicKey:     p.publicKey,
			PrivateKeyPEM: p.privateKey,
		}, coze.WithAuthBaseURL(baseURL))
		if err != nil {
			return fmt.Errorf("Coze failed to create JWT authorization token: %v", err)
		}

		authCli = coze.NewJWTAuth(client, nil)
	} else {
		// Personal testing
		authCli = coze.NewTokenAuth(p.accessToken)
	}
	p.client = coze.NewCozeAPI(authCli, coze.WithBaseURL(baseURL))
	return nil
}

// Response implements the types.LLMProvider interface
func (p *Provider) Response(ctx context.Context, sessionID string, messages []types.Message) (<-chan string, error) {
	responseChan := make(chan string, 10)

	go func() {
		defer close(responseChan)

		var lastMsg string
		if len(messages) > 0 {
			lastMsg = messages[len(messages)-1].Content
		}

		conversationId, ok := p.sessionConversationMap.Load(sessionID)
		if !ok {
			conversation, err := p.client.Conversations.Create(ctx, &coze.CreateConversationsReq{
				Messages: []*coze.Message{},
			})
			if err != nil {
				responseChan <- fmt.Sprintf("[Coze service failed to create conversation: %v]", err)
				return
			}
			conversationId = conversation.ID
			p.sessionConversationMap.Store(sessionID, conversationId)
		}

		stream, err := p.client.Chat.Stream(ctx, &coze.CreateChatsReq{
			BotID:  p.botID,
			UserID: p.userID,
			Messages: []*coze.Message{
				coze.BuildUserQuestionObjects([]*coze.MessageObjectString{
					coze.NewTextMessageObject(lastMsg),
				}, nil),
			},
			ConversationID: conversationId.(string),
		})
		if err != nil {
			responseChan <- fmt.Sprintf("[Coze service response error: %v]", err)
			return
		}
		defer stream.Close()

		for {
			event, err := stream.Recv()
			if err != nil {
				if errors.Is(err, io.EOF) {
					fmt.Println("Coze Stream finished")
				}
				break
			}

			if event.Event == coze.ChatEventConversationMessageDelta {
				responseChan <- event.Message.Content
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

		// On the first LLM call, take the last user message and append the tool prompt
		if len(messages) == 2 && len(tools) > 0 {
			lastMsg := messages[len(messages)-1].Content

			functionBytes, err := json.Marshal(tools)
			if err != nil {
				responseChan <- types.Response{
					Content: fmt.Sprintf("[failed to serialize tools: %v]", err),
					Error:   err.Error(),
				}
				return
			}
			functionStr := string(functionBytes)
			modifyMsg := llm.GetSystemPromptForFunction(functionStr) + lastMsg
			messages[len(messages)-1].Content = modifyMsg
		}
		// If the last message is role="tool", append it to the user message
		if len(messages) > 1 && messages[len(messages)-1].Role == "tool" {
			assistantMsg := "\ntool call result: " + messages[len(messages)-1].Content + "\n\n"

			for len(messages) > 1 {
				if messages[len(messages)-1].Role == "user" {
					messages[len(messages)-1].Content = assistantMsg + messages[len(messages)-1].Content
					break
				}
				messages = messages[:len(messages)-1]
			}
		}

		// Call the regular Response interface to get the result stream
		respChan, err := p.Response(ctx, sessionID, messages)
		if err != nil {
			responseChan <- types.Response{
				Content: fmt.Sprintf("[failed to call Response: %v]", err),
				Error:   err.Error(),
			}
			return
		}

		// Pass through the result
		for token := range respChan {
			responseChan <- types.Response{
				Content: token,
			}
		}
	}()

	return responseChan, nil
}

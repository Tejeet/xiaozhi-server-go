package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"xiaozhi-server-go/src/configs"
	"xiaozhi-server-go/src/configs/database"
	"xiaozhi-server-go/src/core/auth"
	"xiaozhi-server-go/src/core/chat"
	"xiaozhi-server-go/src/core/function"
	"xiaozhi-server-go/src/core/image"
	"xiaozhi-server-go/src/core/mcp"
	"xiaozhi-server-go/src/core/pool"
	"xiaozhi-server-go/src/core/providers"
	"xiaozhi-server-go/src/core/providers/llm"
	"xiaozhi-server-go/src/core/providers/tts"
	"xiaozhi-server-go/src/core/providers/vlllm"
	"xiaozhi-server-go/src/core/types"
	"xiaozhi-server-go/src/core/utils"
	"xiaozhi-server-go/src/models"
	"xiaozhi-server-go/src/task"

	"github.com/google/uuid"
	"github.com/sashabaranov/go-openai"
	"gorm.io/gorm"
)

type MCPResultHandler func(args interface{}) string

// Connection is the unified connection interface
type Connection interface {
	// Send a message
	WriteMessage(messageType int, data []byte) error
	// Read a message
	ReadMessage(stopChan <-chan struct{}) (messageType int, data []byte, err error)
	// Close the connection
	Close() error
	// Get the connection ID
	GetID() string
	// Get the connection type
	GetType() string
	// Check the connection state
	IsClosed() bool
	// Get the last active time
	GetLastActiveTime() time.Time
	// Check whether it is stale
	IsStale(timeout time.Duration) bool
}

type ttsConfigGetter interface {
	Config() *tts.Config
}

type llmConfigGetter interface {
	Config() *llm.Config
}

// ConnectionHandler is the connection-handler struct
type ConnectionHandler struct {
	// Ensure the AsrEventListener interface is implemented
	_                providers.AsrEventListener
	config           *configs.Config
	logger           *utils.Logger
	conn             Connection
	closeOnce        sync.Once
	taskMgr          *task.TaskManager
	authManager      *auth.AuthManager // Authentication manager
	safeCallbackFunc func(func(*ConnectionHandler)) func()
	providers        struct {
		asr   providers.ASRProvider
		llm   providers.LLMProvider
		tts   providers.TTSProvider
		vlllm *vlllm.Provider // VLLLM provider, optional
	}

	initialVoice    string // Initial voice name
	ttsProviderName string // Default TTS provider name
	voiceName       string // Voice name

	// Session related
	sessionID     string            // Session ID between the device and the server
	deviceID      string            // Device ID
	clientId      string            // Client ID
	headers       map[string]string // HTTP header info
	transportType string            // Transport type

	// Client audio related
	clientAudioFormat        string
	clientAudioSampleRate    int
	clientAudioChannels      int
	clientAudioFrameDuration int

	serverAudioFormat        string // Server-side audio format
	serverAudioSampleRate    int
	serverAudioChannels      int
	serverAudioFrameDuration int

	clientListenMode string
	isDeviceVerified bool
	closeAfterChat   bool

	// Agent related
	agentID      uint          // The AgentID bound to the device
	enabledTools []string      // List of enabled tools
	tools        []openai.Tool // Cached tool list
	// Voice processing related
	serverVoiceStop int32 // 1 means true: server-side voice stopped, no more voice data is sent

	opusDecoder *utils.OpusDecoder // Opus decoder

	// Conversation related
	dialogueManager     *chat.DialogueManager
	tts_last_text_index int
	client_asr_text     string // Client ASR text
	quickReplyCache     *utils.QuickReplyCache

	// Concurrency control
	stopChan         chan struct{}
	clientAudioQueue chan []byte
	clientTextQueue  chan string

	// TTS task queue
	ttsQueue chan struct {
		text      string
		round     int // Round
		textIndex int
	}

	audioMessagesQueue chan struct {
		filepath  string
		text      string
		round     int // Round
		textIndex int
	}

	talkRound      int       // Round counter
	roundStartTime time.Time // Round start time
	// functions
	functionRegister *function.FunctionRegistry
	mcpManager       *mcp.Manager

	mcpResultHandlers map[string]MCPResultHandler // MCP handler mapping
	ctx               context.Context
}

// NewConnectionHandler creates a new connection handler
func NewConnectionHandler(
	config *configs.Config,
	providerSet *pool.ProviderSet,
	logger *utils.Logger,
	req *http.Request,
	ctx context.Context,
) *ConnectionHandler {
	handler := &ConnectionHandler{
		config:           config,
		logger:           logger,
		clientListenMode: "auto",
		stopChan:         make(chan struct{}),
		clientAudioQueue: make(chan []byte, 100),
		clientTextQueue:  make(chan string, 100),
		ttsQueue: make(chan struct {
			text      string
			round     int // Round
			textIndex int
		}, 100),
		audioMessagesQueue: make(chan struct {
			filepath  string
			text      string
			round     int // Round
			textIndex int
		}, 100),

		tts_last_text_index: -1,

		talkRound: 0,

		serverAudioFormat:        "opus", // Use Opus format by default
		serverAudioSampleRate:    24000,
		serverAudioChannels:      1,
		serverAudioFrameDuration: 60,

		ctx: ctx,

		headers: make(map[string]string),
	}

	for key, values := range req.Header {
		if len(values) > 0 {
			handler.headers[key] = values[0] // Take the first value
		}
		if key == "Device-Id" {
			handler.deviceID = values[0] // Device ID
		}
		if key == "Client-Id" {
			handler.clientId = values[0] // Client ID
		}
		if key == "Session-Id" {
			handler.sessionID = values[0] // Session ID
		}
		if key == "Transport-Type" {
			handler.transportType = values[0] // Transport type
		}
		logger.Debug("[HTTP] [header %s] %s", key, values[0])
	}

	if handler.sessionID == "" {
		if handler.deviceID == "" {
			handler.sessionID = uuid.New().String() // If there is no device ID, generate a new session ID
		} else {
			handler.sessionID = "device-" + strings.Replace(handler.deviceID, ":", "_", -1)
		}
	}

	// Set up providers correctly
	if providerSet != nil {
		handler.providers.asr = providerSet.ASR
		handler.providers.llm = providerSet.LLM
		handler.providers.tts = providerSet.TTS
		handler.providers.vlllm = providerSet.VLLLM
		handler.mcpManager = providerSet.MCP
	}
	handler.checkDeviceInfo()
	agent, prompt := handler.InitWithAgent()
	handler.checkTTSProvider(agent, config) // Check the TTS provider
	handler.checkLLMProvider(agent, config) // Check whether the LLM provider matches

	handler.quickReplyCache = utils.NewQuickReplyCache(handler.ttsProviderName, handler.voiceName)

	// Initialize the dialogue manager
	handler.dialogueManager = chat.NewDialogueManager(handler.logger, nil)
	handler.dialogueManager.SetSystemMessage(prompt)
	handler.functionRegister = function.NewFunctionRegistry()
	handler.initMCPResultHandlers()

	return handler
}

func (h *ConnectionHandler) InitWithAgent() (*models.Agent, string) {
	// Get the Agent by agentID
	var agent *models.Agent = nil
	var err error
	prompt := h.config.DefaultPrompt
	if h.agentID != 0 {
		// No transaction is needed here
		agent, err = database.GetAgentByID(database.GetDB(), h.agentID)
		if err != nil {
			h.LogError(fmt.Sprintf("Failed to get Agent: %v", err))
		}
		agentName := agent.Name
		prompt = agent.Prompt // Use the Agent's Prompt
		if agentName != "" {
			if strings.Contains(prompt, "{{assistant_name}}") {
				prompt = strings.Replace(prompt, "{{assistant_name}}", agentName, -1)
			} else {
				prompt += "\n\nAssistant name: " + agentName
			}
		}

		// "普通话" (Mandarin) and "中文" (Chinese) are language values stored in the DB; keep them to match agent data
		if agent.Language != "" && agent.Language != "普通话" && agent.Language != "中文" {
			prompt += "\n\nAnswer the user's questions in " + agent.Language + "."
		}

		if agent.EnabledTools != "" {
			h.enabledTools = strings.Split(agent.EnabledTools, ",")
		} else {
			h.enabledTools = []string{} // If none, do not filter
		}

		h.LogInfo(fmt.Sprintf("Allowed tools: %v", h.enabledTools))
		h.LogInfo(fmt.Sprintf("Using the Prompt of Agent %d: %s", h.agentID, prompt))

	}
	return agent, prompt
}

func (h *ConnectionHandler) checkTTSProvider(agent *models.Agent, config *configs.Config) {
	h.ttsProviderName = "default" // Default TTS provider name
	h.voiceName = "default"
	if getter, ok := h.providers.tts.(ttsConfigGetter); ok {

		userID := database.AdminUserID
		alltts, err := database.GetProviderByTypeInternal("TTS", userID, false)
		if err == nil {
			for name, data := range alltts {

				cfg := configs.TTSConfig{}
				if err := json.Unmarshal([]byte(data), &cfg); err != nil {
					h.LogError(fmt.Sprintf("Failed to deserialize TTS provider %s config for user %d: %v", name, userID, err))
					continue
				}
				// h.LogInfo(fmt.Sprintf("TTS provider for user %d: %s, config: %v", userID, name, cfg))
				config.TTS[name] = cfg // Update the config
			}
		} else {
			h.LogError(fmt.Sprintf("Failed to get TTS providers for user %d: %v", userID, err))
		}

		h.ttsProviderName = getter.Config().Type
		// Get it from the agent config
		h.voiceName = getter.Config().Voice
		if agent != nil && agent.Voice != "" {
			err, newVoice := h.providers.tts.SetVoice(agent.Voice) // Set the TTS voice
			if err != nil {
				// Check whether the voice is supported by another TTS provider
				bChangeTTSSucc := false
				for name, cfg := range config.TTS {
					if bSupport, newVoice2, _ := tts.IsSupportedVoice(agent.Voice, cfg.SupportedVoices); bSupport {
						ttsCfg := &tts.Config{
							Name:            name,
							Type:            cfg.Type,
							OutputDir:       cfg.OutputDir,
							Voice:           newVoice2,
							Format:          cfg.Format,
							SampleRate:      h.serverAudioSampleRate,
							AppID:           cfg.AppID,
							Token:           cfg.Token,
							Cluster:         cfg.Cluster,
							SupportedVoices: cfg.SupportedVoices,
						}
						newVoice = newVoice2
						newtts, err := tts.Create(cfg.Type, ttsCfg, false)
						if err == nil {
							h.providers.tts = newtts
							bChangeTTSSucc = true
							h.ttsProviderName = cfg.Type
							h.LogInfo(fmt.Sprintf("Switched TTS provider to: %s, voice name: %s, v:%s", name, agent.Voice, newVoice))
							break
						} else {
							h.LogError(fmt.Sprintf("Failed to create TTS provider: %v", err))
						}
					} else {
						h.LogInfo(fmt.Sprintf("Voice %s of Agent %d is not supported by TTS provider %s", agent.Voice, agent.ID, name))
					}
				}
				if !bChangeTTSSucc {
					h.LogError(fmt.Sprintf("Failed to set the TTS voice to the agent config: %v", err))
				} else {
					h.voiceName = newVoice
				}
			} else {
				h.voiceName = newVoice
			}
		}
		h.initialVoice = h.voiceName // Save the initial voice name
	}
	h.logger.Info("Using TTS provider: %s, voice name: %s", h.ttsProviderName, h.voiceName)

}

func (h *ConnectionHandler) checkLLMProvider(agent *models.Agent, config *configs.Config) {
	if agent == nil {
		return
	}
	agentLLMName := agent.LLM
	// Get extra from the agent
	apiKey := ""
	baseUrl := ""
	if agent.Extra != "" {
		// Parse the Extra field
		var extra map[string]interface{}
		if err := json.Unmarshal([]byte(agent.Extra), &extra); err == nil {
			if key, ok := extra["api_key"].(string); ok {
				apiKey = key
			}
			if url, ok := extra["base_url"].(string); ok {
				baseUrl = url
			}
		} else {
			h.LogError(fmt.Sprintf("The Extra field of Agent %d has an invalid format: %v, err:%v", agent.ID, agent.Extra, err))
		}
	}
	// Check whether the type of handler.providers.llm matches agent.LLM
	if getter, ok := h.providers.llm.(llmConfigGetter); ok {
		// Load the user's private LLM config from the database
		userID := database.AdminUserID
		llms, err := database.GetProviderByTypeInternal("LLM", userID, false)
		if err == nil {
			for name, data := range llms {

				cfg := configs.LLMConfig{}
				if err := json.Unmarshal([]byte(data), &cfg); err != nil {
					h.LogError(fmt.Sprintf("Failed to deserialize LLM provider %s config for user %d: %v", name, userID, err))
					continue
				}
				//h.LogInfo(fmt.Sprintf("LLM provider for user %d: %s, config: %v", userID, name, cfg))
				config.LLM[name] = cfg // Update the config
			}
		} else {
			h.LogError(fmt.Sprintf("Failed to get LLM providers for user %d: %v", userID, err))
		}

		llmName := getter.Config().Name
		if llmName != agentLLMName {
			// Set the LLM provider based on the agent.LLM type
			if cfg, ok := config.LLM[agentLLMName]; !ok {
				h.LogError(fmt.Sprintf("The LLM type %s of Agent %d does not exist", agentLLMName, h.agentID))
			} else {
				if apiKey != "" {
					cfg.APIKey = apiKey // Use the Agent's API key
				}
				if baseUrl != "" {
					cfg.BaseURL = baseUrl // Use the Agent's BaseURL
				}
				llmCfg := &llm.Config{
					Name:        agentLLMName,
					Type:        cfg.Type,
					ModelName:   cfg.ModelName,
					BaseURL:     cfg.BaseURL,
					APIKey:      cfg.APIKey,
					Temperature: cfg.Temperature,
					MaxTokens:   cfg.MaxTokens,
					TopP:        cfg.TopP,
					Extra:       cfg.Extra,
				}
				newllm, err := llm.Create(cfg.Type, llmCfg)
				if err != nil {
					h.LogError(fmt.Sprintf("Failed to create LLM provider: %v", err))
				} else {
					h.providers.llm = newllm
					h.LogInfo(fmt.Sprintf("Switched the LLM provider of Agent %d to: %s", h.agentID, agentLLMName))
				}
			}
		} else {
			if apiKey != "" {
				getter.Config().APIKey = apiKey
			}
			if baseUrl != "" {
				getter.Config().BaseURL = baseUrl
			}
			h.LogInfo(fmt.Sprintf("Using the LLM type of Agent %d: %s, BaseURL:%s", h.agentID, llmName, getter.Config().BaseURL))
		}
	}
}

func (h *ConnectionHandler) checkDeviceInfo() {
	h.agentID = 0 // Clear the AgentID

	if h.deviceID == "" {
		h.LogError("Device ID is not set; cannot check the device binding state")
		return
	}
	device, err := database.FindDeviceByID(database.GetDB(), h.deviceID) // Make sure the device exists
	if err == gorm.ErrRecordNotFound {
		h.LogError(fmt.Sprintf("Failed to find the device: %v", err))
		return
	}

	if device.AgentID != nil {
		h.agentID = *device.AgentID // Get the AgentID bound to the device
	} else {
		// Query the current agent list and bind to the first agent
		agents, err := database.ListAgentsByUser(database.GetDB(), database.AdminUserID)
		if err != nil {
			h.LogError(fmt.Sprintf("Failed to query agents: %v", err))
			return
		}
		if len(agents) > 0 {
			h.agentID = agents[0].ID
			device.AgentID = &h.agentID
			err = database.UpdateDevice(database.GetDB(), device)
			if err != nil {
				h.LogError(fmt.Sprintf("Failed to update the agent bound to the device: %v", err))
				return
			}
		} else {
			h.agentID = 0 // 0 if not bound
		}
	}

	h.LogInfo(fmt.Sprintf("Device binding state: AgentID=%d", h.agentID))
}

func (h *ConnectionHandler) SetTaskCallback(callback func(func(*ConnectionHandler)) func()) {
	h.safeCallbackFunc = callback
}

func (h *ConnectionHandler) SubmitTask(taskType string, params map[string]interface{}) {
	_task, id := task.NewTask(h.ctx, "", params)
	h.LogInfo(fmt.Sprintf("Submitting task: %s, ID: %s, params: %v", _task.Type, id, params))
	// Create a safe callback to be invoked when the task completes
	var taskCallback func(result interface{})
	if h.safeCallbackFunc != nil {
		taskCallback = func(result interface{}) {
			fmt.Print("Task completion callback: ")
			safeCallback := h.safeCallbackFunc(func(handler *ConnectionHandler) {
				// Handle task-completion logic
				handler.handleTaskComplete(_task, id, result)
			})
			// Execute the safe callback
			if safeCallback != nil {
				safeCallback()
			}
		}
	}
	cb := task.NewCallBack(taskCallback)
	_task.Callback = cb
	h.taskMgr.SubmitTask(h.sessionID, _task)
}

func (h *ConnectionHandler) handleTaskComplete(task *task.Task, id string, result interface{}) {
	h.LogInfo(fmt.Sprintf("Task %s completed, ID: %s, %v", task.Type, id, result))
}

func (h *ConnectionHandler) LogInfo(msg string) {
	if h.logger != nil {
		h.logger.Info(msg, map[string]interface{}{
			"device": h.deviceID,
		})
	}
}
func (h *ConnectionHandler) LogDebug(msg string) {
	if h.logger != nil {
		h.logger.Debug(msg, map[string]interface{}{
			"device": h.deviceID,
		})
	}
}
func (h *ConnectionHandler) LogError(msg string) {
	if h.logger != nil {
		h.logger.Error(msg, map[string]interface{}{
			"device": h.deviceID,
		})
	}
}

// Handle handles a WebSocket connection
func (h *ConnectionHandler) Handle(conn Connection) {
	defer conn.Close()

	h.conn = conn

	// Start the message-processing goroutines
	go h.processClientAudioMessagesCoroutine() // Add the client audio message processing goroutine
	go h.processClientTextMessagesCoroutine()  // Add the client text message processing goroutine
	go h.processTTSQueueCoroutine()            // Add the TTS queue processing goroutine
	go h.sendAudioMessageCoroutine()           // Add the audio message sending goroutine

	// Optimized MCP manager handling
	if h.mcpManager == nil {
		h.LogError("No MCP manager available")
		return

	} else {
		h.LogInfo("[MCP] [manager] using the resource pool to quickly bind the connection")
		// The pooled manager is already pre-initialized; just bind the connection
		params := map[string]interface{}{
			"session_id": h.sessionID,
			"vision_url": h.config.Web.VisionURL,
			"device_id":  h.deviceID,
			"client_id":  h.clientId,
			"token":      h.config.Server.Token,
		}
		if err := h.mcpManager.BindConnection(conn, h.functionRegister, params); err != nil {
			h.LogError(fmt.Sprintf("Failed to bind the MCP manager connection: %v", err))
			return
		}
		// No need to re-initialize the server; just make sure the connection-related services are working
		h.LogInfo("[MCP] [bind] connection binding complete, skipping duplicate initialization")
	}

	// Main message loop
	for {
		select {
		case <-h.stopChan:
			return
		default:
			messageType, message, err := conn.ReadMessage(h.stopChan)
			if err != nil {
				h.LogError(fmt.Sprintf("Failed to read message: %v, exiting the main message loop", err))
				return
			}

			if err := h.handleMessage(messageType, message); err != nil {
				h.LogError(fmt.Sprintf("Failed to handle message: %v", err))
			}
		}
	}
}

// processClientTextMessagesCoroutine processes the text message queue
func (h *ConnectionHandler) processClientTextMessagesCoroutine() {
	for {
		select {
		case <-h.stopChan:
			return
		case text := <-h.clientTextQueue:
			if err := h.processClientTextMessage(context.Background(), text); err != nil {
				h.LogError(fmt.Sprintf("Failed to process text data: %v", err))
			}
		}
	}
}

// processClientAudioMessagesCoroutine processes the audio message queue
func (h *ConnectionHandler) processClientAudioMessagesCoroutine() {
	for {
		select {
		case <-h.stopChan:
			return
		case audioData := <-h.clientAudioQueue:
			if h.closeAfterChat {
				continue
			}
			if err := h.providers.asr.AddAudio(audioData); err != nil {
				h.LogError(fmt.Sprintf("Failed to process audio data: %v", err))
			}
		}
	}
}

func (h *ConnectionHandler) sendAudioMessageCoroutine() {
	for {
		select {
		case <-h.stopChan:
			return
		case task := <-h.audioMessagesQueue:
			h.sendAudioMessage(task.filepath, task.text, task.textIndex, task.round)
		}
	}
}

// OnAsrResult implements the AsrEventListener interface
// Returning true stops speech recognition; returning false continues it
func (h *ConnectionHandler) OnAsrResult(result string, isFinalResult bool) bool {
	//h.LogInfo(fmt.Sprintf("[%s] ASR result: %s", h.clientListenMode, result))
	if h.providers.asr.GetSilenceCount() >= 2 {
		h.LogInfo("[ASR] [silence detected] twice in a row, ending the conversation")
		h.closeAfterChat = true // If silence is detected twice in a row, end the conversation
		result = "[SILENCE_TIMEOUT] No user speech detected for a long time; please end the conversation politely"
	}
	if h.clientListenMode == "auto" {
		if result == "" {
			return false
		}
		h.LogInfo(fmt.Sprintf("[ASR] [result %s/%s]", h.clientListenMode, result))
		h.handleChatMessage(context.Background(), result)
		return true
	} else if h.clientListenMode == "manual" {
		h.client_asr_text += result
		if isFinalResult {
			h.handleChatMessage(context.Background(), h.client_asr_text)
			return true
		}
		return false
	} else if h.clientListenMode == "realtime" {
		if result == "" {
			return false
		}
		h.stopServerSpeak()
		h.providers.asr.Reset() // Reset the ASR state, ready for the next recognition
		h.LogInfo(fmt.Sprintf("[ASR] [result %s/%s]", h.clientListenMode, result))
		h.handleChatMessage(context.Background(), result)
		return true
	}
	return false
}

// clientAbortChat handles the abort message
func (h *ConnectionHandler) clientAbortChat() error {
	h.LogInfo("[Client] [abort message] received, stopping speech recognition")
	h.stopServerSpeak()
	h.sendTTSMessage("stop", "", 0)
	h.clearSpeakStatus()
	return nil
}

func (h *ConnectionHandler) QuitIntent(text string) bool {
	// CMD_exit reads the exit commands from the configuration
	exitCommands := h.config.CMDExit
	if exitCommands == nil {
		return false
	}
	cleand_text := utils.RemoveAllPunctuation(text) // Remove punctuation to ensure accurate matching
	// Check whether it contains an exit command
	for _, cmd := range exitCommands {
		h.logger.Debug(fmt.Sprintf("Checking exit command: %s,%s", cmd, cleand_text))
		// Check for equality
		if cleand_text == cmd {
			h.LogInfo("[Client] [exit intent] received, preparing to end the conversation")
			h.Close() // Close the connection directly
			return true
		}
	}
	return false
}

func (h *ConnectionHandler) quickReplyWakeUpWords(text string) bool {
	// Check whether it contains a wake word
	if !h.config.QuickReply || h.talkRound != 1 {
		return false
	}
	if !utils.IsWakeUpWord(text) {
		return false
	}

	repalyWords := h.config.QuickReplyWords
	reply_text := utils.RandomSelectFromArray(repalyWords)
	h.tts_last_text_index = 1 // Reset the text index
	h.SpeakAndPlay(reply_text, 1, h.talkRound)

	return true
}

// handleChatMessage handles a chat message
func (h *ConnectionHandler) handleChatMessage(ctx context.Context, text string) error {
	if text == "" {
		h.logger.Warn("Received an empty chat message, ignoring it")
		h.clientAbortChat()
		return fmt.Errorf("chat message is empty")
	}

	if h.QuitIntent(text) {
		return fmt.Errorf("user requested to exit the conversation")
	}

	// Increment the conversation round
	h.talkRound++
	h.roundStartTime = time.Now()
	currentRound := h.talkRound
	h.LogInfo(fmt.Sprintf("[Conversation] [round %d] starting a new conversation round", currentRound))

	// Regular text message processing flow
	// Send the stt message immediately
	err := h.sendSTTMessage(text)
	if err != nil {
		h.LogError(fmt.Sprintf("Failed to send STT message: %v", err))
		return fmt.Errorf("failed to send STT message: %v", err)
	}

	// Send the tts start state
	if err := h.sendTTSMessage("start", "", 0); err != nil {
		h.LogError(fmt.Sprintf("Failed to send TTS start state: %v", err))
		return fmt.Errorf("failed to send TTS start state: %v", err)
	}

	// Send the "thinking" emotion
	if err := h.sendEmotionMessage("thinking"); err != nil {
		h.LogError(fmt.Sprintf("Failed to send the thinking emotion message: %v", err))
		return fmt.Errorf("failed to send emotion message: %v", err)
	}

	h.LogInfo(fmt.Sprintf("[Chat] [message %s]", text))

	if h.quickReplyWakeUpWords(text) {
		return nil
	}

	// Add the user message to the conversation history
	h.dialogueManager.Put(chat.Message{
		Role:    "user",
		Content: text,
	})

	return h.genResponseByLLM(ctx, h.dialogueManager.GetLLMDialogue(), currentRound)
}

func (h *ConnectionHandler) genResponseByLLM(ctx context.Context, messages []providers.Message, round int) error {
	defer func() {
		if r := recover(); r != nil {
			h.LogError(fmt.Sprintf("genResponseByLLM panicked: %v", r))
			errorMsg := "Sorry, an error occurred while processing your request"
			h.tts_last_text_index = 1 // Reset the text index
			h.SpeakAndPlay(errorMsg, 1, round)
		}
	}()

	llmStartTime := time.Now()
	//h.logger.Info("Starting to generate the LLM reply, round:%d ", round)
	for _, msg := range messages {
		_ = msg
		//msg.Print()
	}
	// Use the LLM to generate a reply
	tools := h.functionRegister.GetAllFunctions()
	responses, err := h.providers.llm.ResponseWithFunctions(ctx, h.sessionID, messages, tools)
	if err != nil {
		return fmt.Errorf("LLM failed to generate a reply: %v", err)
	}

	// Process the reply
	var responseMessage []string
	processedChars := 0
	textIndex := 0

	atomic.StoreInt32(&h.serverVoiceStop, 0)

	// Process the streaming response
	toolCallFlag := false
	functionName := ""
	functionID := ""
	functionArguments := ""
	contentArguments := ""

	for response := range responses {
		content := response.Content
		toolCall := response.ToolCalls

		if response.Error != "" {
			h.LogError(fmt.Sprintf("LLM response error: %s", response.Error))
			errorMsg := "Sorry, the service is temporarily unavailable, please try again later"
			h.tts_last_text_index = 1 // Reset the text index
			h.SpeakAndPlay(errorMsg, 1, round)
			return fmt.Errorf("LLM response error: %s", response.Error)
		}

		if content != "" {
			// Accumulate content_arguments
			contentArguments += content
		}

		if !toolCallFlag && strings.HasPrefix(contentArguments, "<tool_call>") {
			toolCallFlag = true
		}

		if len(toolCall) > 0 {
			toolCallFlag = true
			if toolCall[0].ID != "" {
				functionID = toolCall[0].ID
			}
			if toolCall[0].Function.Name != "" {
				functionName = toolCall[0].Function.Name
			}
			if toolCall[0].Function.Arguments != "" {
				functionArguments += toolCall[0].Function.Arguments
			}
		}

		if content != "" {
			// "service response error" is the marker in the error string returned by LLM providers
			if strings.Contains(content, "service response error") {
				h.LogError(fmt.Sprintf("Detected an LLM service error: %s", content))
				errorMsg := "Sorry, the LLM service is temporarily unavailable, please try again later"
				h.tts_last_text_index = 1 // Reset the text index
				h.SpeakAndPlay(errorMsg, 1, round)
				return fmt.Errorf("LLM service error")
			}

			if toolCallFlag {
				continue
			}

			responseMessage = append(responseMessage, content)
			// Handle segmentation
			fullText := utils.JoinStrings(responseMessage)
			if len(fullText) <= processedChars {
				h.logger.Warn(fmt.Sprintf("Text processing anomaly: fullText length=%d, processedChars=%d", len(fullText), processedChars))
				continue
			}
			currentText := fullText[processedChars:]

			// Split by punctuation
			if segment, charsCnt := utils.SplitAtLastPunctuation(currentText); charsCnt > 0 {
				textIndex++
				segment = strings.TrimSpace(segment)
				if textIndex == 1 {
					now := time.Now()
					llmSpentTime := now.Sub(llmStartTime)
					h.LogInfo(fmt.Sprintf("[LLM] [reply %s/%d] first sentence: %s", llmSpentTime, round, segment))
				} else {
					h.LogInfo(fmt.Sprintf("[LLM] [segment %d/%d] %s", textIndex, round, segment))
				}
				h.tts_last_text_index = textIndex
				err := h.SpeakAndPlay(segment, textIndex, round)
				if err != nil {
					h.LogError(fmt.Sprintf("Failed to play the LLM reply segment: %v", err))
				}
				processedChars += charsCnt
			}
		}
	}

	if toolCallFlag {
		bHasError := false
		if functionID == "" {
			a := utils.Extract_json_from_string(contentArguments)
			if a != nil {
				functionName = a["name"].(string)
				argumentsJson, err := json.Marshal(a["arguments"])
				if err != nil {
					h.LogError(fmt.Sprintf("Failed to parse the function-call arguments: %v", err))
				}
				functionArguments = string(argumentsJson)
				functionID = uuid.New().String()
			} else {
				bHasError = true
			}
			if bHasError {
				h.LogError(fmt.Sprintf("Failed to parse the function-call arguments: %v", err))
			}
		}
		if !bHasError {
			// Clear responseMessage
			responseMessage = []string{}
			arguments := make(map[string]interface{})
			if err := json.Unmarshal([]byte(functionArguments), &arguments); err != nil {
				h.LogError(fmt.Sprintf("Failed to parse the function-call arguments: %v", err))
			}
			functionCallData := map[string]interface{}{
				"id":        functionID,
				"name":      functionName,
				"arguments": functionArguments,
			}
			h.LogInfo(fmt.Sprintf("Function call: %v", arguments))
			if h.mcpManager.IsMCPTool(functionName) {
				// Handle the MCP function call
				result, err := h.mcpManager.ExecuteTool(ctx, functionName, arguments)
				if err != nil {
					h.LogError(fmt.Sprintf("MCP function call failed: %v", err))
					if result == nil {
						result = "MCP tool call failed"
					}
				}
				// Check whether result is of type types.ActionResponse
				if actionResult, ok := result.(types.ActionResponse); ok {
					h.handleFunctionResult(actionResult, functionCallData, textIndex)
				} else {
					h.LogInfo(fmt.Sprintf("MCP function call result: %v", result))
					actionResult := types.ActionResponse{
						Action: types.ActionTypeReqLLM, // Action type
						Result: result,                 // Result produced by the action
					}
					h.handleFunctionResult(actionResult, functionCallData, textIndex)
				}

			} else {
				// Handle a regular function call
				//h.functionRegister.CallFunction(functionName, functionCallData)
			}
		}
	}

	// Handle the remaining text
	fullResponse := utils.JoinStrings(responseMessage)
	if len(fullResponse) > processedChars {
		remainingText := fullResponse[processedChars:]
		if remainingText != "" {
			textIndex++
			h.LogInfo(fmt.Sprintf("[LLM] [segment remaining text %d/%d] %s", textIndex, round, remainingText))
			h.tts_last_text_index = textIndex
			h.SpeakAndPlay(remainingText, textIndex, round)
		}
	} else {
		h.logger.Debug("No remaining text to process: fullResponse length=%d, processedChars=%d", len(fullResponse), processedChars)
	}

	// Analyze the reply and send the corresponding emotion
	content := utils.JoinStrings(responseMessage)

	// Add the assistant reply to the conversation history
	if !toolCallFlag {
		h.dialogueManager.Put(chat.Message{
			Role:    "assistant",
			Content: content,
		})
	}

	return nil
}

func (h *ConnectionHandler) addToolCallMessage(toolResultText string, functionCallData map[string]interface{}) {

	functionID := functionCallData["id"].(string)
	functionName := functionCallData["name"].(string)
	functionArguments := functionCallData["arguments"].(string)
	h.LogInfo(fmt.Sprintf("Function call result: %s", toolResultText))
	h.LogInfo(fmt.Sprintf("Function call arguments: %s", functionArguments))
	h.LogInfo(fmt.Sprintf("Function call name: %s", functionName))
	h.LogInfo(fmt.Sprintf("Function call ID: %s", functionID))

	// Add the assistant message, including tool_calls
	h.dialogueManager.Put(chat.Message{
		Role: "assistant",
		ToolCalls: []types.ToolCall{{
			ID: functionID,
			Function: types.FunctionCall{
				Arguments: functionArguments,
				Name:      functionName,
			},
			Type:  "function",
			Index: 0,
		}},
	})

	// Add the tool message
	toolCallID := functionID
	if toolCallID == "" {
		toolCallID = uuid.New().String()
	}
	h.dialogueManager.Put(chat.Message{
		Role:       "tool",
		ToolCallID: toolCallID,
		Content:    toolResultText,
	})
}

func (h *ConnectionHandler) handleFunctionResult(result types.ActionResponse, functionCallData map[string]interface{}, textIndex int) {
	switch result.Action {
	case types.ActionTypeError:
		h.LogError(fmt.Sprintf("Function call error: %v", result.Result))
	case types.ActionTypeNotFound:
		h.LogError(fmt.Sprintf("Function not found: %v", result.Result))
	case types.ActionTypeNone:
		h.LogInfo(fmt.Sprintf("Function call no-op: %v", result.Result))
	case types.ActionTypeResponse:
		h.LogInfo(fmt.Sprintf("Function call direct reply: %v", result.Response))
		h.SystemSpeak(result.Response.(string))
	case types.ActionTypeCallHandler:
		resultStr := h.handleMCPResultCall(result)
		h.addToolCallMessage(resultStr, functionCallData)
	case types.ActionTypeReqLLM:
		h.LogInfo(fmt.Sprintf("Requesting LLM after the function call: %v", result.Result))
		text, ok := result.Result.(string)
		if ok && len(text) > 0 {
			h.addToolCallMessage(text, functionCallData)
			h.genResponseByLLM(context.Background(), h.dialogueManager.GetLLMDialogue(), h.talkRound)

		} else {
			h.LogError(fmt.Sprintf("Failed to parse the function call result: %v", result.Result))
			// Send an error message
			errorMessage := fmt.Sprintf("Failed to parse the function call result %v", result.Result)
			h.SystemSpeak(errorMessage)
		}
	}
}

func (h *ConnectionHandler) SystemSpeak(text string) error {
	if text == "" {
		h.logger.Warn("SystemSpeak received empty text; cannot synthesize speech")
		return errors.New("received empty text; cannot synthesize speech")
	}
	texts := utils.SplitByPunctuation(text)
	index := 0
	for _, item := range texts {
		index++
		h.tts_last_text_index = index // Reset the text index
		h.SpeakAndPlay(item, index, h.talkRound)
	}
	return nil
}

// isNeedAuth determines whether verification is needed
func (h *ConnectionHandler) isNeedAuth() bool {
	return !h.isDeviceVerified
}

// processTTSQueueCoroutine processes the TTS queue
func (h *ConnectionHandler) processTTSQueueCoroutine() {
	for {
		select {
		case <-h.stopChan:
			return
		case task := <-h.ttsQueue:
			h.processTTSTask(task.text, task.textIndex, task.round)
		}
	}
}

// stopServerSpeak interrupts server-side speaking
func (h *ConnectionHandler) stopServerSpeak() {
	h.LogInfo("[Server] [voice] stop speaking")
	atomic.StoreInt32(&h.serverVoiceStop, 1)
	h.cleanTTSAndAudioQueue(false)
}

func (h *ConnectionHandler) deleteAudioFileIfNeeded(filepath string, reason string) {
	if !h.config.DeleteAudio || filepath == "" {
		return
	}

	// Check whether it is a quick-reply cache file; if so, do not delete it
	if h.quickReplyCache != nil && h.quickReplyCache.IsCachedFile(filepath) {
		h.LogInfo(fmt.Sprintf(reason+" skipping deletion of cached audio file: %s", filepath))
		return
	}

	// Check whether it is a music file; if so, do not delete it
	if utils.IsMusicFile(filepath) {
		h.LogInfo(fmt.Sprintf(reason+" skipping deletion of music file: %s", filepath))
		return
	}

	// Delete the non-cached audio file
	if err := os.Remove(filepath); err != nil {
		h.LogError(fmt.Sprintf(reason+" failed to delete audio file: %v", err))
	} else {
		h.logger.Debug(fmt.Sprintf(reason+" deleted audio file: %s", filepath))
	}
}

// processTTSTask handles a single TTS task
func (h *ConnectionHandler) processTTSTask(text string, textIndex int, round int) {
	filepath := ""
	defer func() {
		h.audioMessagesQueue <- struct {
			filepath  string
			text      string
			round     int
			textIndex int
		}{filepath, text, round, textIndex}
	}()

	if utils.IsQuickReplyHit(text, h.config.QuickReplyWords) {
		// Try to find the audio file in the cache
		if cachedFile := h.quickReplyCache.FindCachedAudio(text); cachedFile != "" {
			h.LogInfo(fmt.Sprintf("[TTS] [cache] using quick-reply audio file=%s", cachedFile))
			filepath = cachedFile
			return
		}
	}
	ttsStartTime := time.Now()
	// Filter out emoji
	text = utils.RemoveAllEmoji(text)
	// Remove parentheses and their contents (e.g. (speaking fast), (suddenly whispering), etc.)
	text = utils.RemoveParentheses(text)

	if text == "" {
		h.logger.Warn(fmt.Sprintf("[TTS] [warning] received empty text index=%d", textIndex))
		return
	}

	// Generate the speech file
	filepath, err := h.providers.tts.ToTTS(text)
	if err != nil {
		h.LogError(fmt.Sprintf("TTS conversion failed: text(%s) %v", text, err))
		return
	} else {
		h.logger.Debug(fmt.Sprintf("TTS conversion succeeded: text(%s), index(%d) %s", text, textIndex, filepath))
		// If it is a quick-reply word, save it to the cache
		if utils.IsQuickReplyHit(text, h.config.QuickReplyWords) {
			if err := h.quickReplyCache.SaveCachedAudio(text, filepath); err != nil {
				h.LogError(fmt.Sprintf("Failed to save quick-reply audio: %v", err))
			} else {
				h.LogInfo(fmt.Sprintf("[TTS] [cache] successfully cached quick-reply audio text=%s", text))
			}
		}
	}
	if atomic.LoadInt32(&h.serverVoiceStop) == 1 { // Server-side voice stopped
		h.LogInfo(fmt.Sprintf("processTTSTask server-side voice stopped, no longer sending audio data: %s", text))
		// When server-side voice is stopped, delete the generated audio file based on configuration
		h.deleteAudioFileIfNeeded(filepath, "when server-side voice stopped")
		return
	}

	if textIndex == 1 {
		now := time.Now()
		ttsSpentTime := now.Sub(ttsStartTime)
		h.logger.Debug(fmt.Sprintf("TTS conversion time: %s, text: %s, index: %d", ttsSpentTime, text, textIndex))
	}

}

// SpeakAndPlay synthesizes and plays speech
func (h *ConnectionHandler) SpeakAndPlay(text string, textIndex int, round int) error {
	defer func() {
		// Enqueue the task without blocking the current flow
		h.ttsQueue <- struct {
			text      string
			round     int
			textIndex int
		}{text, round, textIndex}
	}()

	originText := text // Keep the original text for logging
	text = utils.RemoveAllEmoji(text)
	text = utils.RemoveMarkdownSyntax(text) // Remove Markdown syntax
	if text == "" {
		h.logger.Warn("SpeakAndPlay received empty text; cannot synthesize speech, %d, text:%s.", textIndex, originText)
		return errors.New("received empty text; cannot synthesize speech")
	}

	if atomic.LoadInt32(&h.serverVoiceStop) == 1 { // Server-side voice stopped
		h.LogInfo(fmt.Sprintf("speakAndPlay server-side voice stopped, no longer sending audio data: %s", text))
		text = ""
		return errors.New("server-side voice has stopped; cannot synthesize speech")
	}

	if len(text) > 255 {
		h.logger.Warn(fmt.Sprintf("Text too long, exceeds the 255-character limit, truncating before synthesis: %s", text))
		text = text[:255] // Truncate the text
	}

	return nil
}

func (h *ConnectionHandler) clearSpeakStatus() {
	h.LogInfo("[Server] [speaking status] cleared")
	h.tts_last_text_index = -1
	h.providers.asr.Reset() // Reset the ASR state
}

func (h *ConnectionHandler) closeOpusDecoder() {
	if h.opusDecoder != nil {
		if err := h.opusDecoder.Close(); err != nil {
			h.LogError(fmt.Sprintf("Failed to close the Opus decoder: %v", err))
		}
		h.opusDecoder = nil
	}
}

func (h *ConnectionHandler) cleanTTSAndAudioQueue(bClose bool) error {
	msgPrefix := ""
	if bClose {
		msgPrefix = "closing connection, "
	}
	// Stop TTS tasks, stop adding text to the TTS queue, and clear the ttsQueue
	for {
		select {
		case task := <-h.ttsQueue:
			h.LogInfo(fmt.Sprintf(msgPrefix+"discarding a TTS task: %s", task.text))
		default:
			// The queue is empty; exit the loop
			h.LogInfo(msgPrefix + "ttsQueue is empty, stopping TTS task processing, preparing to clear the audio queue")
			goto clearAudioQueue
		}
	}

clearAudioQueue:
	// Stop sending from audioMessagesQueue and clear the audio data in the queue
	for {
		select {
		case task := <-h.audioMessagesQueue:
			h.LogInfo(fmt.Sprintf(msgPrefix+"discarding an audio task: %s", task.text))
			// Delete the discarded audio file based on configuration
			h.deleteAudioFileIfNeeded(task.filepath, msgPrefix+"when discarding the audio task")
		default:
			// The queue is empty; exit the loop
			h.LogInfo(msgPrefix + "audioMessagesQueue is empty, stopping audio task processing")
			return nil
		}
	}
}

// Close cleans up resources
func (h *ConnectionHandler) Close() {
	h.closeOnce.Do(func() {
		close(h.stopChan)

		h.closeOpusDecoder()
		if h.providers.tts != nil {
			h.providers.tts.SetVoice(h.initialVoice) // Restore the initial voice
		}
		if h.providers.asr != nil {
			h.providers.asr.ResetSilenceCount() // Reset the silence count
			if err := h.providers.asr.Reset(); err != nil {
				h.LogError(fmt.Sprintf("Failed to reset the ASR state: %v", err))
			}
			if err := h.providers.asr.CloseConnection(); err != nil {
				h.LogError(fmt.Sprintf("Failed to disconnect the ASR connection: %v", err))
			}
		}
		h.cleanTTSAndAudioQueue(true)
	})
}

// genResponseByVLLM uses VLLLM to handle messages containing images
func (h *ConnectionHandler) genResponseByVLLM(ctx context.Context, messages []providers.Message, imageData image.ImageData, text string, round int) error {
	h.logger.Info("Starting to generate the VLLLM reply %v", map[string]interface{}{
		"text":          text,
		"has_url":       imageData.URL != "",
		"has_data":      imageData.Data != "",
		"format":        imageData.Format,
		"message_count": len(messages),
	})

	// Use VLLLM to handle the image and text
	responses, err := h.providers.vlllm.ResponseWithImage(ctx, h.sessionID, messages, imageData, text)
	if err != nil {
		h.LogError(fmt.Sprintf("VLLLM failed to generate a reply, trying to fall back to the regular LLM: %v", err))
		// Fallback strategy: call the regular LLM with the text part only
		fallbackText := fmt.Sprintf("The user sent an image and asked: %s (note: images cannot be processed right now, answering based on text only)", text)
		fallbackMessages := append(messages, providers.Message{
			Role:    "user",
			Content: fallbackText,
		})
		return h.genResponseByLLM(ctx, fallbackMessages, round)
	}

	// Process the VLLLM streaming reply
	var responseMessage []string
	processedChars := 0
	textIndex := 0

	atomic.StoreInt32(&h.serverVoiceStop, 0)

	for response := range responses {
		if response == "" {
			continue
		}

		responseMessage = append(responseMessage, response)
		// Handle segmentation
		fullText := utils.JoinStrings(responseMessage)
		currentText := fullText[processedChars:]

		// Split by punctuation
		if segment, chars := utils.SplitAtLastPunctuation(currentText); chars > 0 {
			textIndex++
			h.tts_last_text_index = textIndex
			h.SpeakAndPlay(segment, textIndex, round)
			processedChars += chars
		}
	}

	// Handle the remaining text
	remainingText := utils.JoinStrings(responseMessage)[processedChars:]
	if remainingText != "" {
		textIndex++
		h.tts_last_text_index = textIndex
		h.SpeakAndPlay(remainingText, textIndex, round)
	}

	// Get the full reply content
	content := utils.JoinStrings(responseMessage)

	// Add the VLLLM reply to the conversation history
	h.dialogueManager.Put(chat.Message{
		Role:    "assistant",
		Content: content,
	})

	h.LogInfo(fmt.Sprintf("VLLLM reply processing complete …%v", map[string]interface{}{
		"content_length": len(content),
		"text_segments":  textIndex,
	}))

	return nil
}

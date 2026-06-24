package core

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"xiaozhi-server-go/src/configs/database"
	"xiaozhi-server-go/src/core/types"
	"xiaozhi-server-go/src/core/utils"
	"xiaozhi-server-go/src/httpsvr/vision"
)

func (h *ConnectionHandler) initMCPResultHandlers() {
	// Initialize MCP result handlers
	// More handler initialization logic can be added here
	h.mcpResultHandlers = map[string]MCPResultHandler{
		"mcp_handler_exit":         h.mcp_handler_exit,
		"mcp_handler_take_photo":   h.mcp_handler_take_photo,
		"mcp_handler_change_voice": h.mcp_handler_change_voice,
		"mcp_handler_change_role":  h.mcp_handler_change_role,
		"mcp_handler_play_music":   h.mcp_handler_play_music,
		"mcp_handler_switch_agent": h.mcp_handler_switch_agent,
	}
}

// mcp_handler_switch_agent handles agent-switch requests; the argument can be {"agent_id": <number>}, {"agent_id": "123"}, or {"agent_name": "name"}
func (h *ConnectionHandler) mcp_handler_switch_agent(args interface{}) string {
	var newAgentID uint = 0
	var agentName string

	switch v := args.(type) {
	case map[string]interface{}:
		if idv, ok := v["agent_id"]; ok {
			switch idt := idv.(type) {
			case float64:
				newAgentID = uint(idt)
			case int:
				newAgentID = uint(idt)
			case string:
				if n, err := strconv.Atoi(idt); err == nil {
					newAgentID = uint(n)
				}
			}
		}
		if namev, ok := v["agent_name"]; ok {
			if s, ok2 := namev.(string); ok2 {
				agentName = s
			}
		}
	case string:
		// If a string is passed directly, try to parse it as a numeric ID, otherwise treat it as a name
		if n, err := strconv.Atoi(v); err == nil {
			newAgentID = uint(n)
		} else {
			agentName = v
		}
	case float64:
		newAgentID = uint(v)
	case int:
		newAgentID = uint(v)
	default:
		h.logger.Error("mcp_handler_switch_agent: unsupported arg type %T", v)
		return "Failed to switch agent: invalid parameter format"
	}

	if newAgentID != 0 && newAgentID == h.agentID {
		h.logger.Info("mcp_handler_switch_agent: already using agent %d", newAgentID)
		h.SystemSpeak("You are already using this agent")
		return "You are already using this agent; no need to switch"
	}

	agents, err := database.ListAgentsByUser(database.GetDB(), database.AdminUserID)
	// Find the agent
	if err != nil {
		h.logger.Error("mcp_handler_switch_agent: ListAgentsByUser failed: %v", err)
		h.SystemSpeak("Failed to switch agent: unable to get the agent list")
		return "Failed to switch agent: unable to get the agent list"
	}
	device, err := database.FindDeviceByID(database.GetDB(), h.deviceID) // Make sure the device exists
	if err != nil || device == nil {
		h.logger.Error("mcp_handler_switch_agent: FindDeviceByID failed: %v", err)
		h.SystemSpeak("Failed to switch agent: unable to get device info")
		return "Failed to switch agent: unable to get device info"
	}

	for _, ag := range agents {
		if ag.ID == newAgentID || (agentName != "" && ag.Name == agentName) {
			// Found the matching agent
			h.logger.Info("mcp_handler_switch_agent: found agent %d, name %s", ag.ID, ag.Name)
			h.agentID = ag.ID
			device.AgentID = &ag.ID
			database.UpdateDevice(database.GetDB(), device) // Update the device's agent_id
			agent, prompt := h.InitWithAgent()
			// Update the dialogue system prompt and keep recent context
			h.dialogueManager.SetSystemMessage(prompt)
			h.dialogueManager.KeepRecentMessages(1)
			// Re-check and switch providers
			h.checkTTSProvider(agent, h.config)
			h.checkLLMProvider(agent, h.config)

			if agent != nil && agent.Name != "" {
				h.SystemSpeak("Switched to " + agent.Name)
			} else {
				h.SystemSpeak("Switched to the new agent")
			}
			return "Agent switched successfully"
		}
	}
	h.SystemSpeak("Could not find the matching agent")
	return "Failed to switch agent: matching agent not found"
}

func (h *ConnectionHandler) handleMCPResultCall(result types.ActionResponse) string {
	errResult := "Failed to call tool"
	// First get the result
	if result.Action != types.ActionTypeCallHandler {
		h.logger.Error("handleMCPResultCall: result.Action is not ActionTypeCallHandler, but %d", result.Action)
		return errResult
	}
	if result.Result == nil {
		h.logger.Error("handleMCPResultCall: result.Result is nil")
		return errResult
	}

	// Extract the result.Result struct, including the function name and arguments
	if Caller, ok := result.Result.(types.ActionResponseCall); ok {
		if handler, exists := h.mcpResultHandlers[Caller.FuncName]; exists {
			// Call the corresponding handler function
			resultStr := handler(Caller.Args)
			return resultStr
		} else {
			h.logger.Error("handleMCPResultCall: no handler found for function %s", Caller.FuncName)
		}
	} else {
		h.logger.Error("handleMCPResultCall: result.Result is not a map[string]interface{}")
	}
	return errResult
}

func (h *ConnectionHandler) mcp_handler_play_music(args interface{}) string {
	if songName, ok := args.(string); ok {
		h.logger.Info("mcp_handler_play_music: %s", songName)
		if path, name, err := utils.GetMusicFilePathFuzzy(songName); err != nil {
			h.logger.Error("mcp_handler_play_music: Play failed: %v", err)
			h.SystemSpeak("Could not find a song named " + songName)
			return "Could not find a song named " + songName
		} else {
			//h.SystemSpeak("Now playing music for you: " + songName)
			h.sendAudioMessage(path, name, h.tts_last_text_index, h.talkRound)
			return "Now playing music: " + name
		}
	} else {
		h.logger.Error("mcp_handler_play_music: args is not a string")
	}
	return "Failed to play music: invalid parameter format"
}

func (h *ConnectionHandler) mcp_handler_change_voice(args interface{}) string {
	if voice, ok := args.(string); ok {
		h.logger.Info("mcp_handler_change_voice: %s", voice)
		if err, voiceName := h.providers.tts.SetVoice(voice); err != nil {
			h.logger.Error("mcp_handler_change_voice: SetVoice failed: %v", err)
			h.SystemSpeak("Failed to change voice: there is no timbre named " + voice)
			return "Failed to change voice: there is no timbre named " + voice
		} else {
			h.LogInfo(fmt.Sprintf("mcp_handler_change_voice: SetVoice success: %s", voiceName))
			h.SystemSpeak("Switched to timbre " + voice)
			return "Voice changed successfully, current timbre: " + voiceName
		}
	} else {
		h.logger.Error("mcp_handler_change_voice: args is not a string")
		return "Failed to change voice: invalid parameter format"
	}
}

func (h *ConnectionHandler) mcp_handler_change_role(args interface{}) string {
	if params, ok := args.(map[string]string); ok {
		role := params["role"]
		prompt := params["prompt"]

		h.logger.Info("mcp_handler_change_role: %s", role)
		h.dialogueManager.SetSystemMessage(prompt)
		h.dialogueManager.KeepRecentMessages(5) // Keep the 5 most recent messages
		if getter, ok := h.providers.tts.(ttsConfigGetter); ok {
			ttsProvider := getter.Config().Type
			if ttsProvider == "edge" {
				// Role names must match those defined in config.yaml (roles section)
				if role == "Shaanxi Girlfriend" {
					h.providers.tts.SetVoice("zh-CN-shaanxi-XiaoniNeural") // Shaanxi Girlfriend timbre
				} else if role == "English Teacher" {
					h.providers.tts.SetVoice("zh-CN-XiaoyiNeural") // English Teacher timbre
				} else if role == "Curious Boy" {
					h.providers.tts.SetVoice("zh-CN-YunxiNeural") // Curious Boy timbre
				}
			}
		}
		h.SystemSpeak("Switched to new role " + role)
		return "Role changed successfully: " + role
	} else {
		h.logger.Error("mcp_handler_change_role: args is not a string")
		return "Failed to change role: invalid parameter format"
	}
}

func (h *ConnectionHandler) mcp_handler_exit(args interface{}) string {
	if text, ok := args.(string); ok {
		h.closeAfterChat = true
		h.SystemSpeak(text)
		return "Ending the conversation: " + text
	} else {
		h.logger.Error("mcp_handler_exit: args is not a string")
		return "Failed to end the conversation: invalid parameter format"
	}
}

func (h *ConnectionHandler) mcp_handler_take_photo(args interface{}) string {
	// Special handling for the take-photo function: parse into a VisionResponse
	resultStr, _ := args.(string)
	var visionResponse vision.VisionResponse
	if err := json.Unmarshal([]byte(resultStr), &visionResponse); err != nil {
		h.logger.Error("Failed to parse VisionResponse: %v", err)
		return "Take photo failed: unable to parse the response"
	}

	if !visionResponse.Success {
		h.logger.Error("Take photo failed: %s", visionResponse.Message)
		h.genResponseByLLM(context.Background(), h.dialogueManager.GetLLMDialogue(), h.talkRound)
		return "Take photo failed: " + visionResponse.Message
	}

	h.SystemSpeak(visionResponse.Result)
	return "Photo taken successfully: " + visionResponse.Result
}

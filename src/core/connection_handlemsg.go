package core

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"xiaozhi-server-go/src/core/chat"
	"xiaozhi-server-go/src/core/image"
	"xiaozhi-server-go/src/core/providers"
	"xiaozhi-server-go/src/core/utils"
)

// handleMessage handles a received message
func (h *ConnectionHandler) handleMessage(messageType int, message []byte) error {
	switch messageType {
	case 1: // Text message
		h.clientTextQueue <- string(message)
		return nil
	case 2: // Binary message (audio data)
		if h.clientAudioFormat == "pcm" {
			// Put the PCM data directly into the queue
			h.clientAudioQueue <- message
		} else if h.clientAudioFormat == "opus" {
			// Check whether the opus decoder is initialized
			if h.opusDecoder != nil {
				// Decode opus data into PCM
				decodedData, err := h.opusDecoder.Decode(message)
				if err != nil {
					h.logger.Error(fmt.Sprintf("Failed to decode Opus audio: %v", err))
					// Even if decoding fails, still try to pass the raw data to ASR
					h.clientAudioQueue <- message
				} else {
					// Decoding succeeded; put the PCM data into the queue
					h.logger.Debug(fmt.Sprintf("Opus decode succeeded: %d bytes -> %d bytes", len(message), len(decodedData)))
					if len(decodedData) > 0 {
						h.clientAudioQueue <- decodedData
					}
				}
			} else {
				// No decoder; pass the raw data directly
				h.clientAudioQueue <- message
			}
		}
		return nil
	default:
		h.logger.Error(fmt.Sprintf("Unknown message type: %d", messageType))
		return fmt.Errorf("unknown message type: %d", messageType)
	}
}

// processClientTextMessage handles text data
func (h *ConnectionHandler) processClientTextMessage(ctx context.Context, text string) error {
	// Parse the JSON message
	var msgJSON interface{}
	if err := json.Unmarshal([]byte(text), &msgJSON); err != nil {
		return h.conn.WriteMessage(1, []byte(text))
	}

	// Check whether it is an integer type
	if _, ok := msgJSON.(float64); ok {
		return h.conn.WriteMessage(1, []byte(text))
	}

	// Parse it as a map to handle the specific message
	msgMap, ok := msgJSON.(map[string]interface{})
	if !ok {
		return fmt.Errorf("invalid message format")
	}

	// Dispatch based on the message type
	msgType, ok := msgMap["type"].(string)
	if !ok {
		return fmt.Errorf("invalid message type")
	}

	switch msgType {
	case "hello":
		return h.handleHelloMessage(msgMap)
	case "abort":
		return h.clientAbortChat()
	case "listen":
		return h.handleListenMessage(msgMap)
	case "iot":
		return h.handleIotMessage(msgMap)
	case "chat":
		return h.handleChatMessage(ctx, text)
	case "vision":
		return h.handleVisionMessage(msgMap)
	case "image":
		return h.handleImageMessage(ctx, msgMap)
	case "mcp":
		return h.mcpManager.HandleXiaoZhiMCPMessage(msgMap)
	default:
		h.logger.Warn("=== Unknown message type ===", map[string]interface{}{
			"unknown_type": msgType,
			"full_message": msgMap,
		})
		return fmt.Errorf("unknown message type: %s", msgType)
	}
}

func (h *ConnectionHandler) handleVisionMessage(msgMap map[string]interface{}) error {
	// Handle a vision message
	cmd := msgMap["cmd"].(string)
	if cmd == "gen_pic" {
	} else if cmd == "gen_video" {
	} else if cmd == "read_img" {
	}
	return nil
}

// handleHelloMessage handles the hello message
// The client uploads info such as the audio format and sample rate
func (h *ConnectionHandler) handleHelloMessage(msgMap map[string]interface{}) error {
	h.LogInfo(fmt.Sprintf("[Client] [hello message received] %v", msgMap))
	// Get the client's encoding format
	if audioParams, ok := msgMap["audio_params"].(map[string]interface{}); ok {
		if format, ok := audioParams["format"].(string); ok {
			h.clientAudioFormat = format
			if format == "pcm" {
				// The client uses PCM format, so the server uses PCM format too
				h.serverAudioFormat = "pcm"
			}
		}
		if sampleRate, ok := audioParams["sample_rate"].(float64); ok {
			h.clientAudioSampleRate = int(sampleRate)
		}
		if channels, ok := audioParams["channels"].(float64); ok {
			h.clientAudioChannels = int(channels)
		}
		if frameDuration, ok := audioParams["frame_duration"].(float64); ok {
			h.clientAudioFrameDuration = int(frameDuration)
		}
		h.LogInfo(fmt.Sprintf("[Client] [audio params %s/%d/%d/%d]",
			h.clientAudioFormat, h.clientAudioSampleRate, h.clientAudioChannels, h.clientAudioFrameDuration))
	}
	h.sendHelloMessage()
	h.closeOpusDecoder()
	// Initialize the opus decoder
	opusDecoder, err := utils.NewOpusDecoder(&utils.OpusDecoderConfig{
		SampleRate:  h.clientAudioSampleRate, // The client uses a 24kHz sample rate
		MaxChannels: h.clientAudioChannels,   // Mono audio
	})
	if err != nil {
		h.logger.Error(fmt.Sprintf("Failed to initialize Opus decoder: %v", err))
	} else {
		h.opusDecoder = opusDecoder
		h.LogInfo("[Opus] [decoder] initialized successfully")
	}

	return nil
}

// handleListenMessage handles voice-related messages
func (h *ConnectionHandler) handleListenMessage(msgMap map[string]interface{}) error {

	// Handle the state parameter
	state, ok := msgMap["state"].(string)
	if !ok {
		return fmt.Errorf("listen message is missing the state parameter")
	}

	// Handle the mode parameter
	if mode, ok := msgMap["mode"].(string); ok {
		h.clientListenMode = mode
		h.LogInfo(fmt.Sprintf("[Client] [listen mode %s/%s]", h.clientListenMode, state))
		h.providers.asr.SetListener(h)
	}

	switch state {
	case "start":
		if h.client_asr_text != "" && h.clientListenMode == "manual" {
			h.clientAbortChat()
		}
		h.client_asr_text = ""
	case "stop":
		h.providers.asr.SendLastAudio([]byte{}) // Send empty data to mark the end
		h.LogInfo("Client stopped speech recognition")
	case "detect":
		text, hasText := msgMap["text"].(string)

		if hasText && text != "" {
			// Text only; handle with the regular LLM
			h.LogInfo(fmt.Sprintf("[Detect] [text-only message %s] handling with LLM", text))
			return h.handleChatMessage(context.Background(), text)
		} else {
			// Neither image nor text
			h.logger.Warn("detect message has neither a text nor an image parameter")
			return fmt.Errorf("detect message is missing the text or image parameter")
		}
	}
	return nil
}

// handleIotMessage handles IoT device messages
func (h *ConnectionHandler) handleIotMessage(msgMap map[string]interface{}) error {
	if descriptors, ok := msgMap["descriptors"].([]interface{}); ok {
		// Handle device descriptors
		// The specific IoT device descriptor handling logic needs to be implemented here
		h.LogInfo(fmt.Sprintf("Received IoT device descriptors: %v", descriptors))
	}
	if states, ok := msgMap["states"].([]interface{}); ok {
		// Handle device states
		// The specific IoT device state handling logic needs to be implemented here
		h.LogInfo(fmt.Sprintf("Received IoT device states: %v", states))
	}
	return nil
}

// handleImageMessage handles image messages
func (h *ConnectionHandler) handleImageMessage(ctx context.Context, msgMap map[string]interface{}) error {
	// Increment the conversation round
	h.talkRound++
	currentRound := h.talkRound
	h.LogInfo(fmt.Sprintf("Starting a new image conversation round: %d", currentRound))

	// Check whether a VLLLM provider is available
	if h.providers.vlllm == nil {
		h.logger.Warn("VLLLM service is not configured; the image message will be ignored")
		return h.conn.WriteMessage(1, []byte("The system does not currently support image processing"))
	}

	// Parse the text content
	text, ok := msgMap["text"].(string)
	if !ok {
		text = "Please describe this image" // Default prompt
	}

	// Parse the image data
	imageDataMap, ok := msgMap["image_data"].(map[string]interface{})
	if !ok {
		return fmt.Errorf("image data is missing")
	}

	imageData := image.ImageData{}
	if url, ok := imageDataMap["url"].(string); ok {
		imageData.URL = url
	}
	if data, ok := imageDataMap["data"].(string); ok {
		imageData.Data = data
	}
	if format, ok := imageDataMap["format"].(string); ok {
		imageData.Format = format
	}

	// Validate the image data
	if imageData.URL == "" && imageData.Data == "" {
		return fmt.Errorf("image data is empty")
	}

	h.LogInfo(fmt.Sprintf("Received image message %v", map[string]interface{}{
		"text":        text,
		"has_url":     imageData.URL != "",
		"has_data":    imageData.Data != "",
		"format":      imageData.Format,
		"data_length": len(imageData.Data),
	}))

	// Send the STT message immediately
	err := h.sendSTTMessage(text)
	if err != nil {
		h.logger.Error(fmt.Sprintf("Failed to send STT message: %v", err))
		return fmt.Errorf("failed to send STT message: %v", err)
	}

	// Send the TTS start state
	if err := h.sendTTSMessage("start", "", 0); err != nil {
		h.logger.Error(fmt.Sprintf("Failed to send TTS start state: %v", err))
		return fmt.Errorf("failed to send TTS start state: %v", err)
	}

	// Send the "thinking" emotion
	if err := h.sendEmotionMessage("thinking"); err != nil {
		h.logger.Error(fmt.Sprintf("Failed to send the thinking emotion message: %v", err))
		return fmt.Errorf("failed to send emotion message: %v", err)
	}

	// Add the user message to the conversation history (including a description of the image)
	userMessage := fmt.Sprintf("%s [The user sent an image in %s format]", text, imageData.Format)
	h.dialogueManager.Put(chat.Message{
		Role:    "user",
		Content: userMessage,
	})

	// Get the conversation history
	messages := make([]providers.Message, 0)
	for _, msg := range h.dialogueManager.GetLLMDialogue() {
		// Exclude the last message containing image info, because we will handle it with VLLLM
		if msg.Role == "user" && strings.Contains(msg.Content, "[The user sent an image") {
			continue
		}
		messages = append(messages, providers.Message{
			Role:    msg.Role,
			Content: msg.Content,
		})
	}

	return h.genResponseByVLLM(ctx, messages, imageData, text, currentRound)
}

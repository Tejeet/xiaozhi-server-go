package core

import (
	"encoding/json"
	"fmt"
	"sync/atomic"
	"time"
	"xiaozhi-server-go/src/core/utils"
)

// sendHelloMessage sends the hello message
func (h *ConnectionHandler) sendHelloMessage() error {
	// Add safety checks
	if h.conn == nil {
		return fmt.Errorf("connection object is not initialized; cannot send hello message")
	}

	// Other possible nil checks
	if h.config == nil {
		return fmt.Errorf("config object is not initialized")
	}

	hello := make(map[string]interface{})
	hello["type"] = "hello"
	hello["version"] = 1
	hello["transport"] = "websocket"
	hello["session_id"] = h.sessionID
	hello["audio_params"] = map[string]interface{}{
		"format":         h.serverAudioFormat,
		"sample_rate":    h.serverAudioSampleRate,
		"channels":       h.serverAudioChannels,
		"frame_duration": h.serverAudioFrameDuration,
	}
	data, err := json.Marshal(hello)
	if err != nil {
		return fmt.Errorf("failed to serialize the hello message: %v", err)
	}

	return h.conn.WriteMessage(1, data)
}

func (h *ConnectionHandler) sendTTSMessage(state string, text string, textIndex int) error {
	// Send the TTS state notification
	stateMsg := map[string]interface{}{
		"type":        "tts",
		"state":       state,
		"session_id":  h.sessionID,
		"text":        text,
		"index":       textIndex,
		"audio_codec": "opus", // Indicates Opus encoding is used
	}
	data, err := json.Marshal(stateMsg)
	if err != nil {
		return fmt.Errorf("failed to serialize the %s state: %v", state, err)
	}
	if err := h.conn.WriteMessage(1, data); err != nil {
		return fmt.Errorf("failed to send the %s state: %v", state, err)
	}
	return nil
}

func (h *ConnectionHandler) sendSTTMessage(text string) error {
	sttMsg := map[string]interface{}{
		"type":       "stt",
		"text":       text,
		"session_id": h.sessionID,
	}
	jsonData, err := json.Marshal(sttMsg)
	if err != nil {
		return fmt.Errorf("failed to serialize the STT message: %v", err)
	}
	if err := h.conn.WriteMessage(1, jsonData); err != nil {
		return fmt.Errorf("failed to send the STT message: %v", err)
	}

	return nil
}

// sendEmotionMessage sends an emotion message
func (h *ConnectionHandler) sendEmotionMessage(emotion string) error {
	data := map[string]interface{}{
		"type":       "llm",
		"text":       utils.GetEmotionEmoji(emotion),
		"emotion":    emotion,
		"session_id": h.sessionID,
	}
	jsonData, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("failed to serialize the emotion message: %v", err)
	}
	return h.conn.WriteMessage(1, jsonData)
}

func (h *ConnectionHandler) sendAudioMessage(filepath string, text string, textIndex int, round int) {
	startTime := time.Now() // Record the start time of the send task
	defer func() {
		// After audio sending completes, decide whether to delete the file based on configuration
		h.deleteAudioFileIfNeeded(filepath, "audio sending completed")

		spentTime := time.Since(startTime).Milliseconds()
		h.LogDebug(fmt.Sprintf("[TTS] [send task %d/%dms/%dms] %s", textIndex, h.tts_last_text_index, spentTime, text))
		h.providers.asr.ResetStartListenTime()
		if textIndex == h.tts_last_text_index {
			if round != h.talkRound {
				h.LogInfo("sendTTSMessage stop: skipping the stop-state send, the round has changed")
			} else {
				h.sendTTSMessage("stop", "", textIndex)
				if h.closeAfterChat {
					h.Close()
				} else {
					h.clearSpeakStatus()
				}
			}
		}
	}()

	if len(filepath) == 0 {
		return
	}
	// Check the round
	if round != h.talkRound {
		h.LogInfo(fmt.Sprintf("sendAudioMessage: skipping audio from a stale round: task round=%d, current round=%d, text=%s",
			round, h.talkRound, text))
		// Even when skipping, delete the audio file based on configuration
		h.deleteAudioFileIfNeeded(filepath, "skipped stale round")
		return
	}

	if atomic.LoadInt32(&h.serverVoiceStop) == 1 { // Server-side voice stopped
		h.LogInfo(fmt.Sprintf("sendAudioMessage server-side voice stopped, no longer sending audio data: %s", text))
		// When server-side voice is stopped, also delete the audio file based on configuration
		h.deleteAudioFileIfNeeded(filepath, "server-side voice stopped")
		return
	}

	var audioData [][]byte
	var duration float64
	var err error

	// Use the TTS provider's method to convert the audio to Opus format
	if h.serverAudioFormat == "pcm" {
		h.LogInfo("server-side audio format is PCM, sending directly")
		audioData, duration, err = utils.AudioToPCMData(filepath)
		if err != nil {
			h.LogError(fmt.Sprintf("failed to convert audio to PCM: %v", err))
			return
		}
	} else if h.serverAudioFormat == "opus" {
		audioData, duration, err = utils.AudioToOpusData(filepath)
		if err != nil {
			h.LogError(fmt.Sprintf("failed to convert audio to Opus: %v", err))
			return
		}
	}

	// Send the TTS start-state notification
	if err := h.sendTTSMessage("sentence_start", text, textIndex); err != nil {
		h.LogError(fmt.Sprintf("failed to send TTS start state: %v", err))
		return
	}

	if textIndex == 1 {
		now := time.Now()
		spentTime := now.Sub(h.roundStartTime)
		h.logger.Debug("time to first reply sentence %s, first sentence [%s], round: %d", spentTime, text, round)
	}
	h.logger.Debug("TTS send (%s): \"%s\" (index:%d/%d, duration:%f, frames:%d)", h.serverAudioFormat, text, textIndex, h.tts_last_text_index, duration, len(audioData))

	// Send the audio data in a time-paced manner
	if err := h.sendAudioFrames(audioData, text, round); err != nil {
		h.LogError(fmt.Sprintf("failed to time-pace audio data: %v", err))
		return
	}

	// Send the TTS end-state notification
	if err := h.sendTTSMessage("sentence_end", text, textIndex); err != nil {
		h.LogError(fmt.Sprintf("failed to send TTS end state: %v", err))
		return
	}
}

// sendAudioFrames sends audio frames in a time-paced manner to avoid overflowing the client buffer
func (h *ConnectionHandler) sendAudioFrames(audioData [][]byte, text string, round int) error {
	if len(audioData) == 0 {
		return nil
	}

	startTime := time.Now()
	playPosition := 0 // Playback position (milliseconds)

	// Pre-buffer: send the first few frames to improve playback smoothness
	preBufferFrames := 3
	if len(audioData) < preBufferFrames {
		preBufferFrames = len(audioData)
	}
	preBufferTime := time.Duration(h.serverAudioFrameDuration*preBufferFrames) * time.Millisecond // Pre-buffer time (milliseconds)

	// Send the pre-buffer frames
	for i := 0; i < preBufferFrames; i++ {
		// Check whether it was interrupted
		if atomic.LoadInt32(&h.serverVoiceStop) == 1 || round != h.talkRound {
			h.LogInfo(fmt.Sprintf("audio sending interrupted (pre-buffer stage): frame=%d/%d, text=%s", i+1, preBufferFrames, text))
			return nil
		}

		if err := h.conn.WriteMessage(2, audioData[i]); err != nil {
			return fmt.Errorf("failed to send pre-buffer audio frame: %v", err)
		}
		playPosition += h.serverAudioFrameDuration
	}

	// Send the remaining audio frames
	remainingFrames := audioData[preBufferFrames:]
	for i, chunk := range remainingFrames {
		// Check for interruption or a round change
		if atomic.LoadInt32(&h.serverVoiceStop) == 1 || round != h.talkRound {
			h.LogInfo(fmt.Sprintf("audio sending interrupted: frame=%d/%d, text=%s", i+preBufferFrames+1, len(audioData), text))
			return nil
		}

		// Check whether the connection is closed
		select {
		case <-h.stopChan:
			return nil
		default:
		}

		// Compute the expected send time
		expectedTime := startTime.Add(time.Duration(playPosition)*time.Millisecond - preBufferTime)
		currentTime := time.Now()
		delay := expectedTime.Sub(currentTime)

		// Flow-control delay handling
		if delay > 0 {
			// Use a simple interruptible sleep
			ticker := time.NewTicker(10 * time.Millisecond) // Fixed 10ms check interval
			defer ticker.Stop()

			endTime := time.Now().Add(delay)
			for time.Now().Before(endTime) {
				select {
				case <-ticker.C:
					// Check the interruption conditions
					if atomic.LoadInt32(&h.serverVoiceStop) == 1 || round != h.talkRound {
						h.LogInfo(fmt.Sprintf("audio sending interrupted during delay: frame=%d/%d, text=%s", i+preBufferFrames+1, len(audioData), text))
						return nil
					}
				case <-h.stopChan:
					return nil
				}
			}
		}

		// Send the audio frame
		if err := h.conn.WriteMessage(2, chunk); err != nil {
			return fmt.Errorf("failed to send audio frame: %v", err)
		}

		playPosition += h.serverAudioFrameDuration
	}
	time.Sleep(preBufferTime) // Make sure the pre-buffer time has elapsed
	spentTime := time.Since(startTime).Milliseconds()
	h.LogInfo(fmt.Sprintf("[TTS] [audio frames %d/%dms/%dms] %s", len(audioData), playPosition, spentTime, text))
	return nil
}

package utils

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	MAX_FILENAME_LENGTH = 250 // Maximum file-name length limit
	USER_FILE_SUBSIZE   = 20  // Fixed length to subtract in user file names: date and suffix
)

// AudioCache is the audio cache type
type AudioCache struct {
	CacheDir      string // Cache directory
	TTSProvider   string // TTS provider name
	VoiceName     string // Voice/timbre name
	AudioFormat   string // Audio format, defaults to "mp3"
	SampleRate    int    // Sample rate
	Channels      int    // Number of channels
	BitsPerSample int    // Bits per sample
	DeviceID      string // Device ID, used to distinguish caches for different devices

	CacheFileSubSize int // Fixed length to subtract in the file name
}

// NewAudioCache creates an audio cache configuration
func NewAudioCache(ttsProvider, cacheDir, voiceName, audioFormat string) *AudioCache {
	return &AudioCache{
		CacheDir:      cacheDir,
		TTSProvider:   ttsProvider,
		VoiceName:     voiceName,
		AudioFormat:   audioFormat,
		SampleRate:    24000,
		Channels:      1,
		BitsPerSample: 16,

		CacheFileSubSize: len(ttsProvider) + len(voiceName) + 10, // Compute the fixed length to subtract in the file name
	}
}

func (ac *AudioCache) SetAudioInfo(sampleRate int, channels int, bitsPerSample int) {
	ac.SampleRate = sampleRate
	ac.Channels = channels
	ac.BitsPerSample = bitsPerSample
}

func (ac *AudioCache) SetDeviceID(deviceID string) {
	ac.DeviceID = strings.Replace(deviceID, ":", "_", -1)
}

// FindCachedAudio finds a cached audio file
func (ac *AudioCache) FindCachedAudio(text string) string {
	// Check whether the directory exists
	if _, err := os.Stat(ac.CacheDir); os.IsNotExist(err) {
		return ""
	}

	// Generate the file name
	filename := ac.generateFilename(text, "mp3")

	// Build the full file path
	fullPath := fmt.Sprintf("%s/%s", ac.CacheDir, filename)

	// Check whether the file exists
	if _, err := os.Stat(fullPath); err == nil {
		return fullPath
	}

	// If it doesn't exist, replace the suffix and check for wav
	fullPath = strings.Replace(fullPath, ".mp3", ".wav", 1)
	if _, err := os.Stat(fullPath); err == nil {
		return fullPath
	}

	return ""
}

// SaveCachedAudio saves audio to the cache directory
func (ac *AudioCache) SaveCachedAudio(text string, data []byte) (string, error) {
	suffix := "wav"
	dir := ac.CacheDir

	// Generate the target file name
	filename := ""
	targetPath := ""
	if ac.VoiceName == "user" && ac.DeviceID != "" {
		filename = ac.generateUserFileName(text, suffix)
		dir = fmt.Sprintf("%s/%s", ac.CacheDir, ac.DeviceID)
		targetPath = fmt.Sprintf("%s/%s", dir, filename)
	} else {
		filename = ac.generateFilename(text, suffix)
		targetPath = fmt.Sprintf("%s/%s", ac.CacheDir, filename)
	}

	// Create the cache directory
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("failed to create cache directory: %v", err)
	}

	// Check whether the target file already exists
	if _, err := os.Stat(targetPath); err == nil {
		//DefaultLogger.Info("audio file already exists, skipping save: %s", targetPath)
		return targetPath, nil // File already exists, skip saving
	}

	return SaveAudioToWavFile(data, targetPath, ac.SampleRate, ac.Channels, ac.BitsPerSample, false)
}

func (ac *AudioCache) generateUserFileName(text, suffix string) string {
	// Sanitize the text
	safeText := ac.sanitizeFilename(text, USER_FILE_SUBSIZE)
	if suffix == "" {
		suffix = ac.AudioFormat
	}

	// File name format: yyMMddHHmmss_text.format
	filename := fmt.Sprintf("%02d%02d%02d%02d%02d%02d_%s.%s",
		time.Now().Year(), time.Now().Month(), time.Now().Day(),
		time.Now().Hour(), time.Now().Minute(), time.Now().Second(),
		safeText, suffix)
	return filename
}

// generateFilename generates the audio file name
func (ac *AudioCache) generateFilename(text, suffix string) string {
	// Sanitize the text
	safeText := ac.sanitizeFilename(text, ac.CacheFileSubSize)

	if suffix == "" {
		suffix = ac.AudioFormat
	}
	// File name format: text_provider_voice.format
	filename := fmt.Sprintf(
		"%s_%s_%s.%s",
		safeText,
		ac.TTSProvider,
		ac.VoiceName,
		suffix,
	)

	return filename
}

// sanitizeFilename cleans the file name, removing unsafe characters
func (ac *AudioCache) sanitizeFilename(text string, l int) string {

	// Remove or replace unsafe characters in the file name
	unsafe := regexp.MustCompile(`[\\/:*?"<>|\x00-\x1f]`)
	safe := unsafe.ReplaceAllString(text, "_")

	// Remove leading/trailing underscores, dots, and spaces
	safe = strings.Trim(safe, "_. ")

	maxTextLen := MAX_FILENAME_LENGTH - l // Limit the text length to avoid an overly long file name
	if len(safe) > maxTextLen {
		safe = safe[:maxTextLen]
		// Check whether the last character was truncated
		for len(safe) > 0 {
			r, size := utf8.DecodeLastRuneInString(safe)
			if r == utf8.RuneError && size == 1 {
				safe = safe[:len(safe)-1]
				fmt.Println("Truncated the last incomplete character, new length:", len(safe), "text:", safe)
			} else {
				break
			}
		}
	}

	// If empty after cleaning, use a default name
	if safe == "" {
		safe = "audio"
	}

	return safe
}

// IsAudioCacheHit checks whether the text is an audio-cache hit
func IsAudioCacheHit(text string, audioCacheWords []string) bool {
	return IsInArray(text, audioCacheWords)
}

// IsCachedFile determines whether the given file path is a cache file
func (ac *AudioCache) IsCachedFile(filePath string) bool {
	if filePath == "" {
		return false
	}

	// Get the directory part of the file
	dir := filepath.Dir(filePath)

	// Simply check whether the file's parent directory is the cache directory
	return dir == ac.CacheDir ||
		strings.HasSuffix(dir, "/"+ac.CacheDir) ||
		strings.HasSuffix(dir, "\\"+ac.CacheDir)
}

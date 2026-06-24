package utils

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// QuickReplyCache is the quick-reply cache configuration
type QuickReplyCache struct {
	CacheDir    string // Cache directory, defaults to "wake_replay"
	TTSProvider string // TTS provider name
	VoiceName   string // Voice/timbre name
	AudioFormat string // Audio format, defaults to "mp3"
}

// NewQuickReplyCache creates a quick-reply cache configuration
func NewQuickReplyCache(ttsProvider, voiceName string) *QuickReplyCache {
	return &QuickReplyCache{
		CacheDir:    "wake_replay",
		TTSProvider: ttsProvider,
		VoiceName:   voiceName,
		AudioFormat: "mp3",
	}
}

// FindCachedAudio finds a cached quick-reply audio file
func (qrc *QuickReplyCache) FindCachedAudio(text string) string {
	// Check whether the directory exists
	if _, err := os.Stat(qrc.CacheDir); os.IsNotExist(err) {
		return ""
	}

	// Generate the file name
	filename := qrc.generateFilename(text)

	// Build the full file path
	fullPath := fmt.Sprintf("%s/%s", qrc.CacheDir, filename)

	// Check whether the file exists
	if _, err := os.Stat(fullPath); err == nil {
		return fullPath
	}

	return ""
}

// SaveCachedAudio saves a quick-reply audio file to the cache directory
func (qrc *QuickReplyCache) SaveCachedAudio(text, sourcePath string) error {
	// Create the cache directory
	if err := os.MkdirAll(qrc.CacheDir, 0o755); err != nil {
		return fmt.Errorf("failed to create cache directory: %v", err)
	}

	// Generate the target file name
	filename := qrc.generateFilename(text)
	targetPath := fmt.Sprintf("%s/%s", qrc.CacheDir, filename)

	// Check whether the target file already exists
	if _, err := os.Stat(targetPath); err == nil {
		return nil // File already exists, skip saving
	}

	// Copy the file to the target location
	return qrc.copyFile(sourcePath, targetPath)
}

// generateFilename generates the quick-reply audio file name
func (qrc *QuickReplyCache) generateFilename(text string) string {
	// Sanitize the text
	safeText := qrc.sanitizeFilename(text)

	// File name format: text_provider_voice.format
	filename := fmt.Sprintf(
		"%s_%s_%s.%s",
		safeText,
		qrc.TTSProvider,
		qrc.VoiceName,
		qrc.AudioFormat,
	)

	return filename
}

// sanitizeFilename cleans the file name, removing unsafe characters
func (qrc *QuickReplyCache) sanitizeFilename(text string) string {
	// Remove or replace unsafe characters in the file name
	unsafe := regexp.MustCompile(`[<>:"/\\|?*\s]+`)
	safe := unsafe.ReplaceAllString(text, "_")

	// Limit the file-name length to avoid it being too long
	if len(safe) > 50 {
		safe = safe[:50]
	}

	// Remove leading and trailing underscores
	safe = strings.Trim(safe, "_")

	// If empty after cleaning, use a default name
	if safe == "" {
		safe = "quick_reply"
	}

	return safe
}

// copyFile copies a file
func (qrc *QuickReplyCache) copyFile(src, dst string) error {
	sourceFile, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("failed to open source file: %v", err)
	}
	defer sourceFile.Close()

	targetFile, err := os.Create(dst)
	if err != nil {
		return fmt.Errorf("failed to create target file: %v", err)
	}
	defer targetFile.Close()

	// Copy the file contents
	if _, err := targetFile.ReadFrom(sourceFile); err != nil {
		return fmt.Errorf("failed to copy file contents: %v", err)
	}

	return nil
}

// IsQuickReplyHit checks whether the text is a quick-reply word
func IsQuickReplyHit(text string, quickReplyWords []string) bool {
	return IsInArray(text, quickReplyWords)
}

// IsCachedFile determines whether the given file path is a cache file
func (qrc *QuickReplyCache) IsCachedFile(filePath string) bool {
	if filePath == "" {
		return false
	}

	// Get the directory part of the file
	dir := filepath.Dir(filePath)

	// Simply check whether the file's parent directory is the cache directory
	return dir == qrc.CacheDir ||
		strings.HasSuffix(dir, "/"+qrc.CacheDir) ||
		strings.HasSuffix(dir, "\\"+qrc.CacheDir)
}

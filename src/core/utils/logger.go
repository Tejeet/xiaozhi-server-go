package utils

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// LogLevel is the log level
type LogLevel string

const (
	DebugLevel LogLevel = "debug"
	InfoLevel  LogLevel = "info"
	WarnLevel  LogLevel = "warn"
	ErrorLevel LogLevel = "error"
)

const (
	LogRetentionDays = 7 // Number of days to retain logs, hard-coded to 7 days
)

var DefaultLogger *Logger

type LogCfg struct {
	LogFormat string `yaml:"log_format" json:"log_format"`
	LogLevel  string `yaml:"log_level" json:"log_level"`
	LogDir    string `yaml:"log_dir" json:"log_dir"`
	LogFile   string `yaml:"log_file" json:"log_file"`
}

// CustomTextHandler is a custom text handler that supports colored output and formatting
type CustomTextHandler struct {
	writer io.Writer
	level  slog.Level
	mu     sync.Mutex
}

var (
	colorReset  = "\x1b[0m"
	colorTime   = "\x1b[93m" // Time: bright yellow
	colorDebug  = "\x1b[36m" // DEBUG: cyan
	colorInfo   = "\x1b[32m" // INFO: green
	colorWarn   = "\x1b[33m" // WARN: yellow
	colorError  = "\x1b[31m" // ERROR: red
	colorASR    = "\x1b[35m" // ASR: magenta
	colorLLM    = "\x1b[34m" // LLM: blue
	colorTTS    = "\x1b[95m" // TTS: bright magenta
	colorTiming = "\x1b[92m" // Timing: bright green
)

func (h *CustomTextHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return level >= h.level
}

func (h *CustomTextHandler) Handle(ctx context.Context, r slog.Record) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	// Get the timestamp
	timeStr := r.Time.Format("2006-01-02 15:04:05.000")

	// Get the log level
	levelStr := r.Level.String()

	// Apply color
	var levelColor string
	switch r.Level {
	case slog.LevelDebug:
		levelColor = colorDebug
	case slog.LevelInfo:
		levelColor = colorInfo
	case slog.LevelWarn:
		levelColor = colorWarn
	case slog.LevelError:
		levelColor = colorError
	default:
		levelColor = colorReset
	}

	// Check whether it is a special-stage log
	var stageColor string
	var isStageLog bool
	msg := r.Message

	if strings.HasPrefix(msg, "[ASR]") {
		stageColor = colorASR
		isStageLog = true
	} else if strings.HasPrefix(msg, "[LLM]") {
		stageColor = colorLLM
		isStageLog = true
	} else if strings.HasPrefix(msg, "[TTS]") {
		stageColor = colorTTS
		isStageLog = true
	} else if strings.HasPrefix(msg, "[TIMING]") {
		stageColor = colorTiming
		isStageLog = true
	}

	// Build the output
	var output string
	if isStageLog {
		// Stage log format: [time] [stage] message
		output = fmt.Sprintf("%s[%s]%s %s%s%s",
			colorTime, timeStr, colorReset,
			stageColor, msg, colorReset)
	} else {
		// Regular log format: [time] [level] message
		output = fmt.Sprintf("%s[%s]%s %s[%s]%s %s",
			colorTime, timeStr, colorReset,
			levelColor, levelStr, colorReset,
			msg)
	}

	// Add attributes (if any)
	if r.NumAttrs() > 0 {
		output += " {"
		r.Attrs(func(a slog.Attr) bool {
			output += fmt.Sprintf(" %s=%v", a.Key, a.Value)
			return true
		})
		output += " }"
	}
	output += "\n"

	_, err := h.writer.Write([]byte(output))
	return err
}

func (h *CustomTextHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return h // Simplified implementation
}

func (h *CustomTextHandler) WithGroup(name string) slog.Handler {
	return h // Simplified implementation
}

// Logger is the logging interface implementation
type Logger struct {
	config      *LogCfg
	jsonLogger  *slog.Logger // File JSON output
	textLogger  *slog.Logger // Console text output
	logFile     *os.File
	currentDate string        // Current date YYYY-MM-DD
	mu          sync.RWMutex  // Read-write lock protection
	ticker      *time.Ticker  // Timer
	stopCh      chan struct{} // Stop signal
}

// configLogLevelToSlogLevel converts the log level from the config to a slog.Level
func configLogLevelToSlogLevel(configLevel string) slog.Level {
	switch strings.ToLower(configLevel) {
	case "debug":
		return slog.LevelDebug
	case "info":
		return slog.LevelInfo
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

// NewLogger creates a new logger
func NewLogger(config *LogCfg) (*Logger, error) {
	// Make sure the log directory exists
	if err := os.MkdirAll(config.LogDir, 0o755); err != nil {
		return nil, fmt.Errorf("failed to create log directory: %v", err)
	}

	// Open or create the log file
	logPath := filepath.Join(config.LogDir, config.LogFile)
	file, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, fmt.Errorf("failed to open log file: %v", err)
	}

	// Set the slog level
	slogLevel := configLogLevelToSlogLevel(config.LogLevel)

	// Create the JSON handler (for file output)
	jsonHandler := slog.NewJSONHandler(file, &slog.HandlerOptions{
		Level: slogLevel,
	})

	// Create the custom text handler (for console output)
	customHandler := &CustomTextHandler{
		writer: os.Stdout,
		level:  slogLevel,
	}

	// Create the logger instances
	jsonLogger := slog.New(jsonHandler)
	textLogger := slog.New(customHandler)

	logger := &Logger{
		config:      config,
		jsonLogger:  jsonLogger,
		textLogger:  textLogger,
		logFile:     file,
		currentDate: time.Now().Format("2006-01-02"),
		stopCh:      make(chan struct{}),
	}

	// Start the log-rotation checker
	logger.startRotationChecker()
	if DefaultLogger == nil {
		DefaultLogger = logger
	}

	return logger, nil
}

// startRotationChecker starts the periodic checker
func (l *Logger) startRotationChecker() {
	l.ticker = time.NewTicker(1 * time.Minute) // Check once per minute
	go func() {
		for {
			select {
			case <-l.ticker.C:
				l.checkAndRotate()
			case <-l.stopCh:
				return
			}
		}
	}()
}

// checkAndRotate checks and performs rotation
func (l *Logger) checkAndRotate() {
	today := time.Now().Format("2006-01-02")
	if today != l.currentDate {
		l.rotateLogFile(today)
		l.cleanOldLogs()
	}
}

// rotateLogFile performs log rotation
func (l *Logger) rotateLogFile(newDate string) {
	l.mu.Lock()
	defer l.mu.Unlock()

	// Close the current log file
	if l.logFile != nil {
		l.logFile.Close()
	}

	// Build the old and new file names
	logDir := l.config.LogDir
	currentLogPath := filepath.Join(logDir, l.config.LogFile)

	// Generate a file name with the date
	baseFileName := strings.TrimSuffix(l.config.LogFile, filepath.Ext(l.config.LogFile))
	ext := filepath.Ext(l.config.LogFile)
	archivedLogPath := filepath.Join(logDir, fmt.Sprintf("%s-%s%s", baseFileName, l.currentDate, ext))

	// Rename the current log file to the dated file
	if _, err := os.Stat(currentLogPath); err == nil {
		if err := os.Rename(currentLogPath, archivedLogPath); err != nil {
			// If the rename fails, log it to the console
			l.textLogger.Error("failed to rename log file", slog.String("error", err.Error()))
		}
	}

	// Create the new log file
	file, err := os.OpenFile(currentLogPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		l.textLogger.Error("failed to create new log file", slog.String("error", err.Error()))
		return
	}

	// Update the logger config
	l.logFile = file
	l.currentDate = newDate

	// Re-create the JSON handler
	slogLevel := configLogLevelToSlogLevel(l.config.LogLevel)
	jsonHandler := slog.NewJSONHandler(file, &slog.HandlerOptions{
		Level: slogLevel,
	})
	l.jsonLogger = slog.New(jsonHandler)

	// Log the rotation info
	l.textLogger.Info("log file rotated", slog.String("new_date", newDate))
}

// cleanOldLogs cleans up old log files
func (l *Logger) cleanOldLogs() {
	logDir := l.config.LogDir

	// Read the log directory
	entries, err := os.ReadDir(logDir)
	if err != nil {
		l.textLogger.Error("failed to read log directory", slog.String("error", err.Error()))
		return
	}

	// Compute the retention cutoff date
	cutoffDate := time.Now().AddDate(0, 0, -LogRetentionDays)
	baseFileName := strings.TrimSuffix(l.config.LogFile, filepath.Ext(l.config.LogFile))
	ext := filepath.Ext(l.config.LogFile)

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		fileName := entry.Name()
		// Check whether it is a dated log file in the format: server-YYYY-MM-DD.log
		if strings.HasPrefix(fileName, baseFileName+"-") && strings.HasSuffix(fileName, ext) {
			// Extract the date part
			dateStr := strings.TrimPrefix(fileName, baseFileName+"-")
			dateStr = strings.TrimSuffix(dateStr, ext)

			// Parse the date
			fileDate, err := time.Parse("2006-01-02", dateStr)
			if err != nil {
				continue // If the date format is incorrect, skip
			}

			// If the file date is before the cutoff date, delete the file
			if fileDate.Before(cutoffDate) {
				filePath := filepath.Join(logDir, fileName)
				if err := os.Remove(filePath); err != nil {
					l.textLogger.Error("failed to delete old log file",
						slog.String("file", fileName),
						slog.String("error", err.Error()))
				} else {
					l.textLogger.Info("deleted old log file", slog.String("file", fileName))
				}
			}
		}
	}
}

// Close closes the log file
func (l *Logger) Close() error {
	// Stop the timer
	if l.ticker != nil {
		l.ticker.Stop()
	}
	// Send the stop signal
	close(l.stopCh)
	// Close the log file
	if l.logFile != nil {
		return l.logFile.Close()
	}
	return nil
}

// log is the generic logging function (internal use)
func (l *Logger) log(level slog.Level, msg string, fields ...interface{}) {
	// Use a read lock to protect concurrent access
	l.mu.RLock()
	defer l.mu.RUnlock()

	// Build the slog attributes
	var attrs []slog.Attr
	if len(fields) > 0 && fields[0] != nil {
		// Handle the fields parameter
		if fieldsMap, ok := fields[0].(map[string]interface{}); ok {
			// Extract and sort the keys
			keys := make([]string, 0, len(fieldsMap))
			for k := range fieldsMap {
				keys = append(keys, k)
			}
			sort.Strings(keys)

			// Add attributes in sorted key order
			for _, k := range keys {
				attrs = append(attrs, slog.Any(k, fieldsMap[k]))
			}
		} else {
			// If it is not a map, add it directly as the "fields" field
			attrs = append(attrs, slog.Any("fields", fields[0]))
		}
	}

	// Write to both the file (JSON) and the console (text)
	ctx := context.Background()
	l.jsonLogger.LogAttrs(ctx, level, msg, attrs...)
	l.textLogger.LogAttrs(ctx, level, msg, attrs...)
}

// Debug logs at the debug level
func (l *Logger) Debug(msg string, args ...interface{}) {
	if l.config.LogLevel == "DEBUG" {
		if len(args) > 0 && containsFormatPlaceholders(msg) {
			formattedMsg := fmt.Sprintf(msg, args...)
			l.log(slog.LevelDebug, formattedMsg)
		} else {
			l.log(slog.LevelDebug, msg, args...)
		}
	}
}

func containsFormatPlaceholders(s string) bool {
	return strings.Contains(s, "%")
}

// Info logs at the info level
func (l *Logger) Info(msg string, args ...interface{}) {
	// Detect whether it is in formatting mode
	if len(args) > 0 && containsFormatPlaceholders(msg) {
		// Formatting mode: similar to Info
		formattedMsg := fmt.Sprintf(msg, args...)
		l.log(slog.LevelInfo, formattedMsg)
	} else {
		// Structured mode: the original way
		l.log(slog.LevelInfo, msg, args...)
	}
}

// Warn logs at the warning level
func (l *Logger) Warn(msg string, args ...interface{}) {
	if len(args) > 0 && containsFormatPlaceholders(msg) {
		formattedMsg := fmt.Sprintf(msg, args...)
		l.log(slog.LevelWarn, formattedMsg)
	} else {
		l.log(slog.LevelWarn, msg, args...)
	}
}

// Error logs at the error level
func (l *Logger) Error(msg string, args ...interface{}) {
	if len(args) > 0 && containsFormatPlaceholders(msg) {
		formattedMsg := fmt.Sprintf(msg, args...)
		l.log(slog.LevelError, formattedMsg)
	} else {
		l.log(slog.LevelError, msg, args...)
	}
}

// InfoASR logs info-level messages for the ASR stage
func (l *Logger) InfoASR(msg string, args ...interface{}) {
	prefixedMsg := "[ASR] " + msg
	l.Info(prefixedMsg, args...)
}

// InfoLLM logs info-level messages for the LLM stage
func (l *Logger) InfoLLM(msg string, args ...interface{}) {
	prefixedMsg := "[LLM] " + msg
	l.Info(prefixedMsg, args...)
}

// InfoTTS logs info-level messages for the TTS stage
func (l *Logger) InfoTTS(msg string, args ...interface{}) {
	prefixedMsg := "[TTS] " + msg
	l.Info(prefixedMsg, args...)
}

// InfoTiming logs timing-info messages
func (l *Logger) InfoTiming(msg string, args ...interface{}) {
	prefixedMsg := "[TIMING] " + msg
	l.Info(prefixedMsg, args...)
}

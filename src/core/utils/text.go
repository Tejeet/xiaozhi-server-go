package utils

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"regexp"
	"strings"
	"unicode/utf8"
)

var (
	// Pre-compiled regular expressions
	reSplitString          = regexp.MustCompile(`[.,!?;。！？；：]+`)
	reMarkdownChars        = regexp.MustCompile(`(\*\*|__|\*|_|#{1,6}\s|` + "`" + `{1,3}|~~|>\s|\[.*?\]\(.*?\)|\!\[.*?\]\(.*?\)|\|.*?\|)`)
	reRemoveAllPunctuation = regexp.MustCompile(
		`[.,!?;:，。！？、；：""''「」『』（）\(\)【】\[\]{}《》〈〉—–\-_~·…‖\|\\/*&\^%\$#@\+=<>]`,
	)
	reWakeUpWord          = regexp.MustCompile(`^你好.+`)
	reRemoveParenthesesCN = regexp.MustCompile(`（[^）]*）`)   // Chinese parentheses
	reRemoveParenthesesEN = regexp.MustCompile(`\([^)]*\)`) // English parentheses
)

// SplitAtLastPunctuation splits text at the last punctuation mark, optimizing sentence splitting for chat scenarios
func SplitAtLastPunctuation(text string) (string, int) {
	if len(text) == 0 {
		return "", 0
	}

	// Define sentence-splitting punctuation of different priorities
	// Priority 1: punctuation that forces a pause (period, question mark, exclamation mark, etc.)
	strongPunctuations := []string{"。", "？", "！", "；", "?", "!", ";"}

	// Priority 2: medium-pause punctuation (comma, colon, etc.)
	mediumPunctuations := []string{"，", "：", ",", ".", ":"}

	// Priority 3: light-pause punctuation (enumeration comma, brackets, etc.)
	lightPunctuations := []string{"、", "）", ")", "】", "]", "》", ">", "`", "'"}

	// Dynamically adjust the minimum sentence length to avoid exceeding the text length
	minLength := 2
	if len(text) < minLength {
		minLength = 1
	}

	// First look for strong-pause punctuation
	if segment, pos := findLastPunctuationWithMinLength(text, strongPunctuations, minLength); pos > 0 {
		return segment, pos
	}

	// If the text is fairly long (more than 50 characters), consider medium-pause punctuation
	if len(text) > 50 {
		minLength = 8
		if len(text) < minLength {
			minLength = len(text) / 2
		}
		if segment, pos := findLastPunctuationWithMinLength(text, mediumPunctuations, minLength); pos > 0 {
			return segment, pos
		}
	}

	// If the text is very long (more than 80 characters), consider light-pause punctuation
	if len(text) > 80 {
		minLength = 8
		if len(text) < minLength {
			minLength = len(text) / 2
		}
		if segment, pos := findLastPunctuationWithMinLength(text, lightPunctuations, minLength); pos > 0 {
			return segment, pos
		}
	}

	// If no suitable punctuation is found and the text is too long (more than 100 characters), force a split at a space
	if len(text) > 100 {
		minLength = 8
		if len(text) < minLength {
			minLength = len(text) / 2
		}
		if segment, pos := findLastSpaceWithMinLength(text, minLength); pos > 0 {
			return segment, pos
		}
	}

	// If the text is too long (more than 120 characters), force a split
	if len(text) > 120 {
		cutPos := 80
		if len(text) < cutPos {
			cutPos = len(text) / 2
		}
		return text[:cutPos], cutPos
	}

	return "", 0
}

// findLastPunctuationWithMinLength finds the position of the last punctuation mark, ensuring a minimum length
func findLastPunctuationWithMinLength(text string, punctuations []string, minLength int) (string, int) {
	// Safety check: make sure minLength does not exceed the text length
	if minLength >= len(text) {
		minLength = len(text) - 1
		if minLength < 0 {
			return "", 0
		}
	}

	lastIndex := -1
	foundPunctuation := ""

	for _, punct := range punctuations {
		// Start searching from the minimum-length position
		searchText := text[minLength:]
		if idx := strings.LastIndex(searchText, punct); idx != -1 {
			actualIdx := idx + minLength

			// If it is a decimal point (English period) and both sides are ASCII digits, treat it as a decimal point and skip this position
			if punct == "." {
				// Make sure the surrounding indices are in range before checking (decode safely by rune)
				if actualIdx > 0 && actualIdx < len(text) {
					// Decode the previous rune
					beforeRune, _ := utf8.DecodeLastRuneInString(text[:actualIdx])
					// Decode the next rune (starting at actualIdx+len(punct))
					afterStart := actualIdx + len(punct)
					if afterStart < len(text) {
						afterRune, _ := utf8.DecodeRuneInString(text[afterStart:])
						// Only treat it as a decimal point when both sides are ASCII digits
						if beforeRune >= '0' && beforeRune <= '9' && afterRune >= '0' && afterRune <= '9' {
							continue
						}
					}
				}
			}

			if actualIdx > lastIndex {
				lastIndex = actualIdx
				foundPunctuation = punct
			}
		}
	}

	if lastIndex == -1 {
		return "", 0
	}

	endPos := lastIndex + len(foundPunctuation)

	// Check whether there are closing quotes or brackets after the punctuation that should be kept together
	endPos = adjustForClosingQuotes(text, endPos)

	// Make sure it does not exceed the text length
	if endPos > len(text) {
		endPos = len(text)
	}
	return text[:endPos], endPos
}

// adjustForClosingQuotes adjusts the end position to ensure paired quotes and brackets are kept together
func adjustForClosingQuotes(text string, pos int) int {
	if pos >= len(text) {
		return pos
	}

	// Convert to a rune slice to handle UTF-8 characters correctly
	runes := []rune(text)

	// Convert the byte index pos to a rune index
	runePos := len([]rune(text[:pos]))

	// Only handle the case of Chinese closing quotes
	chineseClosingQuotes := map[rune]bool{
		'”': true, // Chinese closing double quote
		'’': true, // Chinese closing single quote
	}

	// Look ahead at most 2 characters (only consider immediately adjacent quotes)
	maxLookAhead := 2

	for i := 0; i < maxLookAhead && runePos < len(runes); i++ {
		char := runes[runePos]

		// If it is a Chinese closing quote, include it
		if chineseClosingQuotes[char] {
			runePos++
			continue
		}

		// On any other character (including spaces, English quotes, opening quotes, etc.), stop searching
		break
	}

	// Convert the rune index back to a byte index and return it
	return len(string(runes[:runePos]))
}

// findLastSpaceWithMinLength finds the position of the last space, ensuring a minimum length
func findLastSpaceWithMinLength(text string, minLength int) (string, int) {
	// Safety check: make sure minLength does not exceed the text length
	if minLength >= len(text) {
		minLength = len(text) - 1
		if minLength < 0 {
			return "", 0
		}
	}

	// Start searching for a space from the minimum-length position
	searchText := text[minLength:]
	if idx := strings.LastIndex(searchText, " "); idx != -1 {
		actualIdx := idx + minLength
		return text[:actualIdx], actualIdx
	}
	return "", 0
}

func SplitByPunctuation(text string) []string {
	// Split the text using a regular expression
	parts := reSplitString.Split(text, -1)

	// Filter out empty strings
	var result []string
	for _, part := range parts {
		if strings.TrimSpace(part) != "" {
			result = append(result, part)
		}
	}

	return result
}

func RemoveMarkdownSyntax(text string) string {
	// Replace Markdown symbols with empty strings
	cleaned := reMarkdownChars.ReplaceAllString(text, "")

	return cleaned
}

// RemoveAllPunctuation removes all punctuation
func RemoveAllPunctuation(text string) string {
	// Replace punctuation with an empty string
	cleaned := reRemoveAllPunctuation.ReplaceAllString(text, "")
	return cleaned
}

// Extract_json_from_string extracts the JSON part from a string
func Extract_json_from_string(input string) map[string]interface{} {
	// Extract the outermost {}
	start := strings.Index(input, "{")
	if start == -1 {
		fmt.Println("JSON start symbol not found")
		return nil
	}
	bracketCount := 0
	end := -1
outer:
	for i := start; i < len(input); i++ {
		switch input[i] {
		case '{':
			bracketCount++
		case '}':
			bracketCount--
			if bracketCount == 0 {
				end = i
				break outer
			}
		}
	}
	if end == -1 {
		fmt.Println("Complete JSON structure not found")
		return nil
	}
	jsonStr := input[start : end+1]
	var jsonData map[string]interface{}
	if err := json.Unmarshal([]byte(jsonStr), &jsonData); err != nil {
		fmt.Println("JSON parse error:", err)
		return nil
	}
	return jsonData
}

// JoinStrings concatenates a string slice
func JoinStrings(strs []string) string {
	var result string
	for _, s := range strs {
		result += s
	}
	return result
}

// IsWakeUpWord determines whether the text is a wake word, in the format "你好xx" (hello xx)
func IsWakeUpWord(text string) bool {
	// Check whether it matches
	return reWakeUpWord.MatchString(text)
}

// IsInArray determines whether text is in the string array
func IsInArray(text string, array []string) bool {
	for _, item := range array {
		if item == text {
			return true
		}
	}
	return false
}

// RandomSelectFromArray randomly selects one item from a string array and returns it
func RandomSelectFromArray(array []string) string {
	if len(array) == 0 {
		return "I'm here"
	}

	// Generate a random index
	index := rand.Intn(len(array))

	return array[index]
}

func GenerateSecurePassword(length int) string {
	const charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789!@#$%^&*()-_=+[]{}|;:,.<>?/~`"
	password := make([]byte, length)
	for i := range password {
		password[i] = charset[rand.Intn(len(charset))]
	}
	return string(password)
}

// RemoveParentheses removes parentheses and their contents
// Supports both Chinese parentheses （） and English parentheses ()
func RemoveParentheses(text string) string {
	// Remove Chinese parentheses and their contents
	text = reRemoveParenthesesCN.ReplaceAllString(text, "")
	// Remove English parentheses and their contents
	text = reRemoveParenthesesEN.ReplaceAllString(text, "")
	return text
}

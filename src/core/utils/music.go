package utils

import (
	"fmt"
	"math/rand"
	"os"
	"strings"
)

var musicNames []string

// MusicMatch represents a music-file match result
type MusicMatch struct {
	FilePath   string
	FileName   string
	Similarity float64
}

func checkMusicDirectory(musicDir string) bool {
	// Check whether the music directory exists
	if _, err := os.Stat(musicDir); os.IsNotExist(err) {
		return false
	}
	return true
}

// GetAllMusicNames gets the names of all songs
func GetAllMusicNames(musicDir string) ([]string, error) {
	if len(musicNames) > 0 {
		return musicNames, nil
	}

	if !checkMusicDirectory(musicDir) {
		return nil, os.ErrNotExist
	}

	files, err := os.ReadDir(musicDir)
	if err != nil {
		return nil, err
	}

	for _, file := range files {
		if file.IsDir() || strings.HasSuffix(file.Name(), ".json") {
			continue
		}
		musicNames = append(musicNames, file.Name())
	}

	return musicNames, nil
}

func IsMusicFile(filePath string) bool {
	if filePath == "" {
		return false
	}
	if strings.Contains(filePath, "/music/") || strings.Contains(filePath, "\\music\\") {
		return true
	}
	return false
}

func getRandomMusicFile(musicDir string) (string, string, error) {
	files, err := GetAllMusicNames(musicDir)
	if err != nil {
		return "", "", err
	}
	if len(files) == 0 {
		return "", "", fmt.Errorf("no music files found in directory '%s'", musicDir)
	}
	// Randomly select a file
	randomIndex := rand.Intn(len(files))
	fileName := files[randomIndex]
	// Strip the extension
	if dotIndex := strings.LastIndex(fileName, "."); dotIndex != -1 {
		fileName = fileName[:dotIndex]
	}
	return fmt.Sprintf("%s/%s", musicDir, files[randomIndex]), fileName, nil
}

func GetFileNameFromPath(filePath string) string {
	if filePath == "" {
		return ""
	}
	// Get the file-name part
	fileName := filePath[strings.LastIndex(filePath, "/")+1:]
	fileName = fileName[strings.LastIndex(fileName, "\\")+1:] // Handle Windows paths
	// Strip the extension
	if dotIndex := strings.LastIndex(fileName, "."); dotIndex != -1 {
		fileName = fileName[:dotIndex]
	}
	return fileName
}

// GetMusicFilePathFuzzy gets a music file path by song name (fuzzy matching)
func GetMusicFilePathFuzzy(songName string) (string, string, error) {
	musicDir := "./music"

	// "随机" is the Chinese voice command for "random"; keep it alongside "random"
	if songName == "random" || songName == "随机" {
		// For a random request, just return a random music file
		return getRandomMusicFile(musicDir)
	}

	if !checkMusicDirectory(musicDir) {
		return "", "", os.ErrNotExist
	}

	// First try an exact match
	filePath := fmt.Sprintf("%s/%s.mp3", musicDir, songName)
	// If it exists, return it directly
	if _, err := os.Stat(filePath); err == nil {
		return filePath, songName, nil
	}

	// Get all music files
	files, err := GetAllMusicNames(musicDir)
	if err != nil {
		return "", "", err
	}

	var bestMatch MusicMatch
	bestMatch.Similarity = 0.0

	normalizedInput := normalizeString(songName)

	// Compute the similarity for each file
	for _, file := range files {
		fileName := file
		fileNameWithoutExt := strings.TrimSuffix(fileName, ".mp3")
		normalizedFileName := normalizeString(fileNameWithoutExt)

		similarity := calculateSimilarity(normalizedInput, normalizedFileName)

		if similarity > bestMatch.Similarity {
			bestMatch.FilePath = fmt.Sprintf("%s/%s", musicDir, fileName)
			bestMatch.FileName = fileName
			bestMatch.Similarity = similarity
		}
	}

	// If the best match's similarity is >= 0.5, return the result
	if bestMatch.Similarity >= 0.5 {
		fileName := GetFileNameFromPath(bestMatch.FilePath)
		return bestMatch.FilePath, fileName, nil
	}

	return "", "", fmt.Errorf(
		"no music file found matching '%s' (best similarity: %.2f)",
		songName,
		bestMatch.Similarity,
	)
}

// normalizeString normalizes a string: removes special characters and spaces, converts to lowercase
func normalizeString(s string) string {
	var result strings.Builder
	for _, r := range s {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' ||
			r >= 0x4e00 && r <= 0x9fff {
			result.WriteRune(r)
		}
	}
	return strings.ToLower(result.String())
}

// calculateSimilarity computes the similarity between two strings (between 0 and 1)
func calculateSimilarity(s1, s2 string) float64 {
	if s1 == s2 {
		return 1.0
	}

	if len(s1) == 0 || len(s2) == 0 {
		return 0.0
	}

	// Containment match check
	containsSimilarity := 0.0
	if strings.Contains(s1, s2) || strings.Contains(s2, s1) {
		shorter := len(s1)
		longer := len(s2)
		if shorter > longer {
			shorter, longer = longer, shorter
		}
		containsSimilarity = float64(shorter) / float64(longer)
	}

	// Edit-distance similarity
	editDist := editDistance(s1, s2)
	maxLen := len(s1)
	if len(s2) > maxLen {
		maxLen = len(s2)
	}
	editSimilarity := 1.0 - float64(editDist)/float64(maxLen)

	// Longest-common-subsequence similarity
	lcsLen := longestCommonSubsequence(s1, s2)
	lcsSimilarity := float64(lcsLen*2) / float64(len(s1)+len(s2))

	// Combined similarity (weighted)
	finalSimilarity := containsSimilarity*0.3 + editSimilarity*0.4 + lcsSimilarity*0.3

	if finalSimilarity > 1.0 {
		finalSimilarity = 1.0
	}

	return finalSimilarity
}

// editDistance computes the edit distance
func editDistance(s1, s2 string) int {
	m, n := len(s1), len(s2)
	dp := make([][]int, m+1)
	for i := range dp {
		dp[i] = make([]int, n+1)
	}

	for i := 0; i <= m; i++ {
		dp[i][0] = i
	}
	for j := 0; j <= n; j++ {
		dp[0][j] = j
	}

	for i := 1; i <= m; i++ {
		for j := 1; j <= n; j++ {
			if s1[i-1] == s2[j-1] {
				dp[i][j] = dp[i-1][j-1]
			} else {
				dp[i][j] = min(dp[i-1][j], dp[i][j-1], dp[i-1][j-1]) + 1
			}
		}
	}

	return dp[m][n]
}

// longestCommonSubsequence computes the length of the longest common subsequence
func longestCommonSubsequence(s1, s2 string) int {
	m, n := len(s1), len(s2)
	dp := make([][]int, m+1)
	for i := range dp {
		dp[i] = make([]int, n+1)
	}

	for i := 1; i <= m; i++ {
		for j := 1; j <= n; j++ {
			if s1[i-1] == s2[j-1] {
				dp[i][j] = dp[i-1][j-1] + 1
			} else {
				dp[i][j] = max(dp[i-1][j], dp[i][j-1])
			}
		}
	}

	return dp[m][n]
}

// min returns the minimum of three integers
func min(a, b, c int) int {
	if a <= b && a <= c {
		return a
	}
	if b <= c {
		return b
	}
	return c
}

// max returns the maximum of two integers
func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

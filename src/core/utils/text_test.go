package utils

import (
	"strings"
	"testing"
)

func TestRemoveAllPunctuation(t *testing.T) {
	// Note: the input/expected values below are intentionally in Chinese,
	// since this test verifies that Chinese punctuation is handled correctly.
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "exit with period",
			input:    "退出。",
			expected: "退出",
		},
		{
			name:     "Chinese punctuation",
			input:    "你好，世界！这是一个测试。",
			expected: "你好世界这是一个测试",
		},
		{
			name:     "English punctuation",
			input:    "Hello, world! This is a test.",
			expected: "Hello world This is a test",
		},
		{
			name:     "mixed punctuation",
			input:    "测试：English, 中文！Mixed?",
			expected: "测试English 中文Mixed",
		},
		{
			name:     "special symbols",
			input:    "符号@#$%^&*()测试",
			expected: "符号测试",
		},
		{
			name:     "quotes and brackets",
			input:    `"引号"、'单引号'（括号）【方括号】`,
			expected: "引号单引号括号方括号",
		},
		{
			name:     "book-title marks and dashes",
			input:    "《书名》——作者",
			expected: "书名作者",
		},
		{
			name:     "empty string",
			input:    "",
			expected: "",
		},
		{
			name:     "punctuation only",
			input:    "！@#$%^&*(),.?;:",
			expected: "",
		},
		{
			name:     "no punctuation",
			input:    "纯文本没有标点符号",
			expected: "纯文本没有标点符号",
		},
		{
			name:     "digits and letters",
			input:    "abc123测试!@#",
			expected: "abc123测试",
		},
		{
			name:     "ellipsis and hyphen",
			input:    "测试…省略号—连字符-普通",
			expected: "测试省略号连字符普通",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := RemoveAllPunctuation(tt.input)
			if result != tt.expected {
				t.Errorf("RemoveAllPunctuation(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestRemoveAllPunctuation_EdgeCases(t *testing.T) {
	t.Run("spaces only", func(t *testing.T) {
		result := RemoveAllPunctuation("   ")
		expected := "   " // Spaces are not punctuation and should be retained
		if result != expected {
			t.Errorf("RemoveAllPunctuation(%q) = %q, want %q", "   ", result, expected)
		}
	})

	t.Run("mixed spaces and punctuation", func(t *testing.T) {
		result := RemoveAllPunctuation("测试 , 空格！")
		expected := "测试  空格" // Keep spaces, remove punctuation
		if result != expected {
			t.Errorf("RemoveAllPunctuation(%q) = %q, want %q", "测试 , 空格！", result, expected)
		}
	})

	t.Run("consecutive punctuation", func(t *testing.T) {
		result := RemoveAllPunctuation("测试！！！。。。？？？")
		expected := "测试"
		if result != expected {
			t.Errorf("RemoveAllPunctuation(%q) = %q, want %q", "测试！！！。。。？？？", result, expected)
		}
	})
}

// Benchmark tests
func BenchmarkRemoveAllPunctuation(b *testing.B) {
	testString := "这是一个测试字符串，包含各种标点符号！@#$%^&*()，。？；：\"\"''「」『』（）【】[]{}《》〈〉—–-_~·…‖|\\/*&^%$#@+=<>"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		RemoveAllPunctuation(testString)
	}
}

func BenchmarkRemoveAllPunctuation_Short(b *testing.B) {
	testString := "退出。"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		RemoveAllPunctuation(testString)
	}
}

func TestSplitAtLastPunctuation_BasicChinese(t *testing.T) {
	text := "你好，世界！这是测试。继续"
	seg, pos := SplitAtLastPunctuation(text)
	expectedIdx := strings.LastIndex(text, "。")
	if expectedIdx == -1 {
		t.Fatalf("setup error: expected to find full stop in %q", text)
	}
	expectedSeg := text[:expectedIdx+len("。")]
	if pos != len(expectedSeg) || seg != expectedSeg {
		t.Fatalf("SplitAtLastPunctuation(%q) = (%q, %d), want (%q, %d)", text, seg, pos, expectedSeg, len(expectedSeg))
	}
}

func TestSplitAtLastPunctuation_DotDecimal_NoSplit(t *testing.T) {
	// contains "3.14" (dot between digits) and total length > 50 so medium punctuations are considered
	text := strings.Repeat("a", 30) + "3.14" + strings.Repeat("b", 20) // length = 54
	seg, pos := SplitAtLastPunctuation(text)
	if seg != "" || pos != 0 {
		t.Fatalf("SplitAtLastPunctuation(%q) = (%q, %d), want (\"\", 0) — decimal point should not cause split", text, seg, pos)
	}
}

func TestSplitAtLastPunctuation_DotBetweenLetters_Split(t *testing.T) {
	// dot between letters should be treated as punctuation when medium punctuations are considered
	text := strings.Repeat("a", 30) + "end.word" + strings.Repeat("b", 20) // length > 50
	seg, pos := SplitAtLastPunctuation(text)
	expectedIdx := strings.LastIndex(text, ".")
	if expectedIdx == -1 {
		t.Fatalf("setup error: expected to find '.' in %q", text)
	}
	expectedSeg := text[:expectedIdx+1]
	if pos != len(expectedSeg) || seg != expectedSeg {
		t.Fatalf("SplitAtLastPunctuation(%q) = (%q, %d), want (%q, %d)", text, seg, pos, expectedSeg, len(expectedSeg))
	}
}

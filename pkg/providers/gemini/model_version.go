package gemini

import (
	"regexp"
	"strconv"
)

var geminiMajorVersion = regexp.MustCompile(`^(?:models/)?gemini-(\d+)`)

// isGemini3OrLater reports whether model names Gemini 3 or a later generation.
// Gemini 3 returns an ID with every function call and pairs each function
// response with its call by that ID. Aliases whose version cannot be read from
// the name (for example "gemini-flash-latest") are treated as older.
func isGemini3OrLater(model string) bool {
	m := geminiMajorVersion.FindStringSubmatch(model)
	if m == nil {
		return false
	}
	major, err := strconv.Atoi(m[1])
	return err == nil && major >= 3
}

package domain

import (
	"regexp"
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

var (
	slugNonAlphaNum = regexp.MustCompile(`[^a-z0-9]+`)
	slugTrimDash    = regexp.MustCompile(`^-+|-+$`)
)

// Slugify converts a string into a URL-friendly slug.
// "General Chat Room" → "general-chat-room"
// "Dev Team 🚀" → "dev-team"
func Slugify(s string) string {
	// Normalize unicode (NFD) and remove diacritics
	result := norm.NFD.String(s)
	cleaned := strings.Map(func(r rune) rune {
		if unicode.Is(unicode.Mn, r) { // Mn = Mark, Nonspacing (diacritics)
			return -1
		}
		return r
	}, result)

	// Lowercase
	cleaned = strings.ToLower(cleaned)

	// Replace non-alphanumeric with dashes
	cleaned = slugNonAlphaNum.ReplaceAllString(cleaned, "-")

	// Trim leading/trailing dashes
	cleaned = slugTrimDash.ReplaceAllString(cleaned, "")

	if cleaned == "" {
		return "room"
	}

	// Limit length
	if len(cleaned) > 100 {
		cleaned = cleaned[:100]
		cleaned = slugTrimDash.ReplaceAllString(cleaned, "")
	}

	return cleaned
}

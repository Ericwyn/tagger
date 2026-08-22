package providers

import "strings"

// NormalizeLyrics removes transport artefacts while preserving LRC metadata
// and timestamps. Providers frequently return a UTF-8 BOM, CRLF line endings,
// or zero-width characters that make the editor appear to contain different
// lyrics even when the visible text is the same.
func NormalizeLyrics(value string) string {
	value = strings.TrimPrefix(value, "\ufeff")
	value = strings.NewReplacer("\r\n", "\n", "\r", "\n", "\u200b", "", "\u200c", "", "\u200d", "", "\ufeff", "").Replace(value)
	lines := strings.Split(value, "\n")
	for index := range lines {
		lines[index] = strings.TrimRight(lines[index], " \t")
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

func HasLyrics(value string) bool {
	return strings.TrimSpace(value) != ""
}

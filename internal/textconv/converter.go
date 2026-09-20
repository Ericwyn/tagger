// Package textconv contains small, deterministic text normalizers used by
// provider matching and provider-owned output transforms.
package textconv

import (
	"strings"
	"sync"
	"unicode"

	"github.com/longbridgeapp/opencc"
)

var (
	converterOnce sync.Once
	converter     *opencc.OpenCC
)

func loadConverter() *opencc.OpenCC {
	converterOnce.Do(func() {
		// t2s is backed by the OpenCC dictionaries embedded in the pure-Go
		// package. Failure is not expected after a successful build; retaining a
		// nil value keeps matching and provider search available if a future
		// dictionary packaging change is introduced.
		converter, _ = opencc.New("t2s")
	})
	return converter
}

// Simplify converts Traditional Chinese text to Mainland Simplified Chinese.
// Non-Chinese text and formatting are preserved by OpenCC. If the converter
// cannot be initialized, the original value is returned unchanged.
func Simplify(value string) string {
	if strings.TrimSpace(value) == "" || !containsHan(value) {
		return value
	}
	cc := loadConverter()
	if cc == nil {
		return value
	}
	converted, err := cc.Convert(value)
	if err != nil {
		return value
	}
	return converted
}

// SimplifyAll converts a string slice without mutating the caller's slice.
func SimplifyAll(values []string) []string {
	if values == nil {
		return nil
	}
	result := make([]string, len(values))
	for index, value := range values {
		result[index] = Simplify(value)
	}
	return result
}

// NormalizeForMatch applies the same script normalization to both sides of a
// comparison. It deliberately does not trim, fold case, or otherwise alter
// the provider's display value; callers can compose it with their existing
// matching normalization.
func NormalizeForMatch(value string) string {
	return Simplify(value)
}

func containsHan(value string) bool {
	for _, runeValue := range value {
		if unicode.In(runeValue, unicode.Han) {
			return true
		}
	}
	return false
}

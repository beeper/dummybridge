package connector

import (
	"fmt"
	"math/rand"
	"strconv"
	"strings"
	"time"

	"github.com/beeper/ai-bridge/pkg/ai-stream"
)

func parseOptionToken(token string) (string, string, bool) {
	trimmed := strings.TrimPrefix(strings.TrimSpace(token), "--")
	key, value, ok := strings.Cut(trimmed, "=")
	return strings.ToLower(strings.TrimSpace(key)), strings.TrimSpace(value), ok
}

func parseValidatedInt(value string, hasValue bool, token, label string, maxValue int, allowZero bool) (int, error) {
	if !hasValue {
		return 0, fmt.Errorf("%s requires a value", token)
	}
	var n int
	var err error
	if allowZero {
		n, err = parseNonNegativeInt(value, label)
	} else {
		n, err = parsePositiveInt(value, label)
	}
	if err != nil {
		return 0, err
	}
	return n, validateMaxIntValue(n, maxValue, label)
}

func parsePositiveInt(raw, label string) (int, error) {
	n, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || n <= 0 {
		return 0, fmt.Errorf("invalid %s %q", label, raw)
	}
	return n, nil
}

func parseNonNegativeInt(raw, label string) (int, error) {
	n, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || n < 0 {
		return 0, fmt.Errorf("invalid %s %q", label, raw)
	}
	return n, nil
}

func parseDurationRangeMS(value string, hasValue bool, token string) (time.Duration, time.Duration, error) {
	return parseDurationRange(value, hasValue, token, "delay-ms", maxDemoDelay)
}

func parseDurationRange(value string, hasValue bool, token, label string, maxValue time.Duration) (time.Duration, time.Duration, error) {
	minValue, maxRange, err := parseIntRangeOption(value, hasValue, token, label, int(maxValue/time.Millisecond))
	if err != nil {
		return 0, 0, err
	}
	return time.Duration(minValue) * time.Millisecond, time.Duration(maxRange) * time.Millisecond, nil
}

func parseIntRangeOption(value string, hasValue bool, token, label string, maxValue int) (int, int, error) {
	if !hasValue {
		return 0, 0, fmt.Errorf("%s requires a value", token)
	}
	minValue, maxRange, ok := strings.Cut(value, ":")
	if !ok {
		n, err := parseNonNegativeInt(value, label)
		if err != nil {
			return 0, 0, err
		}
		if err := validateMaxIntValue(n, maxValue, label); err != nil {
			return 0, 0, err
		}
		return n, n, nil
	}
	minInt, err := parseNonNegativeInt(minValue, label)
	if err != nil {
		return 0, 0, err
	}
	maxInt, err := parseNonNegativeInt(maxRange, label)
	if err != nil {
		return 0, 0, err
	}
	if maxInt < minInt {
		return 0, 0, fmt.Errorf("invalid %s range %q", label, value)
	}
	if err := validateMaxIntValue(maxInt, maxValue, label); err != nil {
		return 0, 0, err
	}
	return minInt, maxInt, nil
}

func validateMaxIntValue(value, maxValue int, label string) error {
	if value > maxValue {
		return fmt.Errorf("%s %d exceeds the maximum of %d", label, value, maxValue)
	}
	return nil
}

func rngForOptions(seedSet bool, seed, fallback int64) *rand.Rand {
	if !seedSet {
		seed = fallback
	}
	return rand.New(rand.NewSource(seed))
}

func chunkText(text string, rng *rand.Rand, minChunk, maxChunk int) []string {
	if strings.TrimSpace(text) == "" {
		return nil
	}
	if minChunk <= 0 {
		minChunk = defaultChunkMin
	}
	if maxChunk < minChunk {
		maxChunk = minChunk
	}
	var chunks []string
	for len(text) > 0 {
		size := minChunk
		if maxChunk > minChunk {
			size += rng.Intn(maxChunk - minChunk + 1)
		}
		if size > len(text) {
			size = len(text)
		}
		parts := aistream.SplitTextUTF8(text, size)
		chunk := parts[0]
		chunks = append(chunks, chunk)
		text = text[len(chunk):]
	}
	return chunks
}

func splitCount(total, parts, index int) int {
	if total <= 0 || parts <= 0 || index < 0 || index >= parts {
		return 0
	}
	base := total / parts
	remainder := total % parts
	if index < remainder {
		return base + 1
	}
	return base
}

func sliceByStep(text string, parts, index int) string {
	if parts <= 1 || text == "" {
		return text
	}
	start := 0
	for i := 0; i < index; i++ {
		start += splitCount(len(text), parts, i)
	}
	length := splitCount(len(text), parts, index)
	if start >= len(text) || length <= 0 {
		return ""
	}
	end := min(start+length, len(text))
	return text[start:end]
}

func sanitizeToolName(name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	var out strings.Builder
	for _, r := range name {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '_' || r == '-' {
			out.WriteRune(r)
		}
	}
	if out.Len() == 0 {
		return "tool"
	}
	return out.String()
}

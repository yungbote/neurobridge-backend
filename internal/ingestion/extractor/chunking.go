package extractor

import "strings"

// SplitIntoChunks splits long text into overlapping chunks.
func SplitIntoChunks(text string, chunkSize int, overlap int) []string {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}

	// Work in runes so we never cut a UTF-8 sequence in half
	r := []rune(text)

	if chunkSize < 200 {
		chunkSize = 200
	}
	if overlap < 0 {
		overlap = 0
	}
	step := chunkSize - overlap
	if step <= 0 {
		step = chunkSize
	}

	out := make([]string, 0, (len(r)/step)+1)
	for start := 0; start < len(r); start += step {
		end := start + chunkSize
		if end > len(r) {
			end = len(r)
		}

		p := strings.TrimSpace(string(r[start:end]))
		if p != "" {
			out = append(out, p)
		}

		if end == len(r) {
			break
		}
	}

	return out
}

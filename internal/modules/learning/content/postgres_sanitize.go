package content

import "strings"

// SanitizeStringForPostgres removes characters that Postgres cannot store in UTF-8 text/jsonb.
// Today the primary offender is the NUL character, which can appear via JSON "\u0000" escapes.
//
// We replace NUL with the literal sequence "\0" (two characters) so intent is preserved
// for common C/bytes contexts while remaining storable.
func SanitizeStringForPostgres(s string) string {
	if s == "" {
		return s
	}
	// Fast path: no NUL bytes and no surrogate code points.
	if strings.IndexByte(s, 0) < 0 && !containsSurrogateCodePoint(s) {
		return s
	}

	var b strings.Builder
	// Worst-case: every rune becomes 2 chars; keep it simple.
	b.Grow(len(s) + 8)

	for _, r := range s {
		switch {
		case r == 0:
			b.WriteString(`\0`)
		case r >= 0xD800 && r <= 0xDFFF:
			// Surrogate code points are not valid Unicode scalar values.
			b.WriteRune('\uFFFD')
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

func containsSurrogateCodePoint(s string) bool {
	for _, r := range s {
		if r >= 0xD800 && r <= 0xDFFF {
			return true
		}
	}
	return false
}

func sanitizeJSONValueForPostgres(v any) any {
	switch t := v.(type) {
	case map[string]any:
		for k, vv := range t {
			t[k] = sanitizeJSONValueForPostgres(vv)
		}
		return t
	case []any:
		for i := range t {
			t[i] = sanitizeJSONValueForPostgres(t[i])
		}
		return t
	case string:
		return SanitizeStringForPostgres(t)
	default:
		return v
	}
}

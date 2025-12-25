package content

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
	"strings"
)

func HashBytes(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// CanonicalizeJSON marshals a JSON value with stable key ordering and no whitespace.
// Input may be raw bytes, a map/struct, or any json-marshalable value.
func CanonicalizeJSON(v any) ([]byte, error) {
	switch t := v.(type) {
	case []byte:
		var obj any
		if err := json.Unmarshal(t, &obj); err != nil {
			return nil, err
		}
		return json.Marshal(obj)
	default:
		return json.Marshal(v)
	}
}

func HashSources(promptVersion string, schemaVersion int, chunkIDs []string) string {
	promptVersion = strings.TrimSpace(promptVersion)
	if len(chunkIDs) == 0 {
		return HashBytes([]byte(promptVersion + "|schema=" + itoa(schemaVersion) + "|chunks="))
	}
	arr := make([]string, 0, len(chunkIDs))
	seen := map[string]bool{}
	for _, id := range chunkIDs {
		id = strings.TrimSpace(id)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		arr = append(arr, id)
	}
	sort.Strings(arr)
	base := promptVersion + "|schema=" + itoa(schemaVersion) + "|chunks=" + strings.Join(arr, ",")
	return HashBytes([]byte(base))
}

func itoa(i int) string {
	// small helper to avoid importing strconv everywhere in this package.
	if i == 0 {
		return "0"
	}
	neg := false
	if i < 0 {
		neg = true
		i = -i
	}
	var buf [32]byte
	pos := len(buf)
	for i > 0 {
		pos--
		buf[pos] = byte('0' + (i % 10))
		i /= 10
	}
	if neg {
		pos--
		buf[pos] = '-'
	}
	return string(buf[pos:])
}

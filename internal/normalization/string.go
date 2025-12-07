package normalization

import (
  "strings"
)

func ParseInputString(input string) string {
  normalized := strings.ToLower(strings.TrimSpace(input))
  return normalized
}

func ParseInputStringPtr(input *string) *string {
  normalized := strings.ToLower(strings.TrimSpace(*input))
  return &normalized
}


package interest

import (
	"strings"
)

func NormalizeTag(tag string) string { return strings.ToLower(strings.TrimSpace(tag)) }

// ValidateCurrent detects ambiguous legacy rows without rewriting their IDs or display names.
func ValidateCurrent(current []Weight) error {
	seen := make(map[string]struct{}, len(current))
	for _, weight := range current {
		key := NormalizeTag(weight.Tag)
		if key == "" {
			continue
		}
		if _, ok := seen[key]; ok {
			return ErrNormalizedTagCollision
		}
		seen[key] = struct{}{}
	}
	return nil
}

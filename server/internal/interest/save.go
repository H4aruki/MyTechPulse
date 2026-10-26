package interest

import "sort"

// UpdateOnSave applies one SaveBoost per normalized saved tag. Unlike UpdateOnClick it does not decay other weights.
// It leaves the input slice untouched and returns nil on invalid legacy collisions.
func UpdateOnSave(current []Weight, saved []string) ([]Weight, error) {
	if err := ValidateCurrent(current); err != nil {
		return nil, err
	}
	out := make([]Weight, len(current))
	byTag := make(map[string]int, len(current))
	for i, weight := range current {
		out[i] = weight
		key := NormalizeTag(weight.Tag)
		if key != "" {
			byTag[key] = i
		}
	}
	savedSet := make(map[string]struct{}, len(saved))
	for _, raw := range saved {
		key := NormalizeTag(raw)
		if key != "" {
			savedSet[key] = struct{}{}
		}
	}
	newTags := make([]string, 0, len(savedSet))
	for key := range savedSet {
		if i, ok := byTag[key]; ok {
			out[i].Value = clamp(out[i].Value + SaveBoost)
		} else {
			newTags = append(newTags, key)
		}
	}
	sort.Strings(newTags)
	for _, tag := range newTags {
		out = append(out, Weight{Tag: tag, Value: SaveBoost})
	}
	return out, nil
}

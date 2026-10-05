package interest

import "sort"

func clamp(value int64) int64 {
	if value < 0 {
		return 0
	}
	if value > Scale {
		return Scale
	}
	return value
}

// UpdateOnClick decays all current weights, then applies one boost per normalized clicked tag.
// It leaves the input slice untouched and returns nil on invalid legacy collisions.
func UpdateOnClick(current []Weight, clicked []string) ([]Weight, error) {
	if err := ValidateCurrent(current); err != nil {
		return nil, err
	}
	out := make([]Weight, len(current))
	byTag := make(map[string]int, len(current))
	for i, weight := range current {
		weight.Value = clamp(weight.Value) * DecayNumerator / DecayDenominator
		out[i] = weight
		key := NormalizeTag(weight.Tag)
		if key != "" {
			byTag[key] = i
		}
	}
	clickedSet := make(map[string]struct{}, len(clicked))
	for _, raw := range clicked {
		key := NormalizeTag(raw)
		if key != "" {
			clickedSet[key] = struct{}{}
		}
	}
	newTags := make([]string, 0, len(clickedSet))
	for key := range clickedSet {
		if i, ok := byTag[key]; ok {
			out[i].Value = clamp(out[i].Value + ClickBoost)
		} else {
			newTags = append(newTags, key)
		}
	}
	sort.Strings(newTags)
	for _, tag := range newTags {
		out = append(out, Weight{Tag: tag, Value: ClickBoost})
	}
	// Preserve existing DB row order (IDs first), followed by unresolved names in normalized order.
	return out, nil
}

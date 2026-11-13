// SPDX-License-Identifier: AGPL-3.0-only
package timeline

// Related links only unambiguous cached Message-ID references in the anchor's
// current account/configuration. It never changes Space membership or sorts by
// subject. Missing ancestors stay missing; identical subjects prove nothing.
func Related(records []Record, key string) ([]Record, error) {
	anchor := -1
	for i := range records {
		if records[i].Identity.Key == key {
			anchor = i
			break
		}
	}
	if anchor < 0 {
		return nil, ErrNotFound
	}
	selected := records[anchor]
	scoped := []Record{}
	for _, record := range records {
		if record.Ref.AccountID == selected.Ref.AccountID && record.Ref.Revision == selected.Ref.Revision {
			scoped = append(scoped, record)
		}
	}
	ids := map[string]int{}
	parents := make([]int, len(scoped))
	anchor = -1
	for i, record := range scoped {
		parents[i] = i
		if record.Identity.Key == key {
			anchor = i
		}
		if id := record.Preview.MessageID; id != "" {
			if _, ok := ids[id]; ok {
				ids[id] = -1
			} else {
				ids[id] = i
			}
		}
	}
	find := func(i int) int {
		for parents[i] != i {
			parents[i] = parents[parents[i]]
			i = parents[i]
		}
		return i
	}
	for i, record := range scoped {
		for _, reference := range record.Preview.References {
			if j, ok := ids[reference]; ok && j >= 0 {
				a, b := find(i), find(j)
				if a != b {
					parents[b] = a
				}
			}
		}
	}
	root := find(anchor)
	result := []Record{}
	for i, record := range scoped {
		if find(i) == root {
			result = append(result, record)
		}
	}
	return result, nil
}

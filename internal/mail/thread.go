// SPDX-License-Identifier: AGPL-3.0-only
package mail

// ThreadNode describes transport-reference evidence within an explicit scope.
// Callers choose the account/configuration (or legacy account/folder/validity)
// scope; subjects are deliberately absent from this contract.
type ThreadNode struct {
	Scope      string
	MessageID  string
	References []string
}

// ReferenceGroups joins only explicit, unambiguous Message-ID evidence. A
// shared uncached ancestor can connect its cached descendants, without creating
// an ancestor message or inventing a reply target. Ambiguous IDs never link.
func ReferenceGroups(nodes []ThreadNode) []int {
	type scopedID struct{ scope, id string }
	ids := map[scopedID]int{}
	parents := make([]int, len(nodes))
	for i, node := range nodes {
		parents[i] = i
		if node.MessageID != "" {
			key := scopedID{node.Scope, node.MessageID}
			if _, ok := ids[key]; ok {
				ids[key] = -1
			} else {
				ids[key] = i
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
	join := func(i, j int) {
		a, b := find(i), find(j)
		if a > b {
			a, b = b, a
		}
		parents[b] = a
	}
	missing := map[scopedID]int{}
	for i, node := range nodes {
		references := node.References
		if len(references) > 128 {
			references = references[:128]
		}
		for _, reference := range references {
			if reference == "" {
				continue
			}
			key := scopedID{node.Scope, reference}
			if parent, exists := ids[key]; exists {
				if parent >= 0 {
					join(i, parent)
				}
				continue
			}
			if sibling, exists := missing[key]; exists {
				join(i, sibling)
			} else {
				missing[key] = i
			}
		}
	}
	for i := range parents {
		parents[i] = find(i)
	}
	return parents
}

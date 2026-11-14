// SPDX-License-Identifier: AGPL-3.0-only
package mail

import "testing"

func TestReferenceGroupsUseExplicitMissingAncestorsWithoutSubjectOrScopeGuessing(t *testing.T) {
	nodes := []ThreadNode{
		{Scope: "account-one/revision-one", MessageID: "reply-a", References: []string{"uncached-root"}},
		{Scope: "account-one/revision-one", MessageID: "reply-b", References: []string{"uncached-root"}},
		{Scope: "account-two/revision-one", MessageID: "foreign", References: []string{"uncached-root"}},
		{Scope: "account-one/revision-two", MessageID: "reconnected", References: []string{"uncached-root"}},
		{Scope: "account-one/revision-one", MessageID: "unrelated"},
	}
	groups := ReferenceGroups(nodes)
	if groups[0] != groups[1] || groups[0] == groups[2] || groups[0] == groups[3] || groups[0] == groups[4] {
		t.Fatal("reference scope lost", groups)
	}
	nodes = append(nodes, ThreadNode{Scope: "account-one/revision-one", MessageID: "uncached-root"}, ThreadNode{Scope: "account-one/revision-one", MessageID: "uncached-root"})
	groups = ReferenceGroups(nodes)
	if groups[0] == groups[1] || groups[0] == groups[5] || groups[0] == groups[6] {
		t.Fatal("ambiguous duplicate ancestor linked", groups)
	}
}

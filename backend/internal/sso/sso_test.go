package sso

import (
	"encoding/json"
	"slices"
	"testing"
)

func TestGroupsAreAListOrOneName(t *testing.T) {
	for raw, want := range map[string][]string{
		`["a","b"]`: {"a", "b"},
		`"a"`:       {"a"},
		`""`:        nil,
		`null`:      nil,
		`42`:        nil,
		``:          nil,
	} {
		if got := groupsOf(json.RawMessage(raw)); !slices.Equal(got, want) {
			t.Errorf("groupsOf(%s) = %q, want %q", raw, got, want)
		}
	}
}

package filter

import (
	"fmt"
	"slices"
	"strings"
	"testing"
)

var registry = Registry{
	"author": {
		"in": {Arity: -1, Value: func(v string) (string, error) {
			if strings.TrimSpace(v) == "" {
				return "", fmt.Errorf("a name is needed")
			}
			return strings.ToLower(v), nil
		}, SQL: func(v []string, arg func(any) string) string { return "author IN " + arg(v) }},
		"empty": {SQL: func([]string, func(any) string) string { return "author IS NULL" }},
	},
	"year": {
		"between": {Arity: 2, SQL: func(v []string, arg func(any) string) string {
			return "year BETWEEN " + arg(v[0]) + " AND " + arg(v[1])
		}},
	},
}

func compile(t *testing.T, tree string) (string, []any, error) {
	t.Helper()
	n, err := Parse([]byte(tree))
	if err != nil {
		return "", nil, err
	}
	var args []any
	sql, err := registry.Compile(n, func(v any) string {
		args = append(args, v)
		return fmt.Sprintf("$%d", len(args))
	})
	return sql, args, err
}

func TestCompile(t *testing.T) {
	cases := []struct {
		tree, sql string
		args      []any
	}{
		{`{}`, "TRUE", nil},
		{`{"all": []}`, "TRUE", nil},
		{`{"any": []}`, "FALSE", nil},
		{`{"field": "author", "op": "in", "values": ["Austen", "BRONTË"]}`,
			"(author IN $1)", []any{[]string{"austen", "brontë"}}},
		{`{"field": "author", "op": "empty"}`, "(author IS NULL)", nil},
		{`{"all": [{"field": "author", "op": "in", "values": ["a"]}, {"not": {"any": [
			{"field": "year", "op": "between", "values": ["1800", "1850"]},
			{"field": "author", "op": "empty"}]}}]}`,
			"((author IN $1) AND NOT ((year BETWEEN $2 AND $3) OR (author IS NULL)))",
			[]any{[]string{"a"}, "1800", "1850"}},
		// One child needs no parentheses of its own.
		{`{"all": [{"field": "author", "op": "empty"}]}`, "(author IS NULL)", nil},
	}
	for _, c := range cases {
		sql, args, err := compile(t, c.tree)
		if err != nil || sql != c.sql || fmt.Sprint(args) != fmt.Sprint(c.args) {
			t.Errorf("%s:\n got %q %v %v\nwant %q %v", c.tree, sql, args, err, c.sql, c.args)
		}
	}
}

func TestCompileRefuses(t *testing.T) {
	deep := strings.Repeat(`{"not": `, MaxDepth+1) + `{}` + strings.Repeat(`}`, MaxDepth+1)
	many := `{"any": [` + strings.TrimSuffix(strings.Repeat(`{"field": "author", "op": "empty"},`, MaxRules+1), ",") + `]}`
	cases := map[string]string{
		`{"field": "title", "op": "in", "values": ["x"]}`:                                           "cannot be filtered by \"title\". Use one of: author, year",
		`{"field": "author", "op": "like", "values": ["x"]}`:                                        "no op \"like\". Use one of: empty, in",
		`{"field": "author", "op": "in"}`:                                                           "needs at least one value",
		`{"field": "author", "op": "empty", "values": ["x"]}`:                                       "takes 0 values, not 1",
		`{"field": "year", "op": "between", "values": ["1800"]}`:                                    "takes 2 values, not 1",
		`{"field": "author", "op": "in", "values": [" "]}`:                                          "a name is needed",
		`{"field": "author", "op": "in", "values": ["x"], "all": []}`:                               "never several",
		`{"field": "author", "op": "in", "values": ["` + strings.Repeat("x", MaxValueLen+1) + `"]}`: "longer than",
		`{"fields": "author"}`:                                                                      "unknown field",
		`{"all": []} {"all": []}`:                                                                   "nothing after it",
		`{"field": "author", "op": "in", "values": "x"}`:                                            "not a rule tree",
		deep: "nested more than",
		many: "more than 64 rules",
	}
	for tree, want := range cases {
		_, _, err := compile(t, tree)
		if _, isError := err.(*Error); !isError || !strings.Contains(err.Error(), want) {
			t.Errorf("%.60s: err = %v, want an *Error containing %q", tree, err, want)
		}
	}
}

func TestWithout(t *testing.T) {
	tree, err := Parse([]byte(`{"all": [
		{"field": "author", "op": "in", "values": ["a"]},
		{"any": [{"field": "year", "op": "between", "values": ["1800", "1809"]}, {"field": "year", "op": "between", "values": ["1820", "1829"]}]},
		{"any": [{"field": "author", "op": "empty"}, {"field": "year", "op": "between", "values": ["", "1700"]}]}
	]}`))
	if err != nil {
		t.Fatal(err)
	}
	if got := tree.Without("year").All; len(got) != 2 || got[0].Field != "author" || got[1].Any == nil {
		t.Errorf("without year: %+v; want the author rule and the mixed one", got)
	}
	if got := tree.Without("author").All; len(got) != 2 || got[0].Any == nil || len(got[1].Any) != 2 {
		t.Errorf("without author: %+v; want the years and the mixed one", got)
	}
	single := Node{Field: "author", Op: "empty"}
	if got := single.Without("author"); got.Field != "" || got.All != nil {
		t.Errorf("a lone rule on the field leaves %+v", got)
	}
	if fields := tree.Fields(); !slices.Equal(fields, []string{"author", "year"}) {
		t.Errorf("fields %v", fields)
	}
}

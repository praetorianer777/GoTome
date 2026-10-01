// Package filter is the rule tree that narrows a list of books, and its
// compilation into SQL. What the fields are, and the SQL each comparison
// becomes, is the caller's: a Registry names every field a rule may use, so
// a field that is not in it is refused rather than guessed at, and values
// only ever reach the database as parameters.
package filter

import (
	"bytes"
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strings"
)

// Limits on a tree, so that a hand-written one cannot make the database do
// unbounded work.
const (
	MaxDepth    = 8
	MaxRules    = 64
	MaxValues   = 100
	MaxValueLen = 200
)

// Node is one rule, or a combination of rules: exactly one of All, Any, Not
// and Field is set. An All without rules matches every book.
type Node struct {
	All []Node `json:"all,omitempty"`
	Any []Node `json:"any,omitempty"`
	Not *Node  `json:"not,omitempty"`
	// Field, Op and Values make a rule: the author is one of these names,
	// the year of publication lies between two years.
	Field  string   `json:"field,omitempty"`
	Op     string   `json:"op,omitempty"`
	Values []string `json:"values,omitempty"`
}

// Error is a tree that cannot be used, said so that the person who wrote it
// can fix it.
type Error struct{ Message string }

func (e *Error) Error() string { return e.Message }

func errorf(format string, args ...any) error { return &Error{Message: fmt.Sprintf(format, args...)} }

// Parse reads a tree from JSON. Keys it does not know are an error, so that
// a misspelt one does not quietly match everything.
func Parse(data []byte) (Node, error) {
	var n Node
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&n); err != nil {
		return Node{}, errorf("The filter is not a rule tree in JSON: %v.", err)
	}
	if dec.More() {
		return Node{}, errorf("The filter is one JSON object, with nothing after it.")
	}
	return n, nil
}

// Op is one comparison a field allows.
type Op struct {
	// Arity is how many values the comparison takes; -1 is one or more.
	Arity int
	// Value checks one value and returns it as the comparison uses it, or an
	// error that says what is wrong with it. Nil takes any value as it is.
	Value func(string) (string, error)
	// SQL is the condition on a book, aliased b, for the checked values. It
	// hands every value to arg, which returns the placeholder to write.
	SQL func(values []string, arg func(any) string) string
}

// Field is something books can be filtered by, with its comparisons by name.
type Field map[string]Op

// Registry is every field a tree may use, by name.
type Registry map[string]Field

// Compile checks the tree and returns it as a condition on books, aliased b.
// Every value is passed through arg, which collects the query's parameters
// and returns the placeholder for one.
func (r Registry) Compile(n Node, arg func(any) string) (string, error) {
	rules := 0
	return r.compile(n, arg, 0, &rules)
}

func (r Registry) compile(n Node, arg func(any) string, depth int, rules *int) (string, error) {
	if depth > MaxDepth {
		return "", errorf("The filter is nested more than %d deep.", MaxDepth)
	}
	set := 0
	for _, isSet := range []bool{n.All != nil, n.Any != nil, n.Not != nil, n.Field != ""} {
		if isSet {
			set++
		}
	}
	if set > 1 {
		return "", errorf("A rule is one of all, any, not, or a field with its op, never several.")
	}

	join := func(children []Node, sep, none string) (string, error) {
		if len(children) == 0 {
			return none, nil
		}
		parts := make([]string, len(children))
		for i, child := range children {
			sql, err := r.compile(child, arg, depth+1, rules)
			if err != nil {
				return "", err
			}
			parts[i] = sql
		}
		if len(parts) == 1 {
			return parts[0], nil
		}
		return "(" + strings.Join(parts, sep) + ")", nil
	}

	switch {
	case n.Not != nil:
		sql, err := r.compile(*n.Not, arg, depth+1, rules)
		if err != nil {
			return "", err
		}
		return "NOT " + sql, nil
	case n.Any != nil:
		return join(n.Any, " OR ", "FALSE")
	case n.Field == "":
		return join(n.All, " AND ", "TRUE")
	}

	*rules++
	if *rules > MaxRules {
		return "", errorf("The filter has more than %d rules.", MaxRules)
	}
	field, ok := r[n.Field]
	if !ok {
		names := slices.Sorted(maps.Keys(r))
		return "", errorf("Books cannot be filtered by %q. Use one of: %s.", n.Field, strings.Join(names, ", "))
	}
	op, ok := field[n.Op]
	if !ok {
		ops := slices.Sorted(maps.Keys(field))
		return "", errorf("The field %s has no op %q. Use one of: %s.", n.Field, n.Op, strings.Join(ops, ", "))
	}
	switch {
	case op.Arity < 0 && len(n.Values) == 0:
		return "", errorf("The rule on %s %s needs at least one value.", n.Field, n.Op)
	case op.Arity >= 0 && len(n.Values) != op.Arity:
		return "", errorf("The rule on %s %s takes %d values, not %d.", n.Field, n.Op, op.Arity, len(n.Values))
	case len(n.Values) > MaxValues:
		return "", errorf("A rule takes at most %d values.", MaxValues)
	}
	values := make([]string, len(n.Values))
	for i, v := range n.Values {
		if len(v) > MaxValueLen {
			return "", errorf("A value of the rule on %s is longer than %d bytes.", n.Field, MaxValueLen)
		}
		if op.Value != nil {
			checked, err := op.Value(v)
			if err != nil {
				return "", errorf("The rule on %s: %v", n.Field, err)
			}
			v = checked
		}
		values[i] = v
	}
	return "(" + op.SQL(values, arg) + ")", nil
}

// Fields lists the fields the tree's rules use, each once.
func (n Node) Fields() []string {
	var out []string
	var walk func(Node)
	walk = func(n Node) {
		if n.Field != "" && !slices.Contains(out, n.Field) {
			out = append(out, n.Field)
		}
		for _, c := range n.All {
			walk(c)
		}
		for _, c := range n.Any {
			walk(c)
		}
		if n.Not != nil {
			walk(*n.Not)
		}
	}
	walk(n)
	return out
}

// Without returns the tree without the top-level conditions that are about
// the field alone. A facet counts its values this way: what the other fields
// leave, so that picking a second author widens the list instead of emptying
// the counts.
func (n Node) Without(field string) Node {
	about := func(c Node) bool {
		fields := c.Fields()
		return len(fields) == 1 && fields[0] == field
	}
	switch {
	case n.All != nil:
		kept := []Node{}
		for _, c := range n.All {
			if !about(c) {
				kept = append(kept, c)
			}
		}
		return Node{All: kept}
	case about(n):
		return Node{}
	}
	return n
}

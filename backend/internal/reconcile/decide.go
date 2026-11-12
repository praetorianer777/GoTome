package reconcile

import (
	"slices"
	"strconv"
	"strings"

	"github.com/praetorianer777/gotome/backend/internal/catalog"
	"github.com/praetorianer777/gotome/backend/internal/metadata"
)

type textRules struct {
	key func(string) string
	// subset reports whether the key inner is contained in the key outer.
	subset func(inner, outer string) bool
	// fuzzy lets a provider's value agree with a side's when one is a subset
	// of the other ("2010" and "2010-08").
	fuzzy bool
}

func contains(inner, outer string) bool { return strings.Contains(outer, inner) }
func prefixOf(inner, outer string) bool { return strings.HasPrefix(outer, inner) }
func languageSubset(inner, outer string) bool {
	return strings.HasPrefix(outer, inner+"-")
}

// verdict is the outcome of the rules after "equal"; an empty side is a
// conflict.
type verdict struct {
	side         Side
	rule, reason string
}

// comparison is what the rules need to know about a field.
type comparison struct {
	field string
	// lock is the field whose lock and source count, when it is not field.
	lock           string
	emptyA, emptyB bool
	aInB, bInA     bool
	provider       func(rec metadata.Record) Side
}

func (r *run) judge(c comparison) verdict {
	lock := cmpOr(c.lock, c.field)
	guessA, guessB := r.a.guess(lock) && !c.emptyA, r.b.guess(lock) && !c.emptyB
	switch {
	case c.emptyA:
		return verdict{SideB, RuleEmpty, "side a has no value"}
	case c.emptyB:
		return verdict{SideA, RuleEmpty, "side b has no value"}
	case guessA && !guessB:
		return verdict{SideB, RuleFilename, "side a's value is only a guess from a file name"}
	case guessB && !guessA:
		return verdict{SideA, RuleFilename, "side b's value is only a guess from a file name"}
	case c.aInB:
		return verdict{SideB, RuleSubset, "side a's value is part of side b's"}
	case c.bInA:
		return verdict{SideA, RuleSubset, "side b's value is part of side a's"}
	}
	sa, sb := r.a.strong(lock), r.b.strong(lock)
	if sa != sb {
		who := SideA
		if sb {
			who = SideB
		}
		return verdict{who, RuleLocked, "side " + string(who) + "'s value is locked or typed by hand"}
	}
	if c.provider != nil {
		var hit [2]bool
		var by string
		for _, rec := range r.o.Providers {
			switch c.provider(rec) {
			case SideA:
				hit[0] = true
			case SideB:
				hit[1] = true
			default:
				continue
			}
			if by == "" {
				by = rec.Provider
			}
		}
		if hit[0] != hit[1] {
			who := SideA
			if hit[1] {
				who = SideB
			}
			return verdict{SideProvider + "/" + who, RuleProvider,
				"the record of " + by + ", which shares an identifier, agrees with side " + string(who)}
		}
	}
	return verdict{rule: RuleConflict, reason: "the sides differ and no rule decides between them"}
}

func cmpOr(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// pick resolves a verdict's side, which may be "provider/<side>", to the
// side whose value is taken and the side to report.
func pick(v verdict) (take, from Side) {
	if s, ok := strings.CutPrefix(string(v.side), string(SideProvider)+"/"); ok {
		return Side(s), SideProvider
	}
	return v.side, v.side
}

func nonEmpty(s string) []string {
	if s == "" {
		return nil
	}
	return []string{s}
}

func (r *run) bySide(s Side, a, b string) string {
	if s == SideA {
		return a
	}
	return b
}

// prefer chooses between two spellings of one thing: the one most books use
// or a provider writes, then the one the side typed or locked, then side a.
func (r *run) prefer(field, va, vb string, extra []string) Side {
	score := func(v string) int {
		n := r.o.Usage[v]
		if slices.Contains(extra, v) {
			n += 1 << 20
		}
		return n
	}
	if sa, sb := score(va), score(vb); sa != sb {
		if sa > sb {
			return SideA
		}
		return SideB
	}
	if r.b.strong(field) && !r.a.strong(field) {
		return SideB
	}
	return SideA
}

func (r *run) text(field, va, vb string, tr textRules) Decision {
	ka, kb := tr.key(va), tr.key(vb)
	d := Decision{Field: field, A: nonEmpty(va), B: nonEmpty(vb)}
	if ka == "" && kb == "" {
		d.From, d.Rule, d.Reason = SideA, RuleEmpty, "neither side has a value"
		return d
	}
	if ka == kb {
		who := r.prefer(field, va, vb, r.providerSpellings(field, tr))
		d.From, d.Rule, d.Value = who, RuleEqual, []string{r.bySide(who, va, vb)}
		d.Reason = "both sides say the same once normalised"
		return d
	}
	c := comparison{field: field, emptyA: ka == "", emptyB: kb == ""}
	if tr.subset != nil {
		c.aInB = ka != "" && tr.subset(ka, kb)
		c.bInA = kb != "" && tr.subset(kb, ka)
	}
	c.provider = func(rec metadata.Record) Side {
		pk := tr.key(providerText(field, rec))
		if pk == "" {
			return ""
		}
		same := func(k string) bool {
			return k == pk || tr.fuzzy && k != "" && (tr.subset(k, pk) || tr.subset(pk, k))
		}
		switch {
		case same(ka) && !same(kb):
			return SideA
		case same(kb) && !same(ka):
			return SideB
		}
		return ""
	}
	v := r.judge(c)
	if v.rule == RuleConflict {
		d.Conflict, d.Rule, d.Reason = true, v.rule, v.reason
		return d
	}
	take, from := pick(v)
	d.From, d.Rule, d.Reason, d.Value = from, v.rule, v.reason, []string{r.bySide(take, va, vb)}
	return d
}

func providerText(field string, rec metadata.Record) string {
	switch field {
	case catalog.FieldTitle:
		return (&run{notes: map[string][]Note{}}).cleanTitle(field, "", rec.Title)
	case catalog.FieldSubtitle:
		return rec.Subtitle
	case catalog.FieldSeries:
		return rec.Series
	case catalog.FieldPublisher:
		return rec.Publisher
	case catalog.FieldPublished:
		return rec.Published
	case catalog.FieldLanguage:
		return rec.Language
	}
	return ""
}

func (r *run) providerSpellings(field string, tr textRules) []string {
	var out []string
	for _, rec := range r.o.Providers {
		if v := providerText(field, rec); v != "" {
			out = append(out, v)
		}
	}
	return out
}

// personKeys returns the person keys of names, in order.
func personKeys(names []string) []string {
	out := make([]string, len(names))
	for i, n := range names {
		out[i] = catalog.PersonKey(n)
	}
	return out
}

func subsetOf(inner, outer []string) bool {
	for _, k := range inner {
		if !slices.Contains(outer, k) {
			return false
		}
	}
	return true
}

func (r *run) authors() Decision {
	const field = catalog.FieldContributors
	na, nb := r.a.d.Authors, r.b.d.Authors
	ka, kb := personKeys(na), personKeys(nb)
	d := Decision{Field: field, A: na, B: nb}
	if len(na) == 0 && len(nb) == 0 {
		d.From, d.Rule, d.Reason = SideA, RuleEmpty, "neither side has an author"
		return d
	}
	var provider []string
	for _, rec := range r.o.Providers {
		for _, c := range rec.Contributors {
			if c.Role == "" || c.Role == "author" {
				provider = append(provider, metadata.NaturalName(c.Name))
			}
		}
	}
	if subsetOf(ka, kb) && subsetOf(kb, ka) {
		d.Rule, d.Reason = RuleEqual, "both sides name the same people once normalised"
		d.Value = r.spell(na, nb, provider)
		d.From = fromList(d.Value, na, nb)
		return d
	}
	c := comparison{field: field, emptyA: len(na) == 0, emptyB: len(nb) == 0,
		aInB: len(na) > 0 && subsetOf(ka, kb), bInA: len(nb) > 0 && subsetOf(kb, ka)}
	c.provider = func(rec metadata.Record) Side {
		var pk []string
		for _, c := range rec.Contributors {
			if c.Role == "" || c.Role == "author" {
				pk = append(pk, catalog.PersonKey(c.Name))
			}
		}
		eq := func(k []string) bool { return len(k) > 0 && subsetOf(k, pk) && subsetOf(pk, k) }
		switch {
		case eq(ka) && !eq(kb):
			return SideA
		case eq(kb) && !eq(ka):
			return SideB
		}
		return ""
	}
	v := r.judge(c)
	if v.rule == RuleConflict {
		d.Conflict, d.Rule, d.Reason = true, v.rule, v.reason
		return d
	}
	take, from := pick(v)
	d.From, d.Rule, d.Reason = from, v.rule, v.reason
	if v.rule == RuleSubset {
		inner, outer := nb, na
		if take == SideB {
			inner, outer = na, nb
		}
		d.Value = r.spell(outer, inner, provider)
		return d
	}
	d.Value = slices.Clone(r.sideNames(take))
	return d
}

func (r *run) sideNames(s Side) []string {
	if s == SideA {
		return r.a.d.Authors
	}
	return r.b.d.Authors
}

// spell lists the people of order, each under the cleanest spelling of
// order's and other's, then the people only other names.
func (r *run) spell(order, other, provider []string) []string {
	best := func(name string) string {
		key := catalog.PersonKey(name)
		for _, o := range slices.Concat(other, provider) {
			if catalog.PersonKey(o) == key && r.better(o, name, provider) {
				name = o
			}
		}
		return name
	}
	var out []string
	for _, n := range order {
		out = append(out, best(n))
	}
	for _, n := range other {
		if !slices.Contains(personKeys(order), catalog.PersonKey(n)) {
			out = append(out, best(n))
		}
	}
	return out
}

func (r *run) better(candidate, current string, provider []string) bool {
	score := func(v string) int {
		n := r.o.Usage[v]
		if slices.Contains(provider, v) {
			n += 1 << 20
		}
		return n
	}
	return score(candidate) > score(current)
}

func fromList(v, a, b []string) Side {
	switch {
	case slices.Equal(v, a):
		return SideA
	case slices.Equal(v, b):
		return SideB
	}
	return SideMerged
}

func (r *run) seriesIndex(series Decision) Decision {
	ia, ib := r.a.d.SeriesIndex, r.b.d.SeriesIndex
	f := func(p *float64) []string {
		if p == nil {
			return nil
		}
		return []string{strconv.FormatFloat(*p, 'f', -1, 64)}
	}
	d := Decision{Field: FieldSeriesIndex, A: f(ia), B: f(ib)}
	if ia == nil && ib == nil {
		d.From, d.Rule, d.Reason = SideA, RuleEmpty, "neither side has a position"
		return d
	}
	if ia != nil && ib != nil && *ia == *ib {
		d.From, d.Rule, d.Reason, d.Value = SideA, RuleEqual, "both sides give the same position", f(ia)
		return d
	}
	ska, skb := catalog.Key(r.a.d.Series), catalog.Key(r.b.d.Series)
	if ska != skb && ska != "" && skb != "" && !series.Conflict {
		if ska == catalog.Key(first(series.Value)) {
			d.From, d.Value = SideA, f(ia)
		} else {
			d.From, d.Value = SideB, f(ib)
		}
		d.Rule, d.Reason = series.Rule, "the position goes with the series that was chosen"
		return d
	}
	c := comparison{field: FieldSeriesIndex, lock: catalog.FieldSeries, emptyA: ia == nil, emptyB: ib == nil}
	c.provider = func(rec metadata.Record) Side {
		if rec.SeriesIndex == nil {
			return ""
		}
		switch {
		case ia != nil && *ia == *rec.SeriesIndex && (ib == nil || *ib != *rec.SeriesIndex):
			return SideA
		case ib != nil && *ib == *rec.SeriesIndex && (ia == nil || *ia != *rec.SeriesIndex):
			return SideB
		}
		return ""
	}
	v := r.judge(c)
	if v.rule == RuleConflict {
		d.Conflict, d.Rule, d.Reason = true, v.rule, v.reason
		return d
	}
	take, from := pick(v)
	d.From, d.Rule, d.Reason = from, v.rule, v.reason
	if take == SideA {
		d.Value = f(ia)
	} else {
		d.Value = f(ib)
	}
	return d
}

func (r *run) indexOf(d Decision) *float64 {
	v, err := strconv.ParseFloat(d.Value[0], 64)
	if err != nil {
		return nil
	}
	return &v
}

func (r *run) union(field string, la, lb []string, key func(string) string) Decision {
	d := Decision{Field: field, A: la, B: lb}
	ka, kb := make([]string, len(la)), make([]string, len(lb))
	for i, v := range la {
		ka[i] = key(v)
	}
	for i, v := range lb {
		kb[i] = key(v)
	}
	out := slices.Clone(la)
	for i, v := range lb {
		if !slices.Contains(keysOf(out, key), kb[i]) {
			out = append(out, v)
		}
	}
	d.Value = out
	switch {
	case len(la) == 0 && len(lb) == 0:
		d.From, d.Rule, d.Reason = SideA, RuleEmpty, "neither side has a value"
	case subsetOf(kb, ka) && subsetOf(ka, kb):
		d.From, d.Rule, d.Reason = SideA, RuleEqual, "both sides have the same values"
	case subsetOf(kb, ka):
		d.From, d.Rule, d.Reason = SideA, RuleSubset, "side b's values are all on side a"
	case subsetOf(ka, kb):
		d.From, d.Rule, d.Reason = SideB, RuleSubset, "side a's values are all on side b"
		d.Value = slices.Clone(lb)
	default:
		d.From, d.Rule, d.Reason = SideMerged, RuleUnion, "the values of both sides are kept"
	}
	return d
}

func keysOf(v []string, key func(string) string) []string {
	out := make([]string, len(v))
	for i, s := range v {
		out[i] = key(s)
	}
	return out
}

func (r *run) identifiers() Decision {
	f := func(ids []catalog.Identifier) []string {
		var out []string
		for _, id := range ids {
			out = append(out, id.Type+":"+id.Value)
		}
		return out
	}
	return r.union(catalog.FieldIdentifiers, f(r.a.d.Identifiers), f(r.b.d.Identifiers), strings.ToLower)
}

func (r *run) tags() Decision {
	return r.union(catalog.FieldTags, r.a.d.Tags, r.b.d.Tags, catalog.Key)
}

func (r *run) cover() Decision {
	d := Decision{Field: catalog.FieldCover}
	has := []string{"present"}
	switch a, b := r.a.d.HasCover, r.b.d.HasCover; {
	case a && b:
		d.From, d.Rule, d.Reason, d.Value = SideA, RuleEqual, "both sides have a cover", has
	case a:
		d.From, d.Rule, d.Reason, d.Value = SideA, RuleEmpty, "side b has no cover", has
	case b:
		d.From, d.Rule, d.Reason, d.Value = SideB, RuleEmpty, "side a has no cover", has
	default:
		d.From, d.Rule, d.Reason = SideA, RuleEmpty, "neither side has a cover"
	}
	return d
}

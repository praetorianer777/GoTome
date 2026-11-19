package genres

import (
	"hash/fnv"
	"slices"

	"github.com/google/uuid"
)

const (
	// checkFolds is how many folds the check's books are cut into by a
	// hash of their ID; each is judged by an ensemble trained on the rest.
	checkFolds = 5
	// minCheckGenreBooks is how many books of the pool a genre needs to be
	// trained at all.
	minCheckGenreBooks = 20
	// minCheckWordBooks is how many books of a language get it a word table.
	minCheckWordBooks = 100
	inputPrefix       = "ensemble-"
)

// inputMethod names the check of one of the ensemble's inputs.
func inputMethod(input string) string { return inputPrefix + input }

func isInputMethod(method string) bool {
	return len(method) > len(inputPrefix) && method[:len(inputPrefix)] == inputPrefix
}

func checkFold(id uuid.UUID) int {
	h := fnv.New32a()
	h.Write([]byte("check-fold:" + id.String()))
	return int(h.Sum32() % checkFolds)
}

// judgeOutOfFold trains an ensemble on every fold's other books, as the
// app's is trained, and has it judge the sample's books of that fold: no
// book is judged by a model that has seen it. It sets the scores and what
// the ensemble decided on the sample, and returns how many books an
// ensemble was trained on on average and for how many genres.
func judgeOutOfFold(books []*loaded, model string) (trained, genres int) {
	positives := map[string]int{}
	languages := map[string]int{}
	for _, b := range books {
		for _, g := range b.truth {
			positives[g]++
		}
		if b.lang != "" {
			languages[b.lang]++
		}
	}
	var keys []string
	for _, g := range List {
		if positives[g.Key] >= minCheckGenreBooks {
			keys = append(keys, g.Key)
		}
	}
	if len(keys) == 0 {
		return 0, 0
	}
	var wordLangs []string
	for lang, n := range languages {
		if n >= minCheckWordBooks {
			wordLangs = append(wordLangs, lang)
		}
	}
	slices.Sort(wordLangs)
	opt := EnsembleOptions{Version: "check", Model: model, WordLangs: wordLangs, Workers: 4}
	var total, folds int
	for k := range checkFolds {
		var train []TrainBook
		var held []*loaded
		for _, b := range books {
			if checkFold(b.id) == k {
				if b.one != nil {
					held = append(held, b)
				}
				continue
			}
			train = append(train, TrainBook{
				ID: b.id.String(), Lang: b.lang, Counts: b.counts,
				Content: b.vectors.Content, Metadata: b.vectors.Metadata, Labels: b.truth,
			})
		}
		if len(held) == 0 {
			continue
		}
		e := TrainEnsemble(train, keys, opt)
		total += e.Books
		folds++
		for _, b := range held {
			j := e.Judge(Input{
				Lang: b.lang, Counts: b.counts, Model: model,
				Content: b.vectors.Content, Metadata: b.vectors.Metadata, Shares: b.shares,
			})
			one := b.one
			one.given[MethodEnsemble] = []string{}
			if j.Scores != nil {
				one.scores[MethodEnsemble] = map[string]float64{}
				for g, key := range e.Genres {
					one.scores[MethodEnsemble][key] = j.Scores[g]
				}
				picked, _ := e.Decide(j, nil, MaxGenres)
				for _, p := range picked {
					one.given[MethodEnsemble] = append(one.given[MethodEnsemble], p.Genre)
				}
			}
			for i, name := range InputNames {
				if j.Inputs[i] == nil {
					continue
				}
				m := map[string]float64{}
				for g, key := range e.Genres {
					if v := j.Inputs[i][g]; v == v {
						m[key] = v
					}
				}
				one.scores[inputMethod(name)] = m
			}
		}
	}
	if folds == 0 {
		return 0, len(keys)
	}
	return total / folds, len(keys)
}

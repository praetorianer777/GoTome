package catalog

import (
	"context"
	"errors"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/praetorianer777/gotome/backend/internal/db/sqlc"
)

// AgeRatings are the steps of an age rating, after the German FSK and USK
// scales: suitable from the age given, and 18 is adult.
var AgeRatings = []int16{0, 6, 12, 16, 18}

// AdultRating is the rating a book is called adult at.
const AdultRating int16 = 18

// SourceTags is the source of a rating worked out from a book's tags.
const SourceTags = "tags"

// NoAgeRating, in an Edit, takes the rating away.
const NoAgeRating int16 = -1

// ValidAgeRating reports whether r is one of AgeRatings.
func ValidAgeRating(r int16) bool {
	for _, a := range AgeRatings {
		if a == r {
			return true
		}
	}
	return false
}

// tagAgeRatings maps tags, by their Key, to the rating they mean. The list is
// short on purpose: a tag that is in it keeps a book from accounts with a
// lower maximum, so only tags that say so plainly belong here. Extend it
// freely; docs/age-ratings.md lists it.
var tagAgeRatings = map[string]int16{
	"erotica": 18, "erotic": 18, "erotic fiction": 18, "erotik": 18, "erotische literatur": 18,
	"erotischer roman": 18, "erotische romane": 18, "adult": 18,
	"adult fiction": 18, "adults only": 18, "pornography": 18, "pornografie": 18, "hentai": 18,
	"fsk 18": 18, "ab 18": 18, "ab 18 jahre": 18, "18": 18,
	"fsk 16": 16, "ab 16": 16, "ab 16 jahre": 16, "16": 16,
	"fsk 12": 12, "ab 12": 12, "ab 12 jahre": 12, "12": 12,
	"fsk 6": 6, "ab 6": 6, "ab 6 jahre": 6, "6": 6,
}

// TagAgeRating is the highest rating any of the tags means, or nil when none
// of them means one.
func TagAgeRating(tags []string) *int16 {
	var best *int16
	for _, tag := range tags {
		parts := strings.FieldsFunc(tag, func(r rune) bool { return r == '/' || r == '>' || r == '|' })
		for _, name := range append(parts, tag) {
			if r, ok := tagAgeRatings[Key(name)]; ok && (best == nil || r > *best) {
				best = &r
			}
		}
	}
	return best
}

// RefreshAgeRating gives a book the rating its tags mean. A rating a person
// set (locked) is left; one a provider gave is only raised.
func RefreshAgeRating(ctx context.Context, q *sqlc.Queries, bookID uuid.UUID) error {
	state, err := q.GetBookAgeState(ctx, bookID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil || state.Locked {
		return err
	}
	names, err := q.ListBookTagNames(ctx, bookID)
	if err != nil {
		return err
	}
	derived := TagAgeRating(names)
	fromTags := state.Source == "" || state.Source == SourceTags
	switch {
	case derived == nil && fromTags && state.AgeRating != nil:
		return q.SetBookAgeRating(ctx, sqlc.SetBookAgeRatingParams{ID: bookID})
	case derived != nil && (fromTags && (state.AgeRating == nil || *state.AgeRating != *derived) ||
		!fromTags && (state.AgeRating == nil || *state.AgeRating < *derived)):
		return q.SetBookAgeRating(ctx, sqlc.SetBookAgeRatingParams{ID: bookID, Rating: derived, Source: SourceTags})
	}
	return nil
}

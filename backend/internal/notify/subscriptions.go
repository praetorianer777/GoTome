package notify

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	"github.com/praetorianer777/gotome/backend/internal/auth"
	"github.com/praetorianer777/gotome/backend/internal/db"
	"github.com/praetorianer777/gotome/backend/internal/db/sqlc"
)

// ErrNotYours is the error of a choice about a kind of event the person
// may not hear of.
var ErrNotYours = errors.New("no such kind of event for this person")

// Subscription is whether a person hears of a kind of event in the app.
type Subscription struct {
	Kind string
	App  bool
}

// Subscriptions are the kinds of event the user's role may hear of, in
// order, with what they chose or else the kind's default.
func (s *Service) Subscriptions(ctx context.Context, user auth.User) ([]Subscription, error) {
	rows, err := sqlc.New(s.pool).ListNotificationSubscriptions(ctx, user.ID)
	if err != nil {
		return nil, err
	}
	chosen := map[string]bool{}
	for _, r := range rows {
		chosen[r.Kind] = r.Enabled
	}
	var out []Subscription
	for _, k := range EventKinds {
		if !auth.Allows(user.Role, k.Permission) {
			continue
		}
		on, ok := chosen[k.Kind]
		if !ok {
			on = k.Default
		}
		out = append(out, Subscription{Kind: k.Kind, App: on})
	}
	return out, nil
}

// Subscribe records the user's choices, all or none.
func (s *Service) Subscribe(ctx context.Context, user auth.User, choices []Subscription) error {
	for _, c := range choices {
		k, ok := eventKind(c.Kind)
		if !ok || !auth.Allows(user.Role, k.Permission) {
			return ErrNotYours
		}
	}
	return db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		q := sqlc.New(tx)
		for _, c := range choices {
			if err := q.PutNotificationSubscription(ctx, sqlc.PutNotificationSubscriptionParams{
				UserID: user.ID, Kind: c.Kind, Enabled: c.App,
			}); err != nil {
				return err
			}
		}
		return nil
	})
}

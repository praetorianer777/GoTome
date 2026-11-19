package metadata

import (
	"context"
	"errors"
	"time"
)

// connectionError is a request that got no answer at all: the connection
// was refused, reset or timed out, or what came back was no HTTP over TLS.
type connectionError struct{ err error }

func (e *connectionError) Error() string { return e.err.Error() }
func (e *connectionError) Unwrap() error { return e.err }

var (
	// liveWaits are the pauses before each repeated request of a LiveWeb.
	liveWaits = []time.Duration{5 * time.Second, 20 * time.Second, time.Minute}
	// maxPatience bounds how long a LiveWeb waits for a busy provider.
	maxPatience = 3 * time.Minute
)

// patientWeb asks again, after growing pauses, when a request ends busy or
// without an answer. Every other outcome, a 404 or a refusal among them, is
// passed on at once.
type patientWeb struct {
	web   Web
	waits []time.Duration
	sleep func(context.Context, time.Duration) error
	log   func(format string, args ...any)
	now   func() time.Time
}

func (p *patientWeb) Get(ctx context.Context, r Request) ([]byte, error) {
	for attempt := 0; ; attempt++ {
		body, err := p.web.Get(ctx, r)
		if err == nil || attempt == len(p.waits) || !passing(err) {
			return body, err
		}
		wait := p.waits[attempt]
		var busy *BusyError
		if errors.As(err, &busy) {
			now := time.Now
			if p.now != nil {
				now = p.now
			}
			wait = max(wait, min(busy.Until.Sub(now())+time.Second, maxPatience))
		}
		p.log("asking again in %s after: %v", wait, err)
		if err := p.sleep(ctx, wait); err != nil {
			return nil, err
		}
	}
}

func passing(err error) bool {
	var conn *connectionError
	return errors.Is(err, ErrBusy) || errors.As(err, &conn)
}

func sleep(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

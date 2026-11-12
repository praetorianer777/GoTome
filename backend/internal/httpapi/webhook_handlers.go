package httpapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/praetorianer777/gotome/backend/internal/notify"
	"github.com/praetorianer777/gotome/backend/internal/settings"
	"github.com/praetorianer777/gotome/backend/internal/webhooks"
)

type lastDelivery struct {
	Status       string    `json:"status" enum:"pending,delivered,failed"`
	At           time.Time `json:"at"`
	ResponseCode *int      `json:"responseCode,omitempty"`
}

// webhook is an address told of events. Its secret is never sent.
type webhook struct {
	ID        uuid.UUID `json:"id"`
	Name      string    `json:"name"`
	URL       string    `json:"url"`
	Kinds     []string  `json:"kinds" doc:"The kinds of event it is sent; none is every kind."`
	SecretSet bool      `json:"secretSet"`
	Trusted   bool      `json:"trusted" doc:"Whether the address may be inside the server's own network."`
	Enabled   bool      `json:"enabled"`
	CreatedAt time.Time `json:"createdAt"`
	// Last is the latest delivery; left out until there is one.
	Last *lastDelivery `json:"last,omitempty"`
}

type webhookList struct {
	Webhooks []webhook `json:"webhooks"`
	// Kinds are the kinds of event a webhook may ask for.
	Kinds []string `json:"kinds"`
}

type webhookRequest struct {
	Name    string   `json:"name"`
	URL     string   `json:"url" doc:"An http or https address."`
	Kinds   []string `json:"kinds" doc:"The kinds of event to send; none is every kind."`
	Secret  *string  `json:"secret,omitempty" doc:"Signs each delivery (16 to 200 characters). Required to make a webhook; left out when changing one it keeps the secret."`
	Trusted bool     `json:"trusted"`
	Enabled bool     `json:"enabled"`
}

type webhookDelivery struct {
	ID           uuid.UUID       `json:"id"`
	Kind         string          `json:"kind"`
	Status       string          `json:"status" enum:"pending,delivered,failed"`
	Attempts     int             `json:"attempts"`
	LastError    string          `json:"lastError,omitempty"`
	ResponseCode *int            `json:"responseCode,omitempty"`
	CreatedAt    time.Time       `json:"createdAt"`
	DeliveredAt  *time.Time      `json:"deliveredAt,omitempty"`
	Payload      json.RawMessage `json:"payload" doc:"The body that was sent."`
}

type webhookDeliveryList struct {
	Deliveries []webhookDelivery `json:"deliveries"`
}

func webhookOf(h webhooks.Hook, last map[uuid.UUID]webhooks.LastDelivery) webhook {
	out := webhook{ID: h.ID, Name: h.Name, URL: h.URL, Kinds: h.Kinds, SecretSet: true,
		Trusted: h.Trusted, Enabled: h.Enabled, CreatedAt: h.CreatedAt}
	if out.Kinds == nil {
		out.Kinds = []string{}
	}
	if l, ok := last[h.ID]; ok {
		out.Last = &lastDelivery{Status: l.Status, At: l.At, ResponseCode: l.ResponseCode}
	}
	return out
}

func webhookError(err error) error {
	var field *webhooks.FieldError
	switch {
	case errors.As(err, &field):
		return ErrValidation(map[string]string{field.Field: field.Message})
	case errors.Is(err, webhooks.ErrNotFound):
		return ErrNotFound("There is no such webhook.")
	case errors.Is(err, webhooks.ErrTooMany):
		return ErrConflict("There are as many webhooks as there may be.")
	}
	return err
}

func (r webhookRequest) input() webhooks.Input {
	return webhooks.Input{Name: r.Name, URL: r.URL, Kinds: r.Kinds, Secret: r.Secret, Trusted: r.Trusted, Enabled: r.Enabled}
}

func (s *Server) listWebhooks(w http.ResponseWriter, r *http.Request) error {
	hooks, err := s.Webhooks.List(r.Context())
	if err != nil {
		return err
	}
	last, err := s.Webhooks.Last(r.Context())
	if err != nil {
		return err
	}
	out := webhookList{Webhooks: make([]webhook, len(hooks)), Kinds: make([]string, len(notify.EventKinds))}
	for i, h := range hooks {
		out.Webhooks[i] = webhookOf(h, last)
	}
	for i, k := range notify.EventKinds {
		out.Kinds[i] = k.Kind
	}
	writeJSON(w, r, http.StatusOK, out)
	return nil
}

func (s *Server) createWebhook(w http.ResponseWriter, r *http.Request) error {
	var req webhookRequest
	if err := decodeJSON(w, r, &req); err != nil {
		return err
	}
	h, err := s.Webhooks.Create(r.Context(), UserFrom(r.Context()).ID, req.input())
	if err != nil {
		return webhookError(err)
	}
	writeJSON(w, r, http.StatusCreated, webhookOf(h, nil))
	return nil
}

func (s *Server) updateWebhook(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r, "webhookId", "webhook")
	if err != nil {
		return err
	}
	var req webhookRequest
	if err := decodeJSON(w, r, &req); err != nil {
		return err
	}
	h, err := s.Webhooks.Update(r.Context(), id, req.input())
	if err != nil {
		return webhookError(err)
	}
	last, err := s.Webhooks.Last(r.Context())
	if err != nil {
		return err
	}
	writeJSON(w, r, http.StatusOK, webhookOf(h, last))
	return nil
}

func (s *Server) deleteWebhook(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r, "webhookId", "webhook")
	if err != nil {
		return err
	}
	if err := s.Webhooks.Delete(r.Context(), id); err != nil {
		return webhookError(err)
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

func (s *Server) testWebhook(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r, "webhookId", "webhook")
	if err != nil {
		return err
	}
	if err := s.Webhooks.SendTest(r.Context(), id); err != nil {
		return webhookError(err)
	}
	w.WriteHeader(http.StatusAccepted)
	return nil
}

func (s *Server) listWebhookDeliveries(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r, "webhookId", "webhook")
	if err != nil {
		return err
	}
	list, err := s.Webhooks.Deliveries(r.Context(), id, 100)
	if err != nil {
		return webhookError(err)
	}
	out := webhookDeliveryList{Deliveries: make([]webhookDelivery, len(list))}
	for i, d := range list {
		out.Deliveries[i] = webhookDelivery{ID: d.ID, Kind: d.Kind, Status: d.Status, Attempts: d.Attempts,
			LastError: d.LastError, ResponseCode: d.ResponseCode, CreatedAt: d.CreatedAt, DeliveredAt: d.DeliveredAt, Payload: d.Payload}
	}
	writeJSON(w, r, http.StatusOK, out)
	return nil
}

// getMetrics answers in Prometheus's text format. Switched off, it does not
// exist, to everyone.
func (s *Server) getMetrics(w http.ResponseWriter, r *http.Request) error {
	on, err := s.Settings.Text(r.Context(), settings.MetricsEnabled)
	if err != nil {
		return err
	}
	if on != "on" || s.Metrics == nil {
		return ErrNotFound("")
	}
	var buf bytes.Buffer
	if err := s.Metrics.Write(r.Context(), &buf); err != nil {
		return err
	}
	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(buf.Bytes())
	return nil
}

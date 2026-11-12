package httpapi

import (
	"errors"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/praetorianer777/gotome/backend/internal/auth"
)

type apiKeyView struct {
	ID         uuid.UUID  `json:"id"`
	Name       string     `json:"name"`
	Prefix     string     `json:"prefix" doc:"The key's first characters, to tell keys apart."`
	Scopes     []string   `json:"scopes" doc:"What the key may do: read, progress, upload, edit, admin, metrics."`
	CreatedAt  time.Time  `json:"createdAt"`
	LastUsedAt *time.Time `json:"lastUsedAt,omitempty"`
	ExpiresAt  *time.Time `json:"expiresAt,omitempty"`
}

type apiKeyList struct {
	Keys []apiKeyView `json:"keys"`
}

type createAPIKeyRequest struct {
	Name      string     `json:"name"`
	Scopes    []string   `json:"scopes"`
	ExpiresAt *time.Time `json:"expiresAt,omitempty"`
}

type createdAPIKey struct {
	Key apiKeyView `json:"key"`
	// Token is the key itself, sent as "Authorization: Bearer <token>". It is
	// never shown again.
	Token string `json:"token"`
}

func apiKeyViewOf(k auth.APIKey) apiKeyView {
	return apiKeyView{
		ID: k.ID, Name: k.Name, Prefix: k.Prefix, Scopes: k.Scopes,
		CreatedAt: k.CreatedAt, LastUsedAt: k.LastUsedAt, ExpiresAt: k.ExpiresAt,
	}
}

func (s *Server) listOwnAPIKeys(w http.ResponseWriter, r *http.Request) error {
	keys, err := s.Auth.APIKeys(r.Context(), UserFrom(r.Context()).ID)
	if err != nil {
		return err
	}
	out := apiKeyList{Keys: make([]apiKeyView, len(keys))}
	for i, k := range keys {
		out.Keys[i] = apiKeyViewOf(k)
	}
	writeJSON(w, r, http.StatusOK, out)
	return nil
}

func (s *Server) createOwnAPIKey(w http.ResponseWriter, r *http.Request) error {
	var req createAPIKeyRequest
	if err := decodeJSON(w, r, &req); err != nil {
		return err
	}
	key, token, err := s.Auth.CreateAPIKey(r.Context(), *UserFrom(r.Context()), req.Name, req.Scopes, req.ExpiresAt)
	var invalid *auth.ValidationError
	if errors.As(err, &invalid) {
		return ErrValidation(invalid.Fields)
	}
	if err != nil {
		return err
	}
	writeJSON(w, r, http.StatusCreated, createdAPIKey{Key: apiKeyViewOf(key), Token: token})
	return nil
}

func (s *Server) revokeOwnAPIKey(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r, "keyId", "API key")
	if err != nil {
		return err
	}
	err = s.Auth.DeleteAPIKey(r.Context(), UserFrom(r.Context()).ID, id)
	if errors.Is(err, auth.ErrNoAPIKey) {
		return ErrNotFound("There is no such API key.")
	}
	if err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

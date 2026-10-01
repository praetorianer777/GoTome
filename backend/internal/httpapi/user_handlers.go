package httpapi

import (
	"errors"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/praetorianer777/gotome/backend/internal/auth"
)

// account is a user as administration sees one.
type account struct {
	auth.User
	Disabled  bool      `json:"disabled"`
	CreatedAt time.Time `json:"createdAt"`
	// LastSeenAt is when one of the account's sessions was last used.
	LastSeenAt *time.Time `json:"lastSeenAt,omitempty"`
	// Sessions is how many live sessions the account has.
	Sessions int `json:"sessions"`
	// QuotaBytes is how much the account may upload; left out for no limit.
	QuotaBytes *int64 `json:"quotaBytes,omitempty"`
	// UsedBytes is what its uploads take up.
	UsedBytes int64 `json:"usedBytes"`
}

// storage is what the caller's uploads take up and may.
type storage struct {
	UsedBytes int64 `json:"usedBytes"`
	// QuotaBytes is left out when there is no limit.
	QuotaBytes *int64 `json:"quotaBytes,omitempty"`
}

type setQuotaRequest struct {
	// QuotaBytes is the most the account may upload; null is no limit.
	QuotaBytes *int64 `json:"quotaBytes"`
}

type accountList struct {
	Users []account `json:"users"`
}

type createAccountRequest struct {
	Username string  `json:"username"`
	Password string  `json:"password"`
	Email    *string `json:"email,omitempty"`
	Role     string  `json:"role"`
}

// updateAccountRequest changes what it names; a field left out stays.
type updateAccountRequest struct {
	Role *string `json:"role,omitempty"`
	// Email is the new address; "" removes it.
	Email    *string `json:"email,omitempty"`
	Disabled *bool   `json:"disabled,omitempty"`
	// Password replaces the user's own, and signs them out everywhere.
	Password *string `json:"password,omitempty"`
}

type sessionView struct {
	ID         uuid.UUID `json:"id"`
	UserAgent  string    `json:"userAgent"`
	CreatedAt  time.Time `json:"createdAt"`
	LastSeenAt time.Time `json:"lastSeenAt"`
	// Current is the session this request came with.
	Current bool `json:"current"`
}

type sessionList struct {
	Sessions []sessionView `json:"sessions"`
}

type changePasswordRequest struct {
	CurrentPassword string `json:"currentPassword"`
	NewPassword     string `json:"newPassword"`
}

func toAccountView(a auth.Account) account {
	return account{
		User: a.User, Disabled: a.Disabled, CreatedAt: a.CreatedAt, LastSeenAt: a.LastSeenAt,
		Sessions: a.Sessions, QuotaBytes: a.QuotaBytes, UsedBytes: a.UsedBytes,
	}
}

// accountError turns what the account functions refuse into answers.
func accountError(err error) error {
	var invalid *auth.ValidationError
	switch {
	case errors.As(err, &invalid):
		return ErrValidation(invalid.Fields)
	case errors.Is(err, auth.ErrTaken):
		return ErrConflict("That user name or e-mail address is already in use.")
	case errors.Is(err, auth.ErrNoUser):
		return ErrNotFound("There is no such user.")
	case errors.Is(err, auth.ErrLastAdmin):
		return ErrConflict("This is the last administrator who can sign in. Make another account an administrator first.")
	}
	return err
}

func (s *Server) listUsers(w http.ResponseWriter, r *http.Request) error {
	accounts, err := s.Auth.Accounts(r.Context())
	if err != nil {
		return err
	}
	out := accountList{Users: make([]account, len(accounts))}
	for i, a := range accounts {
		out.Users[i] = toAccountView(a)
	}
	writeJSON(w, r, http.StatusOK, out)
	return nil
}

func (s *Server) createUser(w http.ResponseWriter, r *http.Request) error {
	var req createAccountRequest
	if err := decodeJSON(w, r, &req); err != nil {
		return err
	}
	a, err := s.Auth.CreateAccount(r.Context(), auth.NewAccount{
		Username: req.Username, Password: req.Password, Email: req.Email, Role: req.Role,
	})
	if err != nil {
		return accountError(err)
	}
	writeJSON(w, r, http.StatusCreated, toAccountView(a))
	return nil
}

func (s *Server) updateUser(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r, "userId", "user")
	if err != nil {
		return err
	}
	var req updateAccountRequest
	if err := decodeJSON(w, r, &req); err != nil {
		return err
	}
	a, err := s.Auth.UpdateAccount(r.Context(), id, auth.AccountChanges{
		Role: req.Role, Email: req.Email, Disabled: req.Disabled, Password: req.Password,
	})
	if err != nil {
		return accountError(err)
	}
	writeJSON(w, r, http.StatusOK, toAccountView(a))
	return nil
}

func (s *Server) endUserSessions(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r, "userId", "user")
	if err != nil {
		return err
	}
	if err := s.Auth.EndSessions(r.Context(), id); err != nil {
		return accountError(err)
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

func (s *Server) listOwnSessions(w http.ResponseWriter, r *http.Request) error {
	sessions, err := s.Auth.Sessions(r.Context(), UserFrom(r.Context()).ID, sessionToken(r))
	if err != nil {
		return err
	}
	out := sessionList{Sessions: make([]sessionView, len(sessions))}
	for i, ss := range sessions {
		out.Sessions[i] = sessionView{ID: ss.ID, UserAgent: ss.UserAgent, CreatedAt: ss.CreatedAt, LastSeenAt: ss.LastSeenAt, Current: ss.Current}
	}
	writeJSON(w, r, http.StatusOK, out)
	return nil
}

func (s *Server) endOwnSession(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r, "sessionId", "session")
	if err != nil {
		return err
	}
	err = s.Auth.EndSession(r.Context(), UserFrom(r.Context()).ID, id)
	if errors.Is(err, auth.ErrNoSession) {
		return ErrNotFound("There is no such session.")
	}
	if err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

func (s *Server) changePassword(w http.ResponseWriter, r *http.Request) error {
	var req changePasswordRequest
	if err := decodeJSON(w, r, &req); err != nil {
		return err
	}
	err := s.Auth.ChangePassword(r.Context(), UserFrom(r.Context()).ID, sessionToken(r), req.CurrentPassword, req.NewPassword)
	if errors.Is(err, auth.ErrWrongPassword) {
		return ErrValidation(map[string]string{"currentPassword": "That is not your current password."})
	}
	if err != nil {
		return accountError(err)
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

func (s *Server) setQuota(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r, "userId", "user")
	if err != nil {
		return err
	}
	var req setQuotaRequest
	if err := decodeJSON(w, r, &req); err != nil {
		return err
	}
	a, err := s.Auth.SetQuota(r.Context(), id, req.QuotaBytes)
	if err != nil {
		return accountError(err)
	}
	st, err := s.Scans.Storage(r.Context(), id)
	if err != nil {
		return err
	}
	a.UsedBytes = st.UsedBytes
	writeJSON(w, r, http.StatusOK, toAccountView(a))
	return nil
}

func (s *Server) getOwnStorage(w http.ResponseWriter, r *http.Request) error {
	st, err := s.Scans.Storage(r.Context(), UserFrom(r.Context()).ID)
	if err != nil {
		return err
	}
	writeJSON(w, r, http.StatusOK, storage{UsedBytes: st.UsedBytes, QuotaBytes: st.QuotaBytes})
	return nil
}

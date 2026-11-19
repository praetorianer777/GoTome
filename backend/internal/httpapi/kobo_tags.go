package httpapi

import (
	"context"
	"errors"
	"net/http"
	"slices"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/praetorianer777/gotome/backend/internal/auth"
	"github.com/praetorianer777/gotome/backend/internal/kobo"
	"github.com/praetorianer777/gotome/backend/internal/library"
	"github.com/praetorianer777/gotome/backend/internal/shelves"
)

const koboItemType = "ProductRevisionTagItem"

// koboTag is a collection as Kobo's store words it.
type koboTag struct {
	Created      string        `json:"Created"`
	ID           uuid.UUID     `json:"Id"`
	Items        []koboTagItem `json:"Items"`
	LastModified string        `json:"LastModified"`
	Name         string        `json:"Name"`
	Type         string        `json:"Type"`
}

type koboTagItem struct {
	RevisionID string `json:"RevisionId"`
	Type       string `json:"Type"`
}

type koboTagIn struct {
	Name  string        `json:"Name"`
	Items []koboTagItem `json:"Items"`
}

func koboTagOf(t kobo.Tag) koboTag {
	items := make([]koboTagItem, len(t.Books))
	for i, b := range t.Books {
		items[i] = koboTagItem{RevisionID: b.String(), Type: koboItemType}
	}
	return koboTag{
		Created: t.Created.UTC().Format(koboTime), ID: t.ID, Items: items,
		LastModified: t.Updated.UTC().Format(koboTime), Name: t.Name, Type: "UserTag",
	}
}

// koboTagBooks are the ids of the books a request names.
func koboTagBooks(items []koboTagItem) []uuid.UUID {
	var ids []uuid.UUID
	for _, it := range items {
		if it.Type != koboItemType {
			continue
		}
		if id, err := uuid.Parse(it.RevisionID); err == nil && !slices.Contains(ids, id) {
			ids = append(ids, id)
		}
	}
	return ids
}

func onDevice(books, held []uuid.UUID) []uuid.UUID {
	out := []uuid.UUID{}
	for _, b := range books {
		if slices.Contains(held, b) {
			out = append(out, b)
		}
	}
	return out
}

// koboWanted are the tags a reader should hold: the person's collections
// and the shared ones of others, and their smart shelves and others'
// shared ones, each with the books on it that the reader holds. Books come
// through the scope, so nothing of a library the person no longer sees
// reaches the reader.
func (s *Server) koboWanted(ctx context.Context, scope library.Scope, device uuid.UUID) ([]kobo.Tag, error) {
	held, err := s.Kobo.OnDevice(ctx, device)
	if err != nil {
		return nil, err
	}
	collections, err := s.Shelves.List(ctx, scope, nil)
	if err != nil {
		return nil, err
	}
	var out []kobo.Tag
	for _, c := range collections {
		_, books, err := s.Shelves.Get(ctx, scope, c.ID)
		if errors.Is(err, shelves.ErrNotFound) {
			continue
		}
		if err != nil {
			return nil, err
		}
		out = append(out, kobo.Tag{ID: c.ID, Name: koboTagName(c.Name, c.OwnerID, c.OwnerName, scope), Created: c.CreatedAt, Updated: c.UpdatedAt, Books: onDevice(books, held)})
	}
	smart, err := s.Shelves.SmartShelves(ctx, scope)
	if err != nil {
		return nil, err
	}
	for _, sh := range smart {
		books := []uuid.UUID{}
		if len(held) > 0 {
			if books, err = s.Books.Select(ctx, scope, nil, sh.Filter, held); err != nil {
				loggerFrom(ctx).Error("selecting a smart shelf for a Kobo reader", "error", err)
				books = []uuid.UUID{}
			}
		}
		out = append(out, kobo.Tag{ID: sh.ID, Name: koboTagName(sh.Name, sh.OwnerID, sh.OwnerName, scope), Created: sh.UpdatedAt, Updated: sh.UpdatedAt, Books: books})
	}
	return out, nil
}

func koboTagName(name string, owner uuid.UUID, ownerName string, scope library.Scope) string {
	if owner == scope.Viewer {
		return name
	}
	return name + " (" + ownerName + ")"
}

func (s *Server) koboTags(w http.ResponseWriter, r *http.Request) error {
	device := DeviceFrom(r.Context())
	if device == uuid.Nil {
		return errNotADevice()
	}
	tags, err := s.koboWanted(r.Context(), library.ScopeOf(*UserFrom(r.Context())), device)
	if err != nil {
		return err
	}
	out := make([]koboTag, len(tags))
	for i, t := range tags {
		out[i] = koboTagOf(t)
	}
	writeJSON(w, r, http.StatusOK, out)
	return nil
}

// koboTagSync adds the changes of tags to a sync's answer: the Tag entries
// of Kobo's library sync.
func (s *Server) koboTagSync(ctx context.Context, scope library.Scope, device uuid.UUID, now string) ([]koboSyncItem, error) {
	wanted, err := s.koboWanted(ctx, scope, device)
	if err != nil {
		return nil, err
	}
	changes, err := s.Kobo.SyncTags(ctx, device, wanted)
	if err != nil {
		return nil, err
	}
	var out []koboSyncItem
	for _, c := range changes {
		switch {
		case c.Deleted:
			out = append(out, koboSyncItem{DeletedTag: &koboTagChange{Tag: koboTag{ID: c.Tag.ID, LastModified: now}}})
		case c.New:
			out = append(out, koboSyncItem{NewTag: &koboTagChange{Tag: koboTagOf(c.Tag)}})
		default:
			out = append(out, koboSyncItem{ChangedTag: &koboTagChange{Tag: koboTagOf(c.Tag)}})
		}
	}
	return out, nil
}

type koboTagChange struct {
	Tag koboTag `json:"Tag"`
}

// koboOwnTag finds the tag a request names. It is changed only when it is
// the person's own collection: a shared collection of someone else's and a
// smart shelf go to the reader one way, so a change the reader made to one
// is refused and the reader is told of the tag again.
func (s *Server) koboOwnTag(r *http.Request) (uuid.UUID, shelves.Collection, error) {
	device := DeviceFrom(r.Context())
	if device == uuid.Nil {
		return uuid.Nil, shelves.Collection{}, errNotADevice()
	}
	id, err := uuid.Parse(chi.URLParam(r, "tagId"))
	if err != nil {
		return uuid.Nil, shelves.Collection{}, ErrNotFound("There is no such collection.")
	}
	scope := library.ScopeOf(*UserFrom(r.Context()))
	c, _, err := s.Shelves.Get(r.Context(), scope, id)
	if errors.Is(err, shelves.ErrNotFound) {
		if _, err := s.Shelves.SmartShelf(r.Context(), scope, id); err == nil {
			_ = s.Kobo.ForgetTag(r.Context(), device, id)
			return id, shelves.Collection{}, ErrForbidden("A smart shelf is only read on the device.")
		}
		return id, shelves.Collection{}, ErrNotFound("There is no such collection.")
	}
	if err != nil {
		return id, c, err
	}
	if c.OwnerID != scope.Viewer {
		_ = s.Kobo.ForgetTag(r.Context(), device, id)
		return id, c, ErrForbidden("Only its owner changes a collection.")
	}
	return id, c, nil
}

// koboRecordTag notes what the reader holds of a collection now.
func (s *Server) koboRecordTag(r *http.Request, id uuid.UUID) error {
	device := DeviceFrom(r.Context())
	c, books, err := s.Shelves.Get(r.Context(), library.ScopeOf(*UserFrom(r.Context())), id)
	if err != nil {
		return collectionError(err)
	}
	held, err := s.Kobo.OnDevice(r.Context(), device)
	if err != nil {
		return err
	}
	return s.Kobo.RecordTag(r.Context(), device, kobo.Tag{ID: id, Name: c.Name, Books: onDevice(books, held)})
}

func (s *Server) koboTagCreate(w http.ResponseWriter, r *http.Request) error {
	device := DeviceFrom(r.Context())
	if device == uuid.Nil {
		return errNotADevice()
	}
	var req koboTagIn
	if err := decodeKobo(w, r, &req); err != nil {
		return err
	}
	scope := library.ScopeOf(*UserFrom(r.Context()))
	id, err := s.Shelves.Create(r.Context(), scope, shelves.Details{Name: req.Name})
	if err != nil {
		return collectionError(err)
	}
	if books := koboTagBooks(req.Items); len(books) > 0 {
		if _, err := s.Shelves.Add(r.Context(), scope, id, books); err != nil {
			return collectionError(err)
		}
	}
	if err := s.koboRecordTag(r, id); err != nil {
		return err
	}
	writeJSON(w, r, http.StatusCreated, id.String())
	return nil
}

func (s *Server) koboTagUpdate(w http.ResponseWriter, r *http.Request) error {
	id, c, err := s.koboOwnTag(r)
	if err != nil {
		return err
	}
	var req koboTagIn
	if err := decodeKobo(w, r, &req); err != nil {
		return err
	}
	scope := library.ScopeOf(*UserFrom(r.Context()))
	if err := s.Shelves.Update(r.Context(), scope, id, shelves.Details{Name: req.Name, Description: c.Description, Visibility: c.Visibility}); err != nil {
		return collectionError(err)
	}
	if err := s.koboRecordTag(r, id); err != nil {
		return err
	}
	w.WriteHeader(http.StatusOK)
	return nil
}

func (s *Server) koboTagDelete(w http.ResponseWriter, r *http.Request) error {
	id, _, err := s.koboOwnTag(r)
	if err != nil {
		return err
	}
	if err := s.Shelves.Delete(r.Context(), library.ScopeOf(*UserFrom(r.Context())), id); err != nil {
		return collectionError(err)
	}
	if err := s.Kobo.ForgetTag(r.Context(), DeviceFrom(r.Context()), id); err != nil {
		return err
	}
	w.WriteHeader(http.StatusOK)
	return nil
}

func (s *Server) koboTagItems(remove bool) HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) error {
		id, _, err := s.koboOwnTag(r)
		if err != nil {
			return err
		}
		var req koboTagIn
		if err := decodeKobo(w, r, &req); err != nil {
			return err
		}
		scope := library.ScopeOf(*UserFrom(r.Context()))
		books := koboTagBooks(req.Items)
		if remove {
			for _, b := range books {
				if err := s.Shelves.Remove(r.Context(), scope, id, b); err != nil && !errors.Is(err, shelves.ErrNotFound) {
					return collectionError(err)
				}
			}
		} else if len(books) > 0 {
			if _, err := s.Shelves.Add(r.Context(), scope, id, books); err != nil {
				return collectionError(err)
			}
		}
		if err := s.koboRecordTag(r, id); err != nil {
			return err
		}
		if remove {
			w.WriteHeader(http.StatusOK)
		} else {
			w.WriteHeader(http.StatusCreated)
		}
		return nil
	}
}

func (s *Server) koboTagRoutes() []Route {
	const tag = "kobo"
	route := func(method, path, id, summary string, status int, h HandlerFunc) Route {
		return Route{
			Method: method, Path: "/kobo/{token}" + path, ID: id, Summary: summary, Tag: tag,
			Status: status, Permission: auth.PersonalManage, Devices: true, Handler: h,
		}
	}
	return []Route{
		route(http.MethodPost, "/v1/library/tags", "koboTagCreate", "A Kobo reader makes a collection: a private one of the person's, with the books named", http.StatusCreated, s.koboTagCreate),
		route(http.MethodPut, "/v1/library/tags/{tagId}", "koboTagUpdate", "A Kobo reader renames one of the person's collections; another's and smart shelves are refused", http.StatusOK, s.koboTagUpdate),
		route(http.MethodDelete, "/v1/library/tags/{tagId}", "koboTagDelete", "A Kobo reader deletes one of the person's collections; another's and smart shelves are refused", http.StatusOK, s.koboTagDelete),
		route(http.MethodPost, "/v1/library/tags/{tagId}/items", "koboTagAddItems", "A Kobo reader puts books on one of the person's collections", http.StatusCreated, s.koboTagItems(false)),
		route(http.MethodPost, "/v1/library/tags/{tagId}/items/delete", "koboTagRemoveItems", "A Kobo reader takes books off one of the person's collections", http.StatusOK, s.koboTagItems(true)),
	}
}

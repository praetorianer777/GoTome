package httpapi

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/praetorianer777/gotome/backend/internal/catalog"
	"github.com/praetorianer777/gotome/backend/internal/ingest"
	"github.com/praetorianer777/gotome/backend/internal/library"
)

// uploadForm is the multipart form an upload is sent as.
type uploadForm struct {
	// File is the book's file, named as it was on the uploader's computer:
	// the name's extension says what format it is.
	File string `json:"file"`
}

// uploadFile stores the one file of a multipart form. The file is read as it
// arrives, never held in memory whole.
func (s *Server) uploadFile(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r, "libraryId", "library")
	if err != nil {
		return err
	}
	user := UserFrom(r.Context())
	scope := library.ScopeOf(*user)
	lib, err := s.Libraries.Get(r.Context(), scope, id)
	if errors.Is(err, library.ErrNotFound) {
		return ErrNotFound("There is no such library.")
	}
	if err != nil {
		return err
	}
	if lib.Mode != library.ModeManaged {
		return ErrConflict("Books can only be uploaded into a managed library. Put files for an external library into its folder, and scan it.")
	}

	// Refused before a byte of the file is read, when the length the request
	// announces cannot fit. The form around the file is a few hundred bytes
	// of that length; Upload checks again with the file's own size.
	if r.ContentLength > multipartSlack {
		if err := s.Scans.CheckRoom(r.Context(), user.ID, r.ContentLength-multipartSlack); err != nil {
			return uploadError(err)
		}
	}

	mr, err := r.MultipartReader()
	if err != nil {
		return ErrBadRequest("Send the file as multipart/form-data, in a field named file.")
	}
	part, err := mr.NextPart()
	if err != nil || part.FormName() != "file" || part.FileName() == "" {
		return ErrBadRequest("Send the file as multipart/form-data, in a field named file.")
	}
	defer part.Close()

	// An audiobook takes longer to arrive than the server's deadlines allow
	// any other request.
	rc := http.NewResponseController(w)
	_ = rc.SetReadDeadline(time.Time{})
	_ = rc.SetWriteDeadline(time.Time{})

	res, err := s.Scans.Upload(r.Context(), lib, scope, part.FileName(), part)
	var over *ingest.QuotaError
	switch {
	case errors.As(err, &over):
		return uploadError(err)
	case errors.Is(err, ingest.ErrUnknownFormat):
		return ErrValidation(map[string]string{"file": "GOtome does not read this kind of file. It takes " + knownFormats() + "."})
	case errors.Is(err, ingest.ErrUploadTooLarge):
		return &APIError{
			Status: http.StatusRequestEntityTooLarge, Code: "too_large",
			Message: fmt.Sprintf("The file is larger than the %d MiB an upload may be.", s.Scans.UploadLimit>>20),
		}
	case err != nil && (r.Context().Err() != nil || errors.Is(err, io.ErrUnexpectedEOF)):
		return ErrBadRequest("The upload was cut off before the whole file arrived.")
	case err != nil:
		return err
	}
	writeJSON(w, r, http.StatusOK, res)
	return nil
}

// multipartSlack is more than the form around an uploaded file takes.
const multipartSlack = 1 << 10

// uploadError answers an upload that does not fit the uploader's quota.
func uploadError(err error) error {
	var over *ingest.QuotaError
	if !errors.As(err, &over) {
		return err
	}
	return &APIError{
		Status: http.StatusRequestEntityTooLarge, Code: "over_quota",
		Message: fmt.Sprintf("This file does not fit into your storage: your uploads take up %s of the %s you may use.",
			humanBytes(over.UsedBytes), humanBytes(*over.QuotaBytes)),
	}
}

// humanBytes is a size as people read it: 1.4 GB.
func humanBytes(n int64) string {
	const units = "kMGTPE"
	if n < 1000 {
		return fmt.Sprintf("%d bytes", n)
	}
	v, i := float64(n)/1000, 0
	for v >= 1000 && i < len(units)-1 {
		v /= 1000
		i++
	}
	if units[i] == 'k' {
		return fmt.Sprintf("%.1f kB", v)
	}
	return fmt.Sprintf("%.1f %cB", v, units[i])
}

func knownFormats() string {
	formats := catalog.Formats()
	for i, f := range formats {
		formats[i] = strings.ToUpper(f)
	}
	slices.Sort(formats)
	return strings.Join(formats[:len(formats)-1], ", ") + " and " + formats[len(formats)-1]
}

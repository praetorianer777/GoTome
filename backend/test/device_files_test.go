//go:build integration

package test

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"strings"
	"testing"

	"github.com/praetorianer777/gotome/backend/internal/httpapi"
)

func hexSum(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func (a *app) deviceUpload(c *http.Client, name string, content []byte) (int, map[string]any) {
	a.t.Helper()
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	w, err := form.CreateFormFile("file", name)
	if err != nil {
		a.t.Fatal(err)
	}
	_, _ = w.Write(content)
	_ = form.Close()
	req, _ := http.NewRequest(http.MethodPost, a.url+httpapi.APIPrefix+"/device/uploads", &body)
	req.Header.Set("Content-Type", form.FormDataContentType())
	resp, err := c.Do(req)
	if err != nil {
		a.t.Fatal(err)
	}
	defer resp.Body.Close()
	var out map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return resp.StatusCode, out
}

func (a *app) newDevice(c *http.Client, user string, canUpload bool) (string, *http.Client) {
	a.t.Helper()
	status, made, _ := a.call(c, http.MethodPost, "/auth/devices", map[string]any{"name": "Tolino", "canUpload": canUpload})
	if status != 201 {
		a.t.Fatalf("add a device: %d %v", status, made)
	}
	return made["device"].(map[string]any)["id"].(string), deviceClient(user, made["token"].(string))
}

func TestADevicesFilesAreMatchedToTheBooksItsOwnerSees(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	admin, _ := a.signedIn("admin", "admin")
	reader, readerID := a.signedIn("reader", "reader")
	vault := a.library(admin, "Vault", "private")
	shelf := a.library(admin, "Shelf", "shared")

	open := fakePDF(80, 'o')
	secret := fakePDF(90, 's')
	_, upOpen := a.upload(admin, shelf, "Open Waves.pdf", open)
	_, upSecret := a.upload(admin, vault, "Secret Waves.pdf", secret)
	if upOpen["bookId"] == nil || upSecret["bookId"] == nil {
		t.Fatalf("uploads: %v %v", upOpen, upSecret)
	}
	_, device := a.newDevice(reader, "reader", false)

	match := func(c *http.Client, hashes ...string) (int, map[string]any) {
		status, out, _ := a.call(c, http.MethodPost, "/device/matches", map[string]any{"hashes": hashes})
		return status, out
	}
	unknown := hexSum([]byte("nothing like it"))
	status, out := match(device, hexSum(open), hexSum(secret), unknown)
	matches, _ := out["matches"].([]any)
	if status != 200 || len(matches) != 1 ||
		matches[0].(map[string]any)["bookId"] != upOpen["bookId"] || matches[0].(map[string]any)["sha256"] != hexSum(open) || matches[0].(map[string]any)["fileId"] != upOpen["fileId"] {
		t.Fatalf("matches: %d %v", status, out)
	}

	// The answer about a hash of a book the caller cannot see is the answer
	// about one nobody has.
	_, hidden := match(device, hexSum(secret))
	_, nothing := match(device, unknown)
	if h, n := mustJSON(hidden), mustJSON(nothing); h != n {
		t.Errorf("a hidden book answers %s, an unknown hash %s", h, n)
	}
	if _, onDevice, _ := a.call(reader, http.MethodGet, "/books/"+upSecret["bookId"].(string)+"/deliveries", nil); strings.Contains(mustJSON(onDevice), "Tolino") {
		t.Errorf("the hidden book is on the device: %v", onDevice)
	}

	// A match counts as on the device, and the book was not sent before.
	_, deliveries, _ := a.call(reader, http.MethodGet, "/books/"+upOpen["bookId"].(string)+"/deliveries", nil)
	list := deliveries["deliveries"].([]any)
	if len(list) != 1 || list[0].(map[string]any)["deliveredAt"] == nil {
		t.Errorf("the matched book on the device: %v", deliveries)
	}

	// Once the reader may see the private library, the same hash matches.
	if status, _, _ := a.call(admin, http.MethodPut, "/libraries/"+vault+"/members/"+readerID, nil); status != 204 {
		t.Fatalf("membership: %d", status)
	}
	if _, out := match(device, hexSum(secret)); len(out["matches"].([]any)) != 1 {
		t.Errorf("after joining: %v", out)
	}

	if status, _ := match(device, "xyz"); status != 422 {
		t.Errorf("a malformed hash: %d", status)
	}
	many := make([]string, 501)
	for i := range many {
		many[i] = unknown
	}
	if status, _ := match(device, many...); status != 400 {
		t.Errorf("too many hashes: %d", status)
	}
	if status, _ := match(reader, hexSum(open)); status != 404 {
		t.Errorf("a browser asking: %d", status)
	}
}

func TestAnUploadFromADeviceNeedsThePermissionTheRoleAndRoom(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	admin, _ := a.signedIn("admin", "admin")
	editor, editorID := a.signedIn("editor", "editor")
	reader, _ := a.signedIn("reader", "reader")

	upload := func(c *http.Client) (int, map[string]any) {
		return a.deviceUpload(c, "Tidal Notes.pdf", fakePDF(60, 't'))
	}
	state := func(c *http.Client) map[string]any {
		_, out, _ := a.call(c, http.MethodGet, "/device", nil)
		return out["upload"].(map[string]any)
	}

	_, plain := a.newDevice(editor, "editor", false)
	if status, body := upload(plain); status != 403 {
		t.Errorf("a token not allowed to upload: %d %v", status, body)
	}
	if st := state(plain); st["allowed"] != false || st["reason"] != "token" {
		t.Errorf("its state: %v", st)
	}

	_, allowed := a.newDevice(editor, "editor", true)
	if status, body := upload(allowed); status != 409 {
		t.Errorf("with no managed library: %d %v", status, body)
	}
	if st := state(allowed); st["reason"] != "library" {
		t.Errorf("its state without a library: %v", st)
	}

	lib := a.library(admin, "Shelf", "shared")
	if st := state(allowed); st["allowed"] != true || st["library"] != "Shelf" {
		t.Errorf("its state: %v", st)
	}
	status, body := upload(allowed)
	if status != 200 || body["outcome"] != "added" || body["libraryId"] != lib {
		t.Fatalf("an upload: %d %v", status, body)
	}
	if status, _ := upload(allowed); status != 200 {
		t.Errorf("the same file again: %d", status)
	}
	if status, _, _ := a.call(allowed, http.MethodPut, "/device/books/"+body["bookId"].(string), nil); status != 204 {
		t.Errorf("putting it on the device: %d", status)
	}

	_, readerDevice := a.newDevice(reader, "reader", true)
	if status, body := upload(readerDevice); status != 403 {
		t.Errorf("an account that may not upload: %d %v", status, body)
	}
	if st := state(readerDevice); st["reason"] != "role" {
		t.Errorf("its state: %v", st)
	}

	if status, _, _ := a.call(admin, http.MethodPut, "/users/"+editorID+"/quota", map[string]any{"quotaBytes": 100}); status != 200 {
		t.Fatalf("quota: %d", status)
	}
	status, body = a.deviceUpload(allowed, "Another Tide.pdf", fakePDF(60, 'a'))
	if status != 413 || errorCode(body) != "over_quota" {
		t.Errorf("over the quota: %d %v", status, body)
	}
	if status, _ := a.deviceUpload(allowed, "Large.pdf", fakePDF(5000, 'L')); status != 413 {
		t.Errorf("announced over the quota: %d", status)
	}
	if status, _, _ := a.call(editor, http.MethodPost, "/device/uploads", nil); status != 404 {
		t.Errorf("a browser session: %d", status)
	}
}

func TestAcceptingARequestMayAllowUploads(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	editor, _ := a.signedIn("editor", "editor")
	for _, allow := range []bool{false, true} {
		ticket := a.ask("editor", "Tolino")
		_, list, _ := a.call(editor, http.MethodGet, "/auth/device-requests", nil)
		id := list["requests"].([]any)[0].(map[string]any)["id"].(string)
		if status, _, _ := a.call(editor, http.MethodPost, "/auth/device-requests/"+id+"/accept", map[string]any{"canUpload": allow}); status != 204 {
			t.Fatalf("accept: %d", status)
		}
		_, answer := a.collect(ticket, ticket["secret"].(string))
		device := deviceClient("editor", answer["token"].(string))
		_, mine, _ := a.call(device, http.MethodGet, "/device", nil)
		if got := mine["device"].(map[string]any)["canUpload"]; got != allow {
			t.Errorf("accepted with canUpload=%v, the device says %v", allow, got)
		}
		_, again, _ := a.call(editor, http.MethodGet, "/auth/device-requests", nil)
		if len(again["requests"].([]any)) != 0 {
			t.Errorf("still waiting: %v", again)
		}
	}
}

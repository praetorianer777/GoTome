//go:build integration

package test

import (
	"context"
	"net/http"
	"reflect"
	"slices"
	"testing"
)

// ask requests, as a device's app, to be added to the user name's account.
func (a *app) ask(username, deviceName string) map[string]any {
	a.t.Helper()
	status, ticket, _ := a.call(http.DefaultClient, http.MethodPost, "/device/requests",
		map[string]any{"username": username, "deviceName": deviceName})
	if status != 201 {
		a.t.Fatalf("request: %d %v", status, ticket)
	}
	return ticket
}

// collect asks, as the app, about its request.
func (a *app) collect(ticket map[string]any, secret string) (int, map[string]any) {
	a.t.Helper()
	status, answer, _ := a.call(http.DefaultClient, http.MethodPost,
		"/device/requests/"+ticket["id"].(string)+"/token", map[string]any{"secret": secret})
	return status, answer
}

func TestADeviceIsAddedByAcceptingItsRequest(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	reader, _ := a.signedIn("reader", "reader")
	stranger, _ := a.signedIn("stranger", "reader")

	ticket := a.ask("READER", "Tolino Shine 4")
	if code := ticket["code"].(string); len(code) != 4 {
		t.Errorf("code %q", code)
	}
	secret := ticket["secret"].(string)
	if status, answer := a.collect(ticket, secret); status != 200 || answer["state"] != "pending" || answer["token"] != nil {
		t.Fatalf("before a decision: %d %v", status, answer)
	}
	if status, _ := a.collect(ticket, "not the secret"); status != 404 {
		t.Errorf("with a wrong secret: %d", status)
	}

	_, list, _ := a.call(reader, http.MethodGet, "/auth/device-requests", nil)
	requests := list["requests"].([]any)
	if len(requests) != 1 {
		t.Fatalf("waiting: %v", list)
	}
	waiting := requests[0].(map[string]any)
	if waiting["deviceName"] != "Tolino Shine 4" || waiting["code"] != ticket["code"] || waiting["address"] == "" || waiting["secret"] != nil {
		t.Errorf("waiting request: %v", waiting)
	}
	_, notes, _ := a.call(reader, http.MethodGet, "/notifications", nil)
	items := notes["notifications"].([]any)
	if len(items) != 1 || items[0].(map[string]any)["kind"] != "device.requested" || items[0].(map[string]any)["link"] != "/profile" {
		t.Errorf("notifications: %v", items)
	}

	path := "/auth/device-requests/" + waiting["id"].(string)
	if status, _, _ := a.call(stranger, http.MethodPost, path+"/accept", nil); status != 404 {
		t.Errorf("someone else accepts it: %d", status)
	}
	if _, list, _ := a.call(stranger, http.MethodGet, "/auth/device-requests", nil); len(list["requests"].([]any)) != 0 {
		t.Errorf("someone else sees it: %v", list)
	}
	if status, _, _ := a.call(reader, http.MethodPost, path+"/accept", nil); status != 204 {
		t.Fatalf("accept: %d", status)
	}
	if status, _, _ := a.call(reader, http.MethodPost, path+"/decline", nil); status != 404 {
		t.Errorf("declining once accepted: %d", status)
	}

	status, answer := a.collect(ticket, secret)
	if status != 200 || answer["state"] != "accepted" || answer["username"] != "reader" || answer["token"] == nil {
		t.Fatalf("collected: %d %v", status, answer)
	}
	device := deviceClient(answer["username"].(string), answer["token"].(string))
	if status, this, _ := a.call(device, http.MethodGet, "/device", nil); status != 200 || this["device"].(map[string]any)["name"] != "Tolino Shine 4" {
		t.Errorf("signing in with the token: %d %v", status, this)
	}
	if _, again := a.collect(ticket, secret); again["state"] != "expired" || again["token"] != nil {
		t.Errorf("collected twice: %v", again)
	}
	_, devices, _ := a.call(reader, http.MethodGet, "/auth/devices", nil)
	if n := len(devices["devices"].([]any)); n != 1 {
		t.Errorf("%d devices", n)
	}
}

func TestADeclinedOrExpiredRequestAddsNoDevice(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	reader, _ := a.signedIn("reader", "reader")

	declined := a.ask("reader", "Kobo")
	late := a.ask("reader", "Pocketbook")
	_, list, _ := a.call(reader, http.MethodGet, "/auth/device-requests", nil)
	for _, r := range list["requests"].([]any) {
		r := r.(map[string]any)
		if r["deviceName"] == "Kobo" {
			if status, _, _ := a.call(reader, http.MethodPost, "/auth/device-requests/"+r["id"].(string)+"/decline", nil); status != 204 {
				t.Fatalf("decline: %d", status)
			}
		}
	}
	if _, answer := a.collect(declined, declined["secret"].(string)); answer["state"] != "declined" || answer["token"] != nil {
		t.Errorf("declined: %v", answer)
	}

	if _, err := a.pool.Exec(context.Background(), "UPDATE device_requests SET expires_at = now() - interval '1 second' WHERE id = $1", late["id"]); err != nil {
		t.Fatal(err)
	}
	if _, list, _ := a.call(reader, http.MethodGet, "/auth/device-requests", nil); len(list["requests"].([]any)) != 0 {
		t.Errorf("still waiting: %v", list)
	}
	if status, _, _ := a.call(reader, http.MethodPost, "/auth/device-requests/"+late["id"].(string)+"/accept", nil); status != 404 {
		t.Errorf("accepting an expired request: %d", status)
	}
	if _, answer := a.collect(late, late["secret"].(string)); answer["state"] != "expired" {
		t.Errorf("expired: %v", answer)
	}
	if _, devices, _ := a.call(reader, http.MethodGet, "/auth/devices", nil); len(devices["devices"].([]any)) != 0 {
		t.Errorf("devices: %v", devices)
	}
}

// A request names no account it may not: the app is answered alike for a
// name that is nobody's, and an account takes only so many requests.
func TestARequestTellsNothingAboutTheAccount(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	reader, _ := a.signedIn("reader", "reader")

	keys := func(m map[string]any) []string {
		var out []string
		for k := range m {
			out = append(out, k)
		}
		slices.Sort(out)
		return out
	}
	real, nobody := a.ask("reader", "Tolino"), a.ask("nobody", "Tolino")
	if !reflect.DeepEqual(keys(real), keys(nobody)) {
		t.Errorf("answers differ: %v and %v", keys(real), keys(nobody))
	}
	if _, answer := a.collect(nobody, nobody["secret"].(string)); answer["state"] != "pending" {
		t.Errorf("nobody's request: %v", answer)
	}

	for range 5 {
		a.ask("reader", "Tolino")
	}
	if _, list, _ := a.call(reader, http.MethodGet, "/auth/device-requests", nil); len(list["requests"].([]any)) != 3 {
		t.Errorf("waiting: %d", len(list["requests"].([]any)))
	}

	status, out, header := a.call(http.DefaultClient, http.MethodPost, "/device/requests",
		map[string]any{"username": "reader", "deviceName": "Tolino"})
	for status == 201 {
		status, out, header = a.call(http.DefaultClient, http.MethodPost, "/device/requests",
			map[string]any{"username": "reader", "deviceName": "Tolino"})
	}
	if status != 429 || header.Get("Retry-After") == "" {
		t.Errorf("many requests from one address: %d %v", status, out)
	}
}

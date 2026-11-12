//go:build integration

package test

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/praetorianer777/gotome/backend/internal/httpapi"
	"github.com/praetorianer777/gotome/backend/internal/sso/ssotest"
)

// ssoApp is an app set up to sign people in through a test identity
// provider, with an administrator who signs in with a password.
type ssoApp struct {
	*app
	idp     *ssotest.Provider
	admin   *http.Client
	adminID uuid.UUID
}

func newSSOApp(t *testing.T, extra map[string]string) *ssoApp {
	t.Helper()
	a := &ssoApp{app: newApp(t), idp: ssotest.New(t)}
	var id string
	a.admin, id = a.signedIn("admin", "admin")
	a.adminID = uuid.MustParse(id)
	values := map[string]string{
		"oidc.issuer": a.idp.URL, "oidc.clientId": ssotest.ClientID, "oidc.clientSecret": ssotest.ClientSecret,
		"oidc.name": "Test ID",
	}
	for k, v := range extra {
		values[k] = v
	}
	a.set(values)
	return a
}

func (a *ssoApp) set(values map[string]string) {
	a.t.Helper()
	update := map[string]*string{}
	for k, v := range values {
		update[k] = &v
	}
	if err := a.settings.Update(context.Background(), a.adminID, update); err != nil {
		a.t.Fatal(err)
	}
}

// noFollow is the browser c, stopping at every redirect.
func noFollow(c *http.Client) *http.Client {
	stopped := *c
	stopped.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &stopped
}

// redirect requests the address and returns where it redirects to.
func (a *ssoApp) redirect(c *http.Client, address string) *url.URL {
	a.t.Helper()
	resp, err := noFollow(c).Get(address)
	if err != nil {
		a.t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusFound {
		a.t.Fatalf("GET %s: %d, want a redirect", address, resp.StatusCode)
	}
	// As sent: a path of the app stays one.
	to, err := url.Parse(resp.Header.Get("Location"))
	if err != nil {
		a.t.Fatal(err)
	}
	return to
}

// authorize starts at the route, as the web app does, and goes through the
// identity provider: what it returns is the callback the browser is sent
// back to.
func (a *ssoApp) authorize(c *http.Client, route string, body any) *url.URL {
	a.t.Helper()
	status, out, _ := a.call(c, http.MethodPost, route, body)
	target, _ := out["url"].(string)
	if status != 200 || target == "" {
		a.t.Fatalf("POST %s: %d %v", route, status, out)
	}
	callback := a.redirect(c, target)
	if callback.Path != httpapi.APIPrefix+"/auth/oidc/callback" {
		a.t.Fatalf("the provider sent the browser to %s", callback)
	}
	return callback
}

// signInVia signs the browser in as whoever the provider's claims say and
// returns where in the app it ends up.
func (a *ssoApp) signInVia(c *http.Client, claims map[string]any) string {
	a.t.Helper()
	a.idp.SignIn(claims)
	return a.redirect(c, a.authorize(c, "/auth/oidc/start", map[string]any{"returnTo": "/books?sort=title"}).String()).String()
}

func (a *ssoApp) me(c *http.Client) map[string]any {
	a.t.Helper()
	status, body, _ := a.call(c, http.MethodGet, "/auth/me", nil)
	if status != 200 {
		return nil
	}
	return body
}

var alice = map[string]any{
	"sub": "alice-1", "preferred_username": "alice", "email": "alice@example.org", "email_verified": true,
	"groups": []string{"bookworms"},
}

func with(claims map[string]any, k string, v any) map[string]any {
	out := map[string]any{k: v}
	for key, value := range claims {
		if key != k {
			out[key] = value
		}
	}
	return out
}

func TestTheIdentityProviderSignsInAndMakesTheAccountWithTheMappedRole(t *testing.T) {
	a := newSSOApp(t, map[string]string{"oidc.adminGroups": "Librarians", "oidc.editorGroups": "bookworms"})

	status, methods, _ := a.call(a.browser(), http.MethodGet, "/auth/methods", nil)
	sso, _ := methods["sso"].(map[string]any)
	if status != 200 || methods["password"] != true || sso["name"] != "Test ID" {
		t.Fatalf("methods: %d %v", status, methods)
	}

	c := a.browser()
	if landed := a.signInVia(c, alice); landed != "/books?sort=title" {
		t.Errorf("landed on %q", landed)
	}
	me := a.me(c)
	if me["username"] != "alice" || me["role"] != "editor" || me["email"] != "alice@example.org" {
		t.Fatalf("signed in as %v", me)
	}

	// The same person again is the same account; the groups decide the
	// role every time, compared without regard to case.
	again := a.browser()
	a.signInVia(again, with(alice, "groups", []string{"librarians"}))
	if me := a.me(again); me["id"] != a.me(c)["id"] || me["role"] != "admin" {
		t.Errorf("second sign-in: %v", me)
	}
	a.signInVia(again, with(alice, "groups", nil))
	if me := a.me(again); me["role"] != "reader" {
		t.Errorf("in no group the default role: %v", me)
	}

	// Another person whose name is taken gets it numbered.
	bob := a.browser()
	a.signInVia(bob, map[string]any{"sub": "bob-1", "preferred_username": "alice"})
	if me := a.me(bob); me["username"] != "alice-2" {
		t.Errorf("a taken name: %v", me)
	}
	if n := a.countRows("SELECT count(*) FROM users"); n != 3 {
		t.Errorf("%d accounts, want the administrator, alice and alice-2", n)
	}
	// What the sign-in was made of is not left behind.
	if n := a.countRows("SELECT count(*) FROM oidc_logins"); n != 0 {
		t.Errorf("%d sign-ins left", n)
	}
}

func TestATamperedOrForeignAnswerSignsNobodyIn(t *testing.T) {
	a := newSSOApp(t, nil)
	refused := func(name, landed, want string) {
		t.Helper()
		if !strings.HasPrefix(landed, "/login?sso="+want) {
			t.Errorf("%s: landed on %q, want /login?sso=%s", name, landed, want)
		}
	}

	for name, tamper := range map[string]func(map[string]any){
		"another nonce":    func(c map[string]any) { c["nonce"] = "guessed" },
		"another issuer":   func(c map[string]any) { c["iss"] = "https://evil.example" },
		"another audience": func(c map[string]any) { c["aud"] = "someone-else" },
		"expired":          func(c map[string]any) { c["exp"] = time.Now().Add(-time.Hour).Unix() },
	} {
		c := a.browser()
		a.idp.SignIn(alice)
		a.idp.Tamper(tamper)
		refused(name, a.redirect(c, a.authorize(c, "/auth/oidc/start", map[string]any{}).String()).String(), "failed")
		if a.me(c) != nil {
			t.Errorf("%s: signed in", name)
		}
	}

	c := a.browser()
	a.idp.SignWithUnknownKey(true)
	a.idp.SignIn(alice)
	refused("an unknown key", a.redirect(c, a.authorize(c, "/auth/oidc/start", map[string]any{}).String()).String(), "failed")
	a.idp.SignWithUnknownKey(false)

	// The answer names another provider than the one asked (RFC 9207).
	callback := a.authorize(c, "/auth/oidc/start", map[string]any{})
	q := callback.Query()
	q.Set("iss", "https://evil.example")
	callback.RawQuery = q.Encode()
	refused("an answer from elsewhere", a.redirect(c, callback.String()).String(), "failed")

	// A callback finished in another browser than the one that started it,
	// as someone luring a person to their own sign-in would.
	callback = a.authorize(c, "/auth/oidc/start", map[string]any{})
	victim := a.browser()
	refused("another browser", a.redirect(victim, callback.String()).String(), "expired")
	// The state is good once, even with its cookie brought back.
	a.redirect(c, callback.String())
	a.call(c, http.MethodPost, "/auth/logout", nil)
	c.Jar.SetCookies(callback, []*http.Cookie{{Name: "gotome_oidc", Value: callback.Query().Get("state"), Path: httpapi.APIPrefix + "/auth/oidc"}})
	refused("a used state", a.redirect(c, callback.String()).String(), "expired")

	// The person cancels at the provider.
	callback = a.authorize(c, "/auth/oidc/start", map[string]any{})
	q = callback.Query()
	q.Del("code")
	q.Set("error", "access_denied")
	callback.RawQuery = q.Encode()
	refused("cancelled", a.redirect(c, callback.String()).String(), "cancelled")

	if a.me(victim) != nil || a.me(c) != nil {
		t.Error("a refused sign-in left a session")
	}
	if n := a.countRows("SELECT count(*) FROM users"); n != 2 {
		t.Errorf("%d accounts, want the administrator's and the one of the sign-in that held", n)
	}
	// Somewhere outside the app is never where a sign-in returns to.
	for _, returnTo := range []string{"//evil.example", "https://evil.example", "/\\evil.example", "/api/v1/setup"} {
		c := a.browser()
		a.idp.SignIn(alice)
		landed := a.redirect(c, a.authorize(c, "/auth/oidc/start", map[string]any{"returnTo": returnTo}).String())
		if landed.String() != "/" {
			t.Errorf("returnTo %q: landed on %s", returnTo, landed)
		}
	}
}

func TestWhoMayGetAnAccountIsTheSettingsChoice(t *testing.T) {
	a := newSSOApp(t, map[string]string{"oidc.signup": "off"})
	c := a.browser()
	if landed := a.signInVia(c, alice); landed != "/login?sso=no-account" {
		t.Errorf("without sign-up: %q", landed)
	}

	// An account with the address the provider verified is taken, once
	// that is allowed; an address it did not verify is not.
	_, readerID := a.signedIn("reader", "reader")
	if _, err := a.pool.Exec(context.Background(), "UPDATE users SET email = 'alice@example.org' WHERE id = $1", readerID); err != nil {
		t.Fatal(err)
	}
	a.set(map[string]string{"oidc.linkByEmail": "on"})
	if landed := a.signInVia(c, with(alice, "email_verified", false)); landed != "/login?sso=no-account" {
		t.Errorf("an unverified address: %q", landed)
	}
	a.signInVia(c, with(alice, "email_verified", "true"))
	if me := a.me(c); me["id"] != readerID {
		t.Errorf("by a verified address: %v, want the reader's account", me)
	}

	a.set(map[string]string{"oidc.signup": "on", "oidc.defaultRole": "none"})
	if landed := a.signInVia(a.browser(), map[string]any{"sub": "carol-1"}); landed != "/login?sso=no-role" {
		t.Errorf("in no group with no default role: %q", landed)
	}

	// A disabled account stays shut, whoever vouches for it.
	if status, body, _ := a.call(a.admin, http.MethodPatch, "/users/"+readerID, map[string]any{"disabled": true}); status != 200 {
		t.Fatalf("disable: %d %v", status, body)
	}
	if landed := a.signInVia(a.browser(), alice); landed != "/login?sso=disabled" {
		t.Errorf("disabled: %q", landed)
	}
}

func TestAnIdentityIsLinkedAndUnlinkedFromTheProfile(t *testing.T) {
	a := newSSOApp(t, nil)
	reader, readerID := a.signedIn("reader", "reader")

	a.idp.SignIn(alice)
	callback := a.authorize(reader, "/auth/identities", nil)
	// Someone else's browser cannot finish a link the reader started.
	if landed := a.redirect(a.admin, callback.String()); landed.String() != "/login?sso=expired" {
		t.Errorf("finished elsewhere: %s", landed)
	}
	callback = a.authorize(reader, "/auth/identities", nil)
	if landed := a.redirect(reader, callback.String()); landed.String() != "/profile?sso=linked" {
		t.Fatalf("link: %s", landed)
	}
	if me := a.me(reader); me["id"] != readerID {
		t.Errorf("linking changed who is signed in: %v", me)
	}
	c := a.browser()
	a.signInVia(c, alice)
	if me := a.me(c); me["id"] != readerID {
		t.Errorf("signed in through the link as %v", me)
	}

	// The identity is the reader's; nobody else may link it.
	a.idp.SignIn(alice)
	if landed := a.redirect(a.admin, a.authorize(a.admin, "/auth/identities", nil).String()); landed.String() != "/profile?sso=taken" {
		t.Errorf("linking another's identity: %s", landed)
	}

	status, list, _ := a.call(reader, http.MethodGet, "/auth/identities", nil)
	items, _ := list["items"].([]any)
	if status != 200 || len(items) != 1 {
		t.Fatalf("identities: %d %v", status, list)
	}
	id := items[0].(map[string]any)["id"].(string)
	if item := items[0].(map[string]any); item["issuer"] != a.idp.URL || item["email"] != "alice@example.org" {
		t.Errorf("identity: %v", item)
	}
	if status, _, _ := a.call(a.admin, http.MethodDelete, "/auth/identities/"+id, nil); status != 404 {
		t.Errorf("someone else's identity: %d", status)
	}
	if status, _, _ := a.call(reader, http.MethodDelete, "/auth/identities/"+id, nil); status != 204 {
		t.Errorf("unlink: %d", status)
	}

	// An account made by the provider has no password: its one identity
	// stays.
	bob := a.browser()
	a.signInVia(bob, map[string]any{"sub": "bob-1", "preferred_username": "bob"})
	_, list, _ = a.call(bob, http.MethodGet, "/auth/identities", nil)
	id = list["items"].([]any)[0].(map[string]any)["id"].(string)
	if status, body, _ := a.call(bob, http.MethodDelete, "/auth/identities/"+id, nil); status != 409 {
		t.Errorf("the last way in: %d %v", status, body)
	}
}

func TestTurningPasswordsOffNeverLocksOutTheLastAdministrator(t *testing.T) {
	a := newSSOApp(t, map[string]string{"oidc.adminGroups": "admins"})
	a.signedIn("reader", "reader")
	a.set(map[string]string{"auth.passwords": "off"})
	login := func(username string) int {
		status, _, _ := a.call(a.browser(), http.MethodPost, "/auth/login", map[string]any{"username": username, "password": "a long password"})
		return status
	}

	// No administrator can sign in through the provider yet: they may still
	// use their password, and only they.
	if status := login("admin"); status != 200 {
		t.Errorf("the administrator without an identity: %d", status)
	}
	if status := login("reader"); status != 403 {
		t.Errorf("a reader with passwords off: %d", status)
	}
	if _, methods, _ := a.call(a.browser(), http.MethodGet, "/auth/methods", nil); methods["password"] != true {
		t.Errorf("methods: %v", methods)
	}

	adminSub := map[string]any{"sub": "root-1", "groups": []string{"admins"}}
	a.idp.SignIn(adminSub)
	if landed := a.redirect(a.admin, a.authorize(a.admin, "/auth/identities", nil).String()); landed.String() != "/profile?sso=linked" {
		t.Fatalf("link: %s", landed)
	}
	if status := login("admin"); status != 403 {
		t.Errorf("the administrator, now with an identity: %d", status)
	}
	if _, methods, _ := a.call(a.browser(), http.MethodGet, "/auth/methods", nil); methods["password"] != false {
		t.Errorf("methods: %v", methods)
	}

	// The provider may not take away the last administrator.
	c := a.browser()
	a.signInVia(c, with(adminSub, "groups", nil))
	if me := a.me(c); me["id"] != a.adminID.String() || me["role"] != "admin" {
		t.Errorf("the last administrator demoted: %v", me)
	}
	// Nor may they unlink the one way they have left.
	_, list, _ := a.call(c, http.MethodGet, "/auth/identities", nil)
	id := list["items"].([]any)[0].(map[string]any)["id"].(string)
	if status, _, _ := a.call(c, http.MethodDelete, "/auth/identities/"+id, nil); status != 409 {
		t.Errorf("unlinking the last way in: %d", status)
	}
	// With another administrator who can sign in, the groups decide.
	other := a.browser()
	a.signInVia(other, map[string]any{"sub": "root-2", "groups": []string{"admins"}})
	a.signInVia(c, with(adminSub, "groups", nil))
	if me := a.me(c); me["role"] != "reader" {
		t.Errorf("demoted beside another administrator: %v", me)
	}

	// Once no administrator who can sign in has an identity, passwords
	// come back for administrators.
	if _, err := a.pool.Exec(context.Background(), "DELETE FROM user_identities"); err != nil {
		t.Fatal(err)
	}
	if _, err := a.pool.Exec(context.Background(), "UPDATE users SET role = 'admin' WHERE id = $1", a.adminID); err != nil {
		t.Fatal(err)
	}
	if status := login("admin"); status != 200 {
		t.Errorf("the administrator after the identities went: %d", status)
	}

	// Without an identity provider set up, passwords are the only way in.
	if status := login("reader"); status != 403 {
		t.Fatalf("a reader with passwords off: %d", status)
	}
	if err := a.settings.Update(context.Background(), a.adminID, map[string]*string{"oidc.issuer": nil}); err != nil {
		t.Fatal(err)
	}
	if status := login("reader"); status != 200 {
		t.Errorf("a reader once no provider is set up: %d", status)
	}
}

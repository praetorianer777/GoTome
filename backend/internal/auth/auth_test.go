package auth

import (
	"errors"
	"strings"
	"testing"
	"time"
)

// Cheap settings: the tests check the logic, not the cost.
var fast = PasswordParams{MemoryKiB: 64, Iterations: 1, Parallelism: 1, SaltLen: 16, KeyLen: 32}

func TestPasswordRoundTrip(t *testing.T) {
	hash, err := HashPassword("correct horse battery staple", fast)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(hash, "$argon2id$v=19$m=64,t=1,p=1$") {
		t.Errorf("hash = %q, want the PHC format with its parameters", hash)
	}
	if ok, err := VerifyPassword("correct horse battery staple", hash); err != nil || !ok {
		t.Errorf("the right password: %v, %v", ok, err)
	}
	if ok, err := VerifyPassword("correct horse battery stapl", hash); err != nil || ok {
		t.Errorf("a wrong password: %v, %v", ok, err)
	}

	again, _ := HashPassword("correct horse battery staple", fast)
	if again == hash {
		t.Error("two hashes of one password are equal; the salt is not random")
	}
}

// A hash made with other settings still verifies: the settings travel in it.
func TestVerifyUsesTheParametersInTheHash(t *testing.T) {
	old, _ := HashPassword("secret-password", PasswordParams{MemoryKiB: 32, Iterations: 3, Parallelism: 2, SaltLen: 8, KeyLen: 16})
	if ok, err := VerifyPassword("secret-password", old); err != nil || !ok {
		t.Errorf("a hash with older settings: %v, %v", ok, err)
	}
}

func TestVerifyRefusesMalformedAndHostileHashes(t *testing.T) {
	good, _ := HashPassword("x", fast)
	parts := strings.Split(good, "$")
	with := func(i int, v string) string {
		p := append([]string(nil), parts...)
		p[i] = v
		return strings.Join(p, "$")
	}
	for name, hash := range map[string]string{
		"empty":               "",
		"bcrypt":              "$2a$10$abcdefghijklmnopqrstuv",
		"argon2i":             with(1, "argon2i"),
		"other version":       with(2, "v=16"),
		"no parameters":       with(3, "m=64"),
		"gigabytes of memory": with(3, "m=4294967295,t=1,p=1"),
		"endless iterations":  with(3, "m=64,t=4000000,p=1"),
		"no threads":          with(3, "m=64,t=1,p=0"),
		"salt is not base64":  with(4, "!!!"),
		"empty key":           with(5, ""),
	} {
		if ok, err := VerifyPassword("x", hash); ok || !errors.Is(err, errMalformedHash) {
			t.Errorf("%s: ok = %v, err = %v, want a malformed-hash error", name, ok, err)
		}
	}
}

func TestValidate(t *testing.T) {
	email := func(s string) *string { return &s }

	name, mail, err := validate("  Steve ", "long enough", email(" steve@example.com "))
	if err != nil || name != "Steve" || mail == nil || *mail != "steve@example.com" {
		t.Errorf("valid details: %q, %v, %v", name, mail, err)
	}
	if _, mail, err := validate("steve", "long enough", email("  ")); err != nil || mail != nil {
		t.Errorf("a blank e-mail address is none: %v, %v", mail, err)
	}

	cases := []struct {
		username, password string
		email              *string
		field              string
	}{
		{"", "long enough", nil, "username"},
		{"two words", "long enough", nil, "username"},
		{strings.Repeat("a", MaxUsernameLen+1), "long enough", nil, "username"},
		{"steve", "short", nil, "password"},
		{"steve", strings.Repeat("a", MaxPasswordLen+1), nil, "password"},
		{"steve", "long enough", email("not-an-address"), "email"},
		{"steve", "long enough", email("@example.com"), "email"},
		{"steve", "long enough", email("steve@"), "email"},
	}
	for _, c := range cases {
		_, _, err := validate(c.username, c.password, c.email)
		var invalid *ValidationError
		if !errors.As(err, &invalid) || invalid.Fields[c.field] == "" {
			t.Errorf("%q / %d-char password / %v: err = %v, want a message for %s", c.username, len(c.password), c.email, err, c.field)
		}
	}
}

func TestLimiter(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	l := NewLimiter(3, time.Minute, func() time.Time { return now })

	for i := range 3 {
		if wait := l.RetryAfter("k"); wait != 0 {
			t.Fatalf("blocked after %d failures, want 3 allowed", i)
		}
		l.Fail("k")
		now = now.Add(10 * time.Second)
	}
	// Failures at 0s, 10s and 20s; it is now 30s. The first leaves the window at 60s.
	if wait := l.RetryAfter("k"); wait != 30*time.Second {
		t.Errorf("RetryAfter = %v, want 30s", wait)
	}
	if wait := l.RetryAfter("other"); wait != 0 {
		t.Errorf("another key is blocked for %v", wait)
	}

	now = now.Add(31 * time.Second)
	if wait := l.RetryAfter("k"); wait != 0 {
		t.Errorf("still blocked for %v after the window moved on", wait)
	}

	l.Fail("k")
	l.Fail("k")
	l.Reset("k")
	if wait := l.RetryAfter("k"); wait != 0 {
		t.Errorf("blocked for %v after a reset", wait)
	}
}

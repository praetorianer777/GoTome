package main

import (
	"slices"
	"testing"

	"github.com/praetorianer777/gotome/backend/internal/metadata/crossref"
	"github.com/praetorianer777/gotome/backend/internal/metadata/hardcover"
	"github.com/praetorianer777/gotome/backend/internal/metadata/openlibrary"
	"github.com/praetorianer777/gotome/backend/internal/settings"
)

// The setting offers the providers serve registers, and no other.
func TestTheProvidersSettingNamesTheRegisteredProviders(t *testing.T) {
	registered := []string{openlibrary.Name, crossref.Name, hardcover.Name}
	if !slices.Equal(registered, settings.ProviderNames) {
		t.Errorf("serve registers %v, the setting offers %v", registered, settings.ProviderNames)
	}
}

func TestHealthURL(t *testing.T) {
	cases := []struct{ addr, want string }{
		{":8080", "http://127.0.0.1:8080/readyz"},
		{"0.0.0.0:8080", "http://127.0.0.1:8080/readyz"},
		{"[::]:8080", "http://127.0.0.1:8080/readyz"},
		{"192.0.2.7:9000", "http://192.0.2.7:9000/readyz"},
		{"[::1]:8080", "http://[::1]:8080/readyz"},
	}
	for _, c := range cases {
		if got := healthURL(c.addr); got != c.want {
			t.Errorf("healthURL(%q) = %q, want %q", c.addr, got, c.want)
		}
	}
}

func TestRunRejectsUnknownCommand(t *testing.T) {
	if err := run([]string{"frobnicate"}); err == nil {
		t.Error("an unknown command was accepted")
	}
	if err := run(nil); err == nil {
		t.Error("a missing command was accepted")
	}
}

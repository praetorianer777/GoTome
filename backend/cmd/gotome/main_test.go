package main

import "testing"

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

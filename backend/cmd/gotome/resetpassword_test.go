package main

import (
	"crypto/rand"
	"flag"
	"io"
	"strings"
	"testing"
)

func TestResetPasswordTakesFlagsOnEitherSide(t *testing.T) {
	for _, args := range [][]string{
		{"-enable", "-admin", "ada"},
		{"ada", "-enable", "-admin"},
		{"-enable", "ada", "--admin"},
	} {
		flags := flag.NewFlagSet("reset-password", flag.ContinueOnError)
		enable := flags.Bool("enable", false, "")
		admin := flags.Bool("admin", false, "")
		username, err := parseResetArgs(flags, args)
		if err != nil || username != "ada" || !*enable || !*admin {
			t.Errorf("%v: %q enable=%v admin=%v %v", args, username, *enable, *admin, err)
		}
	}
	for _, args := range [][]string{nil, {"ada", "a-password-as-argument"}, {"-password", "x", "ada"}} {
		flags := flag.NewFlagSet("reset-password", flag.ContinueOnError)
		flags.SetOutput(io.Discard)
		flags.Bool("enable", false, "")
		if _, err := parseResetArgs(flags, args); err == nil {
			t.Errorf("%v was taken", args)
		}
	}
}

func TestResetPasswordReadsTheFirstLineOfStdin(t *testing.T) {
	password := " " + rand.Text() + " "
	for _, in := range []string{password, password + "\n", password + "\r\n", password + "\nmore\n"} {
		got, err := readPasswordLine(strings.NewReader(in))
		if err != nil || got != password {
			t.Errorf("%q: %q %v", in, got, err)
		}
	}
	for _, in := range []string{"", "\n"} {
		if _, err := readPasswordLine(strings.NewReader(in)); err == nil {
			t.Errorf("%q was taken for a password", in)
		}
	}
}

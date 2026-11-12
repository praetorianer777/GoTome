package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/praetorianer777/gotome/backend/internal/auth"
	"github.com/praetorianer777/gotome/backend/internal/config"
)

// resetPassword sets a new password for an account and ends its sessions,
// for an operator who can no longer sign in to do it on the Users page. The
// password is read from the terminal, or from stdin when that is not one,
// never from an argument or a variable: those end up in the process list
// and the shell's history.
func resetPassword(args []string) error {
	flags := flag.NewFlagSet("reset-password", flag.ContinueOnError)
	enable := flags.Bool("enable", false, "enable the account if it is disabled")
	admin := flags.Bool("admin", false, "make the account an administrator")
	flags.Usage = func() {
		fmt.Fprintln(flags.Output(), "Usage: gotome reset-password [-enable] [-admin] <user name>")
		fmt.Fprintln(flags.Output(), "The new password is read from the terminal, or from stdin when it is not one.")
		flags.PrintDefaults()
	}
	username, err := parseResetArgs(flags, args)
	if err != nil {
		return err
	}

	password, err := readNewPassword(os.Stdin, os.Stderr)
	if err != nil {
		return err
	}

	cfg, err := config.Load()
	if err != nil {
		return err
	}
	log := newLogger(cfg)
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	pool, err := openDatabase(ctx, cfg, log)
	if err != nil {
		return err
	}
	defer pool.Close()
	accounts, err := auth.NewService(pool, auth.DefaultPasswordParams(), auth.DefaultSessionTTL)
	if err != nil {
		return err
	}
	account, err := accounts.ResetPassword(ctx, username, password, auth.Reset{Enable: *enable, Admin: *admin})
	var invalid *auth.ValidationError
	switch {
	case errors.Is(err, auth.ErrNoUser):
		return fmt.Errorf("there is no account named %q", username)
	case errors.As(err, &invalid):
		return errors.New(invalid.Fields["password"])
	case err != nil:
		return err
	}
	fmt.Printf("The password of %s is set, and its sessions are ended.\n", account.Username)
	if account.Disabled {
		fmt.Println("The account is disabled and cannot sign in; -enable takes that back.")
	}
	if account.Role != auth.RoleAdmin {
		fmt.Printf("The account is %s %s; -admin makes it an administrator.\n", article(account.Role), account.Role)
	}
	return nil
}

// parseResetArgs takes the flags before and after the user name, as an
// operator may write them on either side.
func parseResetArgs(flags *flag.FlagSet, args []string) (string, error) {
	if err := flags.Parse(args); err != nil {
		return "", err
	}
	rest := flags.Args()
	if len(rest) == 0 {
		flags.Usage()
		return "", errors.New("no user name given")
	}
	username := rest[0]
	if err := flags.Parse(rest[1:]); err != nil {
		return "", err
	}
	if flags.NArg() > 0 {
		flags.Usage()
		return "", errors.New("one user name, and the password is not an argument")
	}
	return username, nil
}

func article(role string) string {
	if role == auth.RoleEditor {
		return "an"
	}
	return "a"
}

// readNewPassword asks for the password twice on a terminal, with echo off,
// and otherwise takes the first line of in.
func readNewPassword(in *os.File, prompt io.Writer) (string, error) {
	first, ok, err := readHidden(in, prompt, "New password: ")
	if err != nil {
		return "", err
	}
	if !ok {
		return readPasswordLine(in)
	}
	again, _, err := readHidden(in, prompt, "The same again: ")
	if err != nil {
		return "", err
	}
	if first != again {
		return "", errors.New("the two passwords differ")
	}
	return first, nil
}

// readPasswordLine reads up to the first line break, which is not part of
// the password; everything else is, spaces included.
func readPasswordLine(in io.Reader) (string, error) {
	line, err := bufio.NewReader(io.LimitReader(in, auth.MaxPasswordLen+2)).ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}
	line = strings.TrimSuffix(strings.TrimSuffix(line, "\n"), "\r")
	if line == "" {
		return "", errors.New("no password given on stdin")
	}
	return line, nil
}

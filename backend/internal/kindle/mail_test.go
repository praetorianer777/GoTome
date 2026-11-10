package kindle

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"io"
	"mime"
	"mime/multipart"
	"net"
	"net/mail"
	"strconv"
	"strings"
	"testing"

	"github.com/praetorianer777/gotome/backend/internal/kindle/smtptest"
)

func plainDial(ctx context.Context, network, address string) (net.Conn, error) {
	var d net.Dialer
	return d.DialContext(ctx, network, address)
}

func serverFor(s *smtptest.Server, security string) Server {
	return Server{Host: s.Host, Port: s.Port, Security: security, Sender: "library@example.org", Dial: plainDial, TLS: s.TLS}
}

func message(book []byte) Message {
	return Message{
		To: "reader@kindle.com", ID: "abc@example.org", Subject: "Dune\r\nBcc: x@example.org", Body: "Sent by GOtome",
		Filename: "Dune\r\n.epub", ContentType: "application/epub+zip", Attachment: bytes.NewReader(book),
	}
}

func TestSendDeliversTheAttachmentIntactOverEveryKindOfTLS(t *testing.T) {
	book := make([]byte, 200_000)
	_, _ = rand.Read(book)
	for _, tc := range []struct {
		name     string
		security string
		opts     smtptest.Options
	}{
		{"starttls", STARTTLS, smtptest.Options{User: "u", Password: "p"}},
		{"implicit", ImplicitTLS, smtptest.Options{Implicit: true, User: "u", Password: "p"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := smtptest.New(t, tc.opts)
			s := serverFor(srv, tc.security)
			s.User, s.Password = "u", "p"
			if err := s.Send(context.Background(), message(book)); err != nil {
				t.Fatal(err)
			}
			mails := srv.Mails()
			if len(mails) != 1 {
				t.Fatalf("%d mails", len(mails))
			}
			m := mails[0]
			if !m.Encrypted || m.AuthUser != "u" || m.From != "library@example.org" || len(m.To) != 1 || m.To[0] != "reader@kindle.com" {
				t.Fatalf("envelope: %+v", m)
			}
			parsed, err := mail.ReadMessage(strings.NewReader(m.Data))
			if err != nil {
				t.Fatal(err)
			}
			if parsed.Header.Get("Bcc") != "" || parsed.Header.Get("Message-ID") != "<abc@example.org>" {
				t.Fatalf("headers: %v", parsed.Header)
			}
			_, params, err := mime.ParseMediaType(parsed.Header.Get("Content-Type"))
			if err != nil {
				t.Fatal(err)
			}
			mr := multipart.NewReader(parsed.Body, params["boundary"])
			var got []byte
			for {
				p, err := mr.NextPart()
				if errors.Is(err, io.EOF) {
					break
				}
				if err != nil {
					t.Fatal(err)
				}
				if p.FileName() == "" {
					continue
				}
				if p.FileName() != "Dune.epub" {
					t.Fatalf("filename %q", p.FileName())
				}
				raw, _ := io.ReadAll(p)
				got, err = base64.StdEncoding.DecodeString(strings.NewReplacer("\r", "", "\n", "").Replace(string(raw)))
				if err != nil {
					t.Fatal(err)
				}
			}
			if !bytes.Equal(got, book) {
				t.Fatalf("attachment differs: %d bytes of %d", len(got), len(book))
			}
		})
	}
}

func TestSendRequiresTLSWhereAskedAndNeverFallsBackToPlain(t *testing.T) {
	srv := smtptest.New(t, smtptest.Options{NoSTARTTLS: true, User: "u", Password: "secret-password"})
	s := serverFor(srv, STARTTLS)
	s.User, s.Password = "u", "secret-password"
	err := s.Send(context.Background(), message([]byte("x")))
	var se *SendError
	if !errors.Is(err, ErrNoTLS) || !errors.As(err, &se) || se.Temporary {
		t.Fatalf("err = %v", err)
	}
	if len(srv.Mails()) != 0 {
		t.Fatal("sent in the clear")
	}
}

func TestSendDoesNotOfferAPasswordInTheClearToAnotherHost(t *testing.T) {
	srv := smtptest.New(t, smtptest.Options{User: "u", Password: "p"})
	s := serverFor(srv, NoTLS)
	s.Host = "mail.example.org"
	s.Dial = func(ctx context.Context, network, _ string) (net.Conn, error) {
		return plainDial(ctx, network, net.JoinHostPort(srv.Host, strconv.Itoa(srv.Port)))
	}
	s.User, s.Password = "u", "p"
	if err := s.Send(context.Background(), message([]byte("x"))); err == nil || len(srv.Mails()) != 0 {
		t.Fatalf("err = %v, mails = %d", err, len(srv.Mails()))
	}
}

func TestSendSaysWhatIsRetriedAndWhatIsNot(t *testing.T) {
	for _, tc := range []struct {
		name      string
		opts      smtptest.Options
		temporary bool
		unknown   bool
	}{
		{"recipient refused", smtptest.Options{RejectRcpt: "550 5.7.1 sender not approved"}, false, false},
		{"asked to wait", smtptest.Options{RejectData: "452 4.3.1 try later"}, true, false},
		{"refused for good", smtptest.Options{RejectData: "552 5.3.4 too big"}, false, false},
		{"answer lost", smtptest.Options{DropAfterData: true}, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := smtptest.New(t, tc.opts)
			err := serverFor(srv, STARTTLS).Send(context.Background(), message([]byte("x")))
			var se *SendError
			if !errors.As(err, &se) {
				t.Fatalf("err = %v", err)
			}
			if se.Temporary != tc.temporary || errors.Is(err, ErrUnknown) != tc.unknown {
				t.Fatalf("temporary=%v unknown=%v: %v", se.Temporary, errors.Is(err, ErrUnknown), err)
			}
		})
	}
}

func TestSendToAServerThatIsNotThereIsTemporary(t *testing.T) {
	s := Server{Host: "127.0.0.1", Port: 1, Security: STARTTLS, Sender: "a@b.c", Dial: plainDial}
	var se *SendError
	if err := s.Send(context.Background(), message([]byte("x"))); !errors.As(err, &se) || !se.Temporary {
		t.Fatalf("err = %v", err)
	}
}

func TestCleanAddressTakesOnlyAKindle(t *testing.T) {
	for in, want := range map[string]string{
		" Reader_1@Kindle.com ": "reader_1@kindle.com",
		"x@free.kindle.com":     "x@free.kindle.com",
	} {
		if got, err := CleanAddress(in); err != nil || got != want {
			t.Errorf("%q: %q, %v", in, got, err)
		}
	}
	for _, in := range []string{"", "a@example.org", "a@kindle.com.evil.org", "a@evilkindle.com", "A <a@kindle.com>", "a@kindle.com, b@example.org", "a b@kindle.com", "@kindle.com"} {
		if _, err := CleanAddress(in); err == nil {
			t.Errorf("%q was taken", in)
		}
	}
}

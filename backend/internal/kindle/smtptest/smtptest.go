// Package smtptest is a mail server for tests: it listens on the loopback,
// speaks enough SMTP for a client to send a message, and keeps what it was
// sent. It stands for the administrator's server, so no test reaches a real
// one.
package smtptest

import (
	"bufio"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"fmt"
	"math/big"
	"net"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// Options say how the server behaves.
type Options struct {
	// Implicit makes the connection TLS from the first byte.
	Implicit bool
	// NoSTARTTLS leaves STARTTLS out of what EHLO offers.
	NoSTARTTLS bool
	// User and Password, when set, are what AUTH PLAIN must bring.
	User, Password string
	// RejectRcpt answers RCPT TO with this reply, such as "550 not allowed".
	RejectRcpt string
	// RejectData answers the end of the message with this reply, such as
	// "452 try later".
	RejectData string
	// DropAfterData closes the connection instead of answering the end of
	// the message, as a server that crashed after taking it would.
	DropAfterData bool
}

// Mail is one message the server was sent.
type Mail struct {
	From     string
	To       []string
	Data     string
	AuthUser string
	// Encrypted says it came over TLS.
	Encrypted bool
}

// Server is the running server.
type Server struct {
	// Host and Port are where it listens.
	Host string
	Port int
	// TLS is the client configuration that trusts its certificate.
	TLS *tls.Config

	opts Options
	ln   net.Listener
	cert tls.Certificate

	mu       sync.Mutex
	mails    []Mail
	attempts int
}

// New starts a server that stops with the test.
func New(t testing.TB, o Options) *Server {
	t.Helper()
	cert, pool := certificate(t)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().(*net.TCPAddr)
	s := &Server{
		Host: "127.0.0.1", Port: addr.Port, opts: o, ln: ln, cert: cert,
		TLS: &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12},
	}
	go s.serve()
	t.Cleanup(func() { ln.Close() })
	return s
}

// Mails are the messages taken so far.
func (s *Server) Mails() []Mail {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Mail(nil), s.mails...)
}

// Attempts is how many messages the server was asked to take, whatever it
// answered.
func (s *Server) Attempts() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.attempts
}

func certificate(t testing.TB) (tls.Certificate, *x509.CertPool) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "smtptest"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(24 * time.Hour),
		KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IPAddresses: []net.IP{net.ParseIP("127.0.0.1")}, DNSNames: []string{"localhost"},
		BasicConstraintsValid: true, IsCA: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	leaf, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	pool := x509.NewCertPool()
	pool.AddCert(leaf)
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}, pool
}

func (s *Server) serve() {
	for {
		c, err := s.ln.Accept()
		if err != nil {
			return
		}
		go s.session(c)
	}
}

func (s *Server) session(c net.Conn) {
	defer func() { c.Close() }()
	_ = c.SetDeadline(time.Now().Add(30 * time.Second))
	encrypted := false
	if s.opts.Implicit {
		c = tls.Server(c, &tls.Config{Certificates: []tls.Certificate{s.cert}, MinVersion: tls.VersionTLS12})
		encrypted = true
	}
	r := bufio.NewReader(c)
	say := func(format string, args ...any) { fmt.Fprintf(c, format+"\r\n", args...) }
	say("220 smtptest ready")
	var cur Mail
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return
		}
		line = strings.TrimRight(line, "\r\n")
		verb, arg, _ := strings.Cut(line, " ")
		switch strings.ToUpper(verb) {
		case "EHLO":
			say("250-smtptest")
			if !s.opts.NoSTARTTLS && !encrypted {
				say("250-STARTTLS")
			}
			if s.opts.User != "" {
				say("250-AUTH PLAIN")
			}
			say("250 8BITMIME")
		case "STARTTLS":
			if s.opts.NoSTARTTLS || encrypted {
				say("502 not offered")
				continue
			}
			say("220 go ahead")
			c = tls.Server(c, &tls.Config{Certificates: []tls.Certificate{s.cert}, MinVersion: tls.VersionTLS12})
			r = bufio.NewReader(c)
			encrypted = true
			cur = Mail{}
		case "AUTH":
			mech, rest, _ := strings.Cut(arg, " ")
			raw, _ := base64.StdEncoding.DecodeString(rest)
			parts := strings.Split(string(raw), "\x00")
			if !strings.EqualFold(mech, "PLAIN") || len(parts) != 3 || parts[1] != s.opts.User || parts[2] != s.opts.Password {
				say("535 5.7.8 credentials rejected")
				continue
			}
			cur.AuthUser = parts[1]
			say("235 2.7.0 welcome")
		case "MAIL":
			if s.opts.User != "" && cur.AuthUser == "" {
				say("530 5.7.0 authenticate first")
				continue
			}
			cur.From = between(arg)
			say("250 ok")
		case "RCPT":
			if s.opts.RejectRcpt != "" {
				say("%s", s.opts.RejectRcpt)
				continue
			}
			cur.To = append(cur.To, between(arg))
			say("250 ok")
		case "DATA":
			say("354 end with a dot")
			var b strings.Builder
			for {
				l, err := r.ReadString('\n')
				if err != nil {
					return
				}
				if l == ".\r\n" {
					break
				}
				b.WriteString(strings.TrimPrefix(l, "."))
			}
			s.mu.Lock()
			s.attempts++
			s.mu.Unlock()
			switch {
			case s.opts.DropAfterData:
				return
			case s.opts.RejectData != "":
				say("%s", s.opts.RejectData)
			default:
				cur.Data, cur.Encrypted = b.String(), encrypted
				s.mu.Lock()
				s.mails = append(s.mails, cur)
				s.mu.Unlock()
				say("250 2.0.0 queued as " + strconv.Itoa(len(s.Mails())))
			}
			cur = Mail{AuthUser: cur.AuthUser}
		case "QUIT":
			say("221 bye")
			return
		case "RSET", "NOOP":
			say("250 ok")
		default:
			say("502 unknown")
		}
	}
}

func between(arg string) string {
	_, rest, _ := strings.Cut(arg, "<")
	address, _, _ := strings.Cut(rest, ">")
	return address
}

package kindle

import (
	"context"
	"crypto/tls"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net"
	"net/smtp"
	"net/textproto"
	"strconv"
	"strings"
	"time"
)

// Security says how the connection to the mail server is protected.
const (
	// STARTTLS starts in the clear and upgrades before anything is sent.
	STARTTLS = "starttls"
	// ImplicitTLS is TLS from the first byte, usually on port 465.
	ImplicitTLS = "tls"
	// NoTLS sends in the clear. Only for a server on a trusted network.
	NoTLS = "none"
)

// sessionTimeout bounds a whole session, the message included.
const sessionTimeout = 10 * time.Minute

var (
	// ErrNoTLS is a server that does not offer STARTTLS where it is required.
	ErrNoTLS = errors.New("the mail server does not offer TLS")
	// ErrUnknown is a message that was sent in full but whose acceptance
	// was never confirmed: it may have been delivered. Sending it again
	// could send it twice, so nothing does.
	ErrUnknown = errors.New("the mail server did not say whether it took the message")
)

// SendError is a failed send. Temporary says it may succeed if tried again
// later: the connection broke or the server asked to wait, with the message
// not accepted.
type SendError struct {
	Temporary bool
	Err       error
}

func (e *SendError) Error() string { return e.Err.Error() }
func (e *SendError) Unwrap() error { return e.Err }

// Server is the mail server and how to sign in at it.
type Server struct {
	Host     string
	Port     int
	Security string
	User     string
	Password string
	// Sender is the From address, and the envelope's.
	Sender string
	// Dial opens the connection; outbound.NewDialer in the server, a plain
	// dialer in tests.
	Dial func(ctx context.Context, network, address string) (net.Conn, error)
	// TLS replaces the TLS configuration, which tests need to trust a
	// certificate of their own. ServerName is set from Host.
	TLS *tls.Config
}

// Message is one mail with one attachment.
type Message struct {
	To string
	// ID is the Message-ID without brackets.
	ID      string
	Subject string
	Body    string
	// Filename and ContentType describe the attachment, which is read once.
	Filename    string
	ContentType string
	Attachment  io.Reader
}

// Send hands the message to the server.
func (s Server) Send(ctx context.Context, m Message) error {
	addr := net.JoinHostPort(s.Host, strconv.Itoa(s.Port))
	conn, err := s.Dial(ctx, "tcp", addr)
	if err != nil {
		return &SendError{Temporary: true, Err: fmt.Errorf("connect to the mail server: %w", err)}
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(sessionTimeout))
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stop()

	cfg := &tls.Config{MinVersion: tls.VersionTLS12}
	if s.TLS != nil {
		cfg = s.TLS.Clone()
	}
	cfg.ServerName = s.Host
	if s.Security == ImplicitTLS {
		conn = tls.Client(conn, cfg)
	}
	c, err := smtp.NewClient(conn, s.Host)
	if err != nil {
		return refusal(err, "greeting")
	}
	defer c.Close()
	if err := c.Hello("gotome"); err != nil {
		return refusal(err, "greeting")
	}
	if s.Security == STARTTLS {
		if ok, _ := c.Extension("STARTTLS"); !ok {
			return &SendError{Err: ErrNoTLS}
		}
		if err := c.StartTLS(cfg); err != nil {
			return refusal(err, "starting TLS")
		}
	}
	if s.User != "" {
		// PlainAuth refuses to send the password in the clear to anything
		// but localhost; with Security none that is the intended end of it.
		if err := c.Auth(smtp.PlainAuth("", s.User, s.Password, s.Host)); err != nil {
			return refusal(err, "signing in")
		}
	}
	if err := c.Mail(s.Sender); err != nil {
		return refusal(err, "sender")
	}
	if err := c.Rcpt(m.To); err != nil {
		return refusal(err, "recipient")
	}
	w, err := c.Data()
	if err != nil {
		return refusal(err, "message")
	}
	if err := m.write(w, s.Sender); err != nil {
		return &SendError{Temporary: true, Err: fmt.Errorf("sending the message: %w", err)}
	}
	if err := w.Close(); err != nil {
		var reply *textproto.Error
		if errors.As(err, &reply) {
			return refusal(err, "message")
		}
		return &SendError{Err: fmt.Errorf("%w: %v", ErrUnknown, err)}
	}
	_ = c.Quit()
	return nil
}

// refusal classifies an error before the message was accepted.
func refusal(err error, step string) error {
	var reply *textproto.Error
	if errors.As(err, &reply) {
		return &SendError{Temporary: reply.Code/100 == 4, Err: fmt.Errorf("the mail server refused the %s: %w", step, err)}
	}
	return &SendError{Temporary: true, Err: fmt.Errorf("the mail server failed at the %s: %w", step, err)}
}

func (m Message) write(w io.Writer, from string) error {
	mw := multipart.NewWriter(w)
	header := func(name, value string) string { return name + ": " + value + "\r\n" }
	_, err := io.WriteString(w,
		header("From", from)+
			header("To", m.To)+
			header("Subject", mime.QEncoding.Encode("utf-8", oneLine(m.Subject)))+
			header("Date", time.Now().UTC().Format(time.RFC1123Z))+
			header("Message-ID", "<"+m.ID+">")+
			header("MIME-Version", "1.0")+
			header("Content-Type", "multipart/mixed; boundary="+mw.Boundary())+
			"\r\n")
	if err != nil {
		return err
	}
	text, err := mw.CreatePart(textproto.MIMEHeader{
		"Content-Type":              {"text/plain; charset=utf-8"},
		"Content-Transfer-Encoding": {"base64"},
	})
	if err != nil {
		return err
	}
	if err := writeBase64(text, strings.NewReader(m.Body)); err != nil {
		return err
	}
	file, err := mw.CreatePart(textproto.MIMEHeader{
		"Content-Type":              {m.ContentType},
		"Content-Transfer-Encoding": {"base64"},
		"Content-Disposition":       {mime.FormatMediaType("attachment", map[string]string{"filename": oneLine(m.Filename)})},
	})
	if err != nil {
		return err
	}
	if err := writeBase64(file, m.Attachment); err != nil {
		return err
	}
	return mw.Close()
}

// oneLine drops what would end a header line.
func oneLine(s string) string {
	return strings.Map(func(r rune) rune {
		if r < ' ' || r == 0x7f {
			return -1
		}
		return r
	}, s)
}

// writeBase64 encodes r in lines of 76 characters, as mail wants.
func writeBase64(w io.Writer, r io.Reader) error {
	enc := base64.NewEncoder(base64.StdEncoding, &lineWriter{w: w})
	if _, err := io.Copy(enc, r); err != nil {
		return err
	}
	if err := enc.Close(); err != nil {
		return err
	}
	_, err := io.WriteString(w, "\r\n")
	return err
}

type lineWriter struct {
	w io.Writer
	n int
}

func (l *lineWriter) Write(p []byte) (int, error) {
	written := 0
	for len(p) > 0 {
		room := 76 - l.n
		chunk := p[:min(len(p), room)]
		if _, err := l.w.Write(chunk); err != nil {
			return written, err
		}
		written += len(chunk)
		p = p[len(chunk):]
		l.n += len(chunk)
		if l.n == 76 {
			if _, err := io.WriteString(l.w, "\r\n"); err != nil {
				return written, err
			}
			l.n = 0
		}
	}
	return written, nil
}

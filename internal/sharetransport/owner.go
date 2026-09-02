package sharetransport

import (
	"context"
	"crypto/ecdsa"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"io"
	"math/rand/v2"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/swobuforge/swobu/shareprotocol"
)

type OwnerConnector struct {
	Address            string
	IdentityPrivateKey *ecdsa.PrivateKey
	RootCAs            *x509.CertPool
	Dialer             net.Dialer
	RetryMinimum       time.Duration
	RetryMaximum       time.Duration
	HandleControl      func(io.ReadWriter) error
	OnConnected        func()
	HandleStream       func(net.Conn)
}

func (o OwnerConnector) Run(ctx context.Context) error {
	if o.Address == "" || o.IdentityPrivateKey == nil || o.HandleStream == nil {
		return errors.New("owner connector dependencies are required")
	}
	minimum := o.RetryMinimum
	if minimum <= 0 {
		minimum = time.Second
	}
	maximum := o.RetryMaximum
	if maximum < minimum {
		maximum = 30 * time.Second
	}
	backoff := minimum
	for {
		err := o.connect(ctx)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		delay := backoff + time.Duration(rand.Int64N(max(1, int64(backoff/2))))
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
		if err != nil && backoff < maximum {
			backoff *= 2
			if backoff > maximum {
				backoff = maximum
			}
		}
	}
}

func (o OwnerConnector) connect(ctx context.Context) error {
	parsed, der, err := SelfSignedClientCertificate(o.IdentityPrivateKey, time.Now())
	if err != nil {
		return err
	}
	certificate := tls.Certificate{Certificate: [][]byte{der}, PrivateKey: o.IdentityPrivateKey}
	raw, err := o.Dialer.DialContext(ctx, "tcp", o.Address)
	if err != nil {
		return err
	}
	tlsConn := tls.Client(raw, &tls.Config{MinVersion: tls.VersionTLS13, ServerName: shareprotocol.RelayHostname, RootCAs: o.RootCAs, Certificates: []tls.Certificate{certificate}})
	if err := tlsConn.HandshakeContext(ctx); err != nil {
		_ = raw.Close()
		return err
	}
	return ServeOwner(ctx, tlsConn, &parsed, o.HandleControl, o.OnConnected, o.HandleStream)
}

func ServeApplicationTLS(conn net.Conn, config *tls.Config, handler http.Handler) error {
	if config == nil || handler == nil {
		_ = conn.Close()
		return errors.New("application TLS dependencies are required")
	}
	config = config.Clone()
	config.NextProtos = []string{"http/1.1", "acme-tls/1"}
	tlsConn := tls.Server(conn, config)
	if err := tlsConn.Handshake(); err != nil {
		_ = tlsConn.Close()
		return err
	}
	server := &http.Server{Handler: handler, ReadHeaderTimeout: 10 * time.Second, ReadTimeout: 30 * time.Second, IdleTimeout: 60 * time.Second}
	err := server.Serve(newSingleConnListener(tlsConn))
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

type singleConnListener struct {
	conn     *closeSignalConn
	accepted bool
	closed   chan struct{}
	once     sync.Once
}

func newSingleConnListener(conn net.Conn) *singleConnListener {
	return &singleConnListener{
		conn:   &closeSignalConn{Conn: conn, closed: make(chan struct{})},
		closed: make(chan struct{}),
	}
}

func (l *singleConnListener) Accept() (net.Conn, error) {
	if !l.accepted {
		l.accepted = true
		return l.conn, nil
	}
	select {
	case <-l.conn.closed:
		return nil, net.ErrClosed
	case <-l.closed:
		return nil, net.ErrClosed
	}
}

func (l *singleConnListener) Close() error {
	l.once.Do(func() { close(l.closed) })
	return nil
}

func (l *singleConnListener) Addr() net.Addr { return l.conn.LocalAddr() }

type closeSignalConn struct {
	net.Conn
	closed chan struct{}
	once   sync.Once
}

func (c *closeSignalConn) Close() error {
	c.once.Do(func() { close(c.closed) })
	return c.Conn.Close()
}

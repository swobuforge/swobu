package sharetransport

import (
	"context"
	"crypto/x509"
	"io"
	"net"

	"github.com/hashicorp/yamux"
)

func ServeOwner(ctx context.Context, conn net.Conn, certificate *x509.Certificate, controlHandler func(io.ReadWriter) error, onConnected func(), handler func(net.Conn)) error {
	session, err := yamux.Client(conn, nil)
	if err != nil {
		return err
	}
	defer session.Close()
	control, err := session.Open()
	if err != nil {
		return err
	}
	defer control.Close()
	done := make(chan error, 1)
	go func() {
		for {
			stream, err := session.Accept()
			if err != nil {
				done <- err
				return
			}
			go handler(stream)
		}
	}()
	if onConnected != nil {
		onConnected()
	}
	if controlHandler != nil {
		if err := controlHandler(control); err != nil {
			return err
		}
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case err := <-done:
		return err
	}
}

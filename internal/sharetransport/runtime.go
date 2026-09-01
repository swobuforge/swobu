package sharetransport

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"io"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/swobuforge/swobu/internal/sharestate"
)

type OwnerRuntime struct {
	Address string
	RootCAs *x509.CertPool
	Store   *sharestate.Store
	TLS     *sharestate.TLSManager
	Handler http.Handler

	mu         sync.Mutex
	baseCtx    context.Context
	stopBase   context.CancelFunc
	cancel     context.CancelFunc
	running    bool
	ready      chan struct{}
	failed     chan error
	generation uint64
	renewEvery time.Duration
	renewOnce  sync.Once
}

func NewOwnerRuntime(address string, roots *x509.CertPool, store *sharestate.Store, manager *sharestate.TLSManager, handler http.Handler) (*OwnerRuntime, error) {
	if address == "" || store == nil || manager == nil || handler == nil {
		return nil, errors.New("Owner runtime dependencies are required")
	}
	baseCtx, stopBase := context.WithCancel(context.Background())
	return &OwnerRuntime{Address: address, RootCAs: roots, Store: store, TLS: manager, Handler: handler, baseCtx: baseCtx, stopBase: stopBase, renewEvery: 6 * time.Hour}, nil
}

func (r *OwnerRuntime) EnsureReady(ctx context.Context) error {
	if err := r.Store.EnsureEndpoint(); err != nil {
		return err
	}
	r.ensureRenewalLoop()
	r.mu.Lock()
	needsProvisioning := !r.Store.CertificateUsable(time.Now())
	if !r.running || needsProvisioning {
		if r.cancel != nil {
			r.cancel()
		}
		r.startLocked()
	}
	ready := r.ready
	failed := r.failed
	r.mu.Unlock()
	select {
	case <-ready:
		return nil
	case err := <-failed:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (r *OwnerRuntime) StartIfActive() {
	if !r.Store.HasActiveGrants() {
		return
	}
	r.ensureRenewalLoop()
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.running {
		r.startLocked()
	}
}

func (r *OwnerRuntime) StopIfInactive() {
	if r.Store.HasActiveGrants() {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.cancel != nil {
		r.cancel()
	}
	r.running = false
}

func (r *OwnerRuntime) Stop() {
	r.mu.Lock()
	if r.cancel != nil {
		r.cancel()
	}
	r.stopBase()
	r.running = false
	r.mu.Unlock()
}

func (r *OwnerRuntime) startLocked() {
	r.generation++
	generation := r.generation
	ctx, cancel := context.WithCancel(r.baseCtx)
	r.cancel = cancel
	r.running = true
	r.ready = make(chan struct{})
	r.failed = make(chan error, 1)
	ready := r.ready
	failed := r.failed
	var readyOnce sync.Once
	signalReady := func() { readyOnce.Do(func() { close(ready) }) }
	snapshot := r.Store.Snapshot()
	connector := OwnerConnector{
		Address:      r.Address,
		EndpointKey:  snapshot.Endpoint.PrivateKey,
		RootCAs:      r.RootCAs,
		RetryMinimum: time.Second,
		RetryMaximum: 30 * time.Second,
		OnConnected: func() {
			if r.Store.CertificateUsable(time.Now()) {
				signalReady()
			}
		},
		HandleControl: func(control io.ReadWriter) error {
			if !r.Store.CertificateUsable(time.Now()) {
				if err := ProvisionCertificate(ctx, control, r.Store, r.TLS); err != nil {
					select {
					case failed <- err:
					default:
					}
					return err
				}
			} else if err := SignalSessionReady(control); err != nil {
				return err
			}
			signalReady()
			return nil
		},
		HandleStream: func(conn net.Conn) {
			_ = ServeApplicationTLS(conn, &tls.Config{MinVersion: tls.VersionTLS13, GetCertificate: r.TLS.GetCertificate}, r.Handler)
		},
	}
	go func() {
		_ = connector.Run(ctx)
		r.mu.Lock()
		if r.generation == generation {
			r.running = false
		}
		r.mu.Unlock()
	}()
}

func (r *OwnerRuntime) renewalLoop() {
	ticker := time.NewTicker(r.renewEvery)
	defer ticker.Stop()
	for {
		select {
		case <-r.baseCtx.Done():
			return
		case now := <-ticker.C:
			r.reconcile(now)
		}
	}
}

func (r *OwnerRuntime) ensureRenewalLoop() {
	r.renewOnce.Do(func() { go r.renewalLoop() })
}

func (r *OwnerRuntime) reconcile(now time.Time) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.Store.HasActiveGrants() {
		if r.cancel != nil {
			r.cancel()
		}
		r.running = false
		return
	}
	if !r.running || !r.Store.CertificateUsable(now) {
		if r.cancel != nil {
			r.cancel()
		}
		r.startLocked()
	}
}

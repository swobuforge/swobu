package sharetransport

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
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

	mu               sync.Mutex
	baseCtx          context.Context
	stopBase         context.CancelFunc
	cancel           context.CancelFunc
	running          bool
	readiness        *readinessResult
	generation       uint64
	activeReady      int
	replacementEvery time.Duration
	replacementOnce  sync.Once
	now              func() time.Time
}

type readinessResult struct {
	done chan struct{}
	once sync.Once
	err  error
}

func newReadinessResult() *readinessResult    { return &readinessResult{done: make(chan struct{})} }
func (r *readinessResult) complete(err error) { r.once.Do(func() { r.err = err; close(r.done) }) }
func (r *readinessResult) terminal() bool {
	select {
	case <-r.done:
		return true
	default:
		return false
	}
}

type ReadyLease struct {
	runtime *OwnerRuntime
	once    sync.Once
}

func (l *ReadyLease) Release() {
	if l == nil || l.runtime == nil {
		return
	}
	l.once.Do(l.runtime.releaseReady)
}

func NewOwnerRuntime(address string, roots *x509.CertPool, store *sharestate.Store, manager *sharestate.TLSManager, handler http.Handler) (*OwnerRuntime, error) {
	if address == "" || store == nil || manager == nil || handler == nil {
		return nil, errors.New("Owner runtime dependencies are required")
	}
	baseCtx, stopBase := context.WithCancel(context.Background())
	return &OwnerRuntime{Address: address, RootCAs: roots, Store: store, TLS: manager, Handler: handler, baseCtx: baseCtx, stopBase: stopBase, replacementEvery: 6 * time.Hour, now: time.Now}, nil
}

func (r *OwnerRuntime) EnsureReady(ctx context.Context) (*ReadyLease, error) {
	if err := r.Store.EnsureEndpoint(); err != nil {
		return nil, err
	}
	now := r.now()
	certificate := r.Store.CertificateState(now)
	if !certificate.Valid && !certificate.CanAttempt(now) {
		return nil, fmt.Errorf("certificate provisioning is temporarily unavailable; retry after %s", certificate.RetryAt.UTC().Format(time.RFC3339))
	}
	r.ensureReplacementLoop()
	r.mu.Lock()
	r.activeReady++
	lease := &ReadyLease{runtime: r}
	certificate = r.Store.CertificateState(r.now())
	restart := !r.running
	if r.running && !certificate.Valid && certificate.CanAttempt(r.now()) && r.readiness != nil && r.readiness.terminal() {
		restart = true
	}
	if restart {
		if r.cancel != nil {
			r.cancel()
		}
		r.startLocked()
	}
	result := r.readiness
	r.mu.Unlock()
	select {
	case <-result.done:
		if result.err != nil {
			lease.Release()
			return nil, result.err
		}
		return lease, nil
	case <-ctx.Done():
		lease.Release()
		return nil, ctx.Err()
	}
}

func (r *OwnerRuntime) StartIfActive() {
	if !r.Store.HasActiveGrants() {
		return
	}
	r.ensureReplacementLoop()
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.running {
		r.startLocked()
	}
}

func (r *OwnerRuntime) StopIfInactive() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.stopIfUnneededLocked()
}

func (r *OwnerRuntime) releaseReady() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.activeReady > 0 {
		r.activeReady--
	}
	r.stopIfUnneededLocked()
}

func (r *OwnerRuntime) stopIfUnneededLocked() {
	if r.activeReady > 0 || r.Store.HasActiveGrants() {
		return
	}
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
	r.readiness = newReadinessResult()
	result := r.readiness
	signalReady := func() { result.complete(nil) }
	snapshot := r.Store.Snapshot()
	connector := OwnerConnector{
		Address: r.Address, IdentityPrivateKey: snapshot.Endpoint.IdentityPrivateKey, RootCAs: r.RootCAs,
		RetryMinimum: time.Second, RetryMaximum: 30 * time.Second,
		OnConnected: func() {
			if r.Store.CertificateState(r.now()).Valid {
				signalReady()
			}
		},
		HandleControl: func(control io.ReadWriter) error {
			now := r.now()
			certificate := r.Store.CertificateState(now)
			if !certificate.Valid && !certificate.CanAttempt(now) {
				return fmt.Errorf("certificate provisioning is temporarily unavailable; retry after %s", certificate.RetryAt.UTC().Format(time.RFC3339))
			}
			if certificate.Due && certificate.CanAttempt(now) {
				if err := ProvisionCertificate(ctx, control, r.Store, r.TLS); err != nil {
					retryAfter := time.Duration(0)
					var provisionErr *CertificateProvisionError
					if errors.As(err, &provisionErr) {
						retryAfter = provisionErr.RetryAfter
					}
					if _, persistErr := r.Store.RecordCertificateFailure(now, retryAfter); persistErr != nil {
						err = errors.Join(err, persistErr)
					}
					if !certificate.Valid {
						result.complete(err)
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
		err := connector.Run(ctx)
		result.complete(err)
		r.mu.Lock()
		if r.generation == generation {
			r.running = false
		}
		r.mu.Unlock()
	}()
}

func (r *OwnerRuntime) replacementLoop() {
	ticker := time.NewTicker(r.replacementEvery)
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

func (r *OwnerRuntime) ensureReplacementLoop() {
	r.replacementOnce.Do(func() { go r.replacementLoop() })
}

func (r *OwnerRuntime) reconcile(now time.Time) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.Store.HasActiveGrants() && r.activeReady == 0 {
		r.stopIfUnneededLocked()
		return
	}
	certificate := r.Store.CertificateState(now)
	needsAttempt := certificate.Due && certificate.CanAttempt(now)
	if !r.running || needsAttempt {
		if r.cancel != nil {
			r.cancel()
		}
		r.startLocked()
	}
}

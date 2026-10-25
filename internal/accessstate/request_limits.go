package accessstate

import (
	"errors"
	"fmt"
	"math"
	"slices"
	"strings"
	"time"

	"github.com/swobuforge/swobu/internal/domain/accessidentity"
	"github.com/swobuforge/swobu/internal/routing"
)

// RequestRateLimit applies to one Principal/Workspace/Service relation. It
// counts downstream operations (not tokens, dollars, concurrent requests or
// upstream attempts). Missing Services are unlimited. A zero quota is never
// accepted: revoke Service access to prohibit use.
type RequestRateLimit struct {
	Service           routing.ServiceKind `json:"service"`
	RequestsPerMinute uint32              `json:"requests_per_minute"`
}

const maxRequestsPerMinute = 100000

var ErrRequestRateLimited = errors.New("Principal Service request rate exceeded")

// canonicalRequestRates returns limits in Service order so semantically
// equivalent reviews and retries bind the same intent and disk representation.
// A non-nil empty slice is an explicit clear when editing a grant.
func canonicalRequestRates(rates []RequestRateLimit, services []routing.ServiceKind) ([]RequestRateLimit, error) {
	if rates == nil {
		return nil, nil
	}
	seen := make(map[routing.ServiceKind]RequestRateLimit, len(rates))
	for _, rate := range rates {
		if _, err := routing.ParseServiceKind(string(rate.Service)); err != nil {
			return nil, fmt.Errorf("request limits: %w", err)
		}
		if rate.RequestsPerMinute < 1 || rate.RequestsPerMinute > maxRequestsPerMinute {
			return nil, errors.New("request limits must be between 1 and 100000 requests per minute")
		}
		if services != nil && !slices.Contains(services, rate.Service) {
			return nil, errors.New("request limit names a Service excluded from this grant")
		}
		if _, exists := seen[rate.Service]; exists {
			return nil, errors.New("duplicate request limit Service")
		}
		seen[rate.Service] = rate
	}
	out := make([]RequestRateLimit, 0, len(seen))
	for _, kind := range []routing.ServiceKind{routing.ServiceModels, routing.ServiceMCP, routing.ServiceSearch} {
		if rate, ok := seen[kind]; ok {
			out = append(out, rate)
		}
	}
	return out, nil
}

func requestRateFingerprint(rates []RequestRateLimit) string {
	if rates == nil {
		return ""
	}
	var result strings.Builder
	result.WriteString("\x00request_limits:")
	for _, r := range rates {
		fmt.Fprintf(&result, "%s=%d;", r.Service, r.RequestsPerMinute)
	}
	return result.String()
}

type requestRateKey struct {
	principal accessidentity.ID
	workspace routing.WorkspaceSlug
	service   routing.ServiceKind
}
type requestRateBucket struct {
	credits float64
	last    time.Time
}

// AdmitCredentialOperation rechecks the *current* permission and charges one
// operation across all credentials of the Principal. Locking makes concurrent
// admissions obey a single ceiling. Credits replenish continuously over 60 s;
// denied operations never consume credit or reach upstream. This is a live
// per-daemon safety ceiling, not a billable or durable usage record.
func (s *Store) AdmitCredentialOperation(id CredentialID, workspace routing.WorkspaceSlug, service routing.ServiceKind) (time.Duration, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	resolution, exists := s.byCredential[id]
	if !exists || resolution.Workspace != workspace {
		return 0, ErrUnauthorized
	}
	index, exists := s.byAccess[resolution.AccessID]
	if !exists || s.state[index].RevokedAt != nil || !permissionAllowsService(s.state[index], workspace, service, s.now()) {
		return 0, ErrUnauthorized
	}
	var limit uint32
	for _, permission := range s.state[index].Permissions {
		if permission.Workspace != workspace {
			continue
		}
		for _, candidate := range permission.RequestLimits {
			if candidate.Service == service {
				limit = candidate.RequestsPerMinute
				break
			}
		}
		break
	}
	if limit == 0 {
		return 0, nil
	}
	now := s.now()
	key := requestRateKey{principal: resolution.AccessID, workspace: workspace, service: service}
	if s.rateBuckets == nil {
		s.rateBuckets = make(map[requestRateKey]requestRateBucket)
	}
	bucket, exists := s.rateBuckets[key]
	if !exists {
		bucket = requestRateBucket{credits: float64(limit), last: now}
	}
	if now.After(bucket.last) {
		bucket.credits = math.Min(float64(limit), bucket.credits+now.Sub(bucket.last).Seconds()*float64(limit)/60)
		bucket.last = now
	}
	// Lowering a limit never grants credit from the former higher ceiling.
	bucket.credits = math.Min(bucket.credits, float64(limit))
	if bucket.credits < 1 {
		wait := time.Duration(math.Ceil((1 - bucket.credits) * float64(time.Minute) / float64(limit)))
		if wait < time.Second {
			wait = time.Second
		}
		s.rateBuckets[key] = bucket
		return wait, ErrRequestRateLimited
	}
	bucket.credits--
	s.rateBuckets[key] = bucket
	return 0, nil
}

// Publication owns bucket retention as well as policy. Keep at most one bucket
// per currently limited relationship/Service, with no revoked-identity tombstones
// or timing-based eviction that could refill a still-active quota early.
func (s *Store) pruneRequestRateBuckets() {
	for key := range s.rateBuckets {
		index, exists := s.byAccess[key.principal]
		keep := false
		if exists && s.state[index].RevokedAt == nil {
			for _, permission := range s.state[index].Permissions {
				if permission.Workspace == key.workspace {
					for _, limit := range permission.RequestLimits {
						keep = keep || limit.Service == key.service
					}
				}
			}
		}
		if !keep {
			delete(s.rateBuckets, key)
		}
	}
}

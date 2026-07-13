package routing

import (
	"testing"
	"time"
)

func TestDefaultRouteRules(t *testing.T) {
	rules := DefaultRouteRules()
	if rules.Timeout != 12*time.Second {
		t.Errorf("Timeout = %v, want 12s", rules.Timeout)
	}
	if rules.Fallback != FallbackBeforeStream {
		t.Errorf("Fallback = %v, want before_stream", rules.Fallback)
	}
	if !rules.SkipUnfit {
		t.Error("SkipUnfit = false, want true")
	}
	if rules.Cooldown != CooldownAuto {
		t.Errorf("Cooldown = %v, want auto", rules.Cooldown)
	}
	for _, fc := range []FailureClass{FailureTimeout, FailureRateLimited, FailureServerError, FailureOverloaded, FailureNetwork} {
		if !rules.RetryableClasses[fc] {
			t.Errorf("RetryableClasses[%s] = false, want true", fc)
		}
	}
	if rules.RetryableClasses[FailureBadRequest] {
		t.Error("RetryableClasses[bad_request] = true, want false")
	}
	if rules.RetryableClasses[FailureAuth] {
		t.Error("RetryableClasses[auth] = true, want false")
	}
}

func TestFailureClass_IsRetryable(t *testing.T) {
	tests := []struct {
		class FailureClass
		want  bool
	}{
		{FailureTimeout, true},
		{FailureRateLimited, true},
		{FailureServerError, true},
		{FailureOverloaded, true},
		{FailureNetwork, true},
		{FailureBadRequest, false},
		{FailureAuth, false},
		{FailureForbidden, false},
		{FailureUnsupported, false},
		{FailureUnknown, false},
	}
	for _, tt := range tests {
		t.Run(string(tt.class), func(t *testing.T) {
			if got := tt.class.IsRetryable(); got != tt.want {
				t.Errorf("IsRetryable() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestRouteState_String(t *testing.T) {
	if string(RouteIncomplete) != "incomplete" {
		t.Errorf("RouteIncomplete = %q", RouteIncomplete)
	}
	if string(RouteDisabled) != "disabled" {
		t.Errorf("RouteDisabled = %q", RouteDisabled)
	}
	if string(RouteUsable) != "usable" {
		t.Errorf("RouteUsable = %q", RouteUsable)
	}
	if string(RouteBlocked) != "blocked" {
		t.Errorf("RouteBlocked = %q", RouteBlocked)
	}
	if string(RouteDegraded) != "degraded" {
		t.Errorf("RouteDegraded = %q", RouteDegraded)
	}
}

func TestTargetState_String(t *testing.T) {
	if string(TargetUsable) != "usable" {
		t.Errorf("TargetUsable = %q", TargetUsable)
	}
	if string(TargetDisabled) != "disabled" {
		t.Errorf("TargetDisabled = %q", TargetDisabled)
	}
	if string(TargetAuthMissing) != "auth_missing" {
		t.Errorf("TargetAuthMissing = %q", TargetAuthMissing)
	}
	if string(TargetCoolingDown) != "cooling_down" {
		t.Errorf("TargetCoolingDown = %q", TargetCoolingDown)
	}
}

func TestCanonicalRequest_ZeroValue(t *testing.T) {
	cr := CanonicalRequest{}
	if cr.Model != "" {
		t.Error("zero-value Model should be empty")
	}
	if cr.EstimatedInputTokens != 0 {
		t.Error("zero-value EstimatedInputTokens should be 0")
	}
}

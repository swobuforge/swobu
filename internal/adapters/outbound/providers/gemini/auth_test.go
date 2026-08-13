package gemini

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"cloud.google.com/go/auth"
)

type fakeADC struct {
	tokens       []string
	tokenCalls   int
	quotaProject string
	quotaErr     error
}

func (f *fakeADC) Token(context.Context) (*auth.Token, error) {
	if f.tokenCalls >= len(f.tokens) {
		return nil, errors.New("no token")
	}
	value := f.tokens[f.tokenCalls]
	f.tokenCalls++
	return &auth.Token{Value: value}, nil
}

func (f *fakeADC) QuotaProjectID(context.Context) (string, error) {
	return f.quotaProject, f.quotaErr
}

type countingCredentialResolver struct{ calls int }

func (r *countingCredentialResolver) ResolveCredential(context.Context, string, string) (string, error) {
	r.calls++
	return "explicit-key", nil
}

func TestResolveAuthSelectsOnlyCredentialReferenceOrADC(t *testing.T) {
	resolver := &countingCredentialResolver{}
	ambient := &fakeADC{tokens: []string{"adc-token"}, quotaProject: "quota-project"}
	detectCalls := 0
	runtime := newRuntime(nil, resolver, func(context.Context) (adcCredentials, error) {
		detectCalls++
		return ambient, nil
	})
	implementation := runtime.BackendResolver.(backendResolver).runtime

	explicitTarget := geminiTarget()
	explicit, err := implementation.resolveAuth(context.Background(), explicitTarget)
	if err != nil || explicit.kind != authAPIKey || explicit.credential != "explicit-key" {
		t.Fatalf("explicit auth = %#v err=%v", explicit, err)
	}
	if detectCalls != 0 || resolver.calls != 1 {
		t.Fatalf("explicit selection calls: ADC=%d resolver=%d", detectCalls, resolver.calls)
	}

	ambientTarget := geminiTarget()
	ambientTarget.CredentialRef = ""
	resolved, err := implementation.resolveAuth(context.Background(), ambientTarget)
	if err != nil || resolved.kind != authADC || resolved.credential != "adc-token" || resolved.quotaProject != "quota-project" {
		t.Fatalf("ambient auth = %#v err=%v", resolved, err)
	}
	if detectCalls != 1 || resolver.calls != 1 {
		t.Fatalf("ambient selection calls: ADC=%d resolver=%d", detectCalls, resolver.calls)
	}
}

func TestResolveADCRetriesDetectionAndReusesSuccessfulCredentials(t *testing.T) {
	detectCalls := 0
	ambient := &fakeADC{tokens: []string{"first-token", "refreshed-token"}}
	runtime := newRuntime(nil, nil, func(context.Context) (adcCredentials, error) {
		detectCalls++
		if detectCalls == 1 {
			return nil, errors.New("not configured")
		}
		return ambient, nil
	}).BackendResolver.(backendResolver).runtime

	if _, err := runtime.resolveADC(context.Background()); err == nil || !strings.Contains(err.Error(), "ADC) is unavailable") {
		t.Fatalf("first detection error = %v", err)
	}
	first, err := runtime.resolveADC(context.Background())
	if err != nil || first.credential != "first-token" {
		t.Fatalf("retry auth = %#v err=%v", first, err)
	}
	second, err := runtime.resolveADC(context.Background())
	if err != nil || second.credential != "refreshed-token" {
		t.Fatalf("cached credential refresh = %#v err=%v", second, err)
	}
	if detectCalls != 2 || ambient.tokenCalls != 2 {
		t.Fatalf("calls: detect=%d token=%d", detectCalls, ambient.tokenCalls)
	}
}

func TestResolveADCOmitsMissingOrFailingQuotaProject(t *testing.T) {
	for name, adc := range map[string]*fakeADC{
		"missing": {tokens: []string{"token"}},
		"error":   {tokens: []string{"token"}, quotaErr: errors.New("unavailable")},
	} {
		t.Run(name, func(t *testing.T) {
			runtime := newRuntime(nil, nil, func(context.Context) (adcCredentials, error) { return adc, nil }).BackendResolver.(backendResolver).runtime
			resolved, err := runtime.resolveADC(context.Background())
			if err != nil || resolved.quotaProject != "" {
				t.Fatalf("resolved = %#v err=%v", resolved, err)
			}
		})
	}
}

func TestApplyAuthProjectsExactlyOneCredentialForm(t *testing.T) {
	for name, test := range map[string]struct {
		auth                  resolvedAuth
		apiKey, bearer, quota string
	}{
		"API key": {auth: resolvedAuth{kind: authAPIKey, credential: "key"}, apiKey: "key"},
		"ADC":     {auth: resolvedAuth{kind: authADC, credential: "token", quotaProject: "quota"}, bearer: "Bearer token", quota: "quota"},
	} {
		t.Run(name, func(t *testing.T) {
			request, _ := http.NewRequest(http.MethodGet, "https://example.test", nil)
			applyAuth(request, test.auth)
			if request.Header.Get("x-goog-api-key") != test.apiKey || request.Header.Get("Authorization") != test.bearer || request.Header.Get("x-goog-user-project") != test.quota {
				t.Fatalf("headers = %#v", request.Header)
			}
		})
	}
}

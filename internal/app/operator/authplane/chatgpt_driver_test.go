package authplane

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/swobuforge/swobu/internal/app/operator/chatgptlogin"
)

func TestChatGPTMethodDriverStartAndPoll(t *testing.T) {
	t.Parallel()
	tokenSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"access_token":"at_ok"}`))
	}))
	defer tokenSrv.Close()

	svc := chatgptlogin.NewService(http.DefaultClient, chatgptlogin.ServiceConfig{TokenURL: tokenSrv.URL})
	driver, err := NewChatGPTMethodDriver(svc)
	if err != nil {
		t.Fatalf("NewChatGPTMethodDriver error: %v", err)
	}
	start, err := driver.Start(context.Background(), StartInput{ProviderSpec: "chatgpt"})
	if err != nil {
		t.Fatalf("Start error: %v", err)
	}
	u, _ := url.Parse(start.AuthorizeURL)
	state := u.Query().Get("state")
	req := httptest.NewRequest(http.MethodGet, "/_swobu/auth/chatgpt/callback?state="+url.QueryEscape(state)+"&code=abc", nil)
	rec := httptest.NewRecorder()
	svc.HandleCallback(rec, req)
	poll, err := driver.Poll(context.Background(), start.SessionID)
	if err != nil {
		t.Fatalf("Poll error: %v", err)
	}
	if poll.State != SessionStateSucceeded {
		t.Fatalf("state = %q", poll.State)
	}
}

func TestChatGPTMethodDriverRejectsOtherProvider(t *testing.T) {
	t.Parallel()
	svc := chatgptlogin.NewService(nil, chatgptlogin.ServiceConfig{})
	driver, _ := NewChatGPTMethodDriver(svc)
	if _, err := driver.Start(context.Background(), StartInput{ProviderSpec: "qwen"}); err == nil {
		t.Fatal("expected unsupported provider error")
	}
}

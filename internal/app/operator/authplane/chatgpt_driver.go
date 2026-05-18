package authplane

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/swobuforge/swobu/internal/app/operator/chatgptlogin"
)

const ChatGPTProviderSpec = "chatgpt"

// ChatGPTAuthMethodDriver adapts the chatgpt login service to authplane
// lifecycle semantics.
type ChatGPTAuthMethodDriver struct {
	service *chatgptlogin.LoginService
}

func NewChatGPTAuthMethodDriver(service *chatgptlogin.LoginService) (*ChatGPTAuthMethodDriver, error) {
	if service == nil {
		return nil, fmt.Errorf("chatgpt login service is required")
	}
	return &ChatGPTAuthMethodDriver{service: service}, nil
}

func (d *ChatGPTAuthMethodDriver) Start(ctx context.Context, in StartInput) (DriverStartResult, error) {
	if strings.ToLower(strings.TrimSpace(in.ProviderSpec)) != ChatGPTProviderSpec { // swobu:io-string source=boundary
		return DriverStartResult{}, fmt.Errorf("provider spec %q is unsupported", strings.TrimSpace(in.ProviderSpec)) // swobu:io-string source=boundary
	}
	start, err := d.service.Start(ctx, chatgptlogin.StartInput{
		AuthMode: strings.TrimSpace(in.AuthMode), // swobu:io-string source=boundary
	})
	if err != nil {
		return DriverStartResult{}, err
	}
	return DriverStartResult{
		SessionID:    start.SessionID,
		AuthorizeURL: start.AuthorizeURL,
		UserCode:     strings.TrimSpace(start.UserCode), // swobu:io-string source=boundary
		ExpiresAt:    start.ExpiresAt.UTC().Format(time.RFC3339),
	}, nil
}

func (d *ChatGPTAuthMethodDriver) Poll(ctx context.Context, sessionID string) (DriverPollResult, error) {
	out, err := d.service.Session(ctx, sessionID)
	if err != nil {
		return DriverPollResult{}, err
	}
	state := SessionState(strings.ToLower(strings.TrimSpace(string(out.State)))) // swobu:io-string source=boundary
	return DriverPollResult{
		State:         state,
		CredentialRef: strings.TrimSpace(out.CredentialRef), // swobu:io-string source=boundary
		ErrorMessage:  strings.TrimSpace(out.ErrorMessage),  // swobu:io-string source=boundary
	}, nil
}

func (d *ChatGPTAuthMethodDriver) Cancel(_ context.Context, sessionID string) error {
	return d.service.Cancel(sessionID)
}

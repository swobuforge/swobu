package authplane

import (
	"context"
	"fmt"
	"strings"

	"github.com/swobuforge/swobu/internal/app/operator/chatgptlogin"
)

const ChatGPTProviderSpec = "chatgpt"

// ChatGPTMethodDriver adapts the chatgpt login service to authplane
// lifecycle semantics.
type ChatGPTMethodDriver struct {
	service *chatgptlogin.Service
}

func NewChatGPTMethodDriver(service *chatgptlogin.Service) (*ChatGPTMethodDriver, error) {
	if service == nil {
		return nil, fmt.Errorf("chatgpt login service is required")
	}
	return &ChatGPTMethodDriver{service: service}, nil
}

func (d *ChatGPTMethodDriver) Start(ctx context.Context, in StartInput) (DriverStartResult, error) {
	if strings.ToLower(strings.TrimSpace(in.ProviderSpec)) != ChatGPTProviderSpec {
		return DriverStartResult{}, fmt.Errorf("provider spec %q is unsupported", strings.TrimSpace(in.ProviderSpec))
	}
	start, err := d.service.Start(ctx, chatgptlogin.StartInput{
		AuthMode: strings.TrimSpace(in.AuthMode),
	})
	if err != nil {
		return DriverStartResult{}, err
	}
	return DriverStartResult{
		SessionID:    start.SessionID,
		AuthorizeURL: start.AuthorizeURL,
		UserCode:     strings.TrimSpace(start.UserCode),
	}, nil
}

func (d *ChatGPTMethodDriver) Poll(ctx context.Context, sessionID string) (DriverPollResult, error) {
	out, err := d.service.Session(ctx, sessionID)
	if err != nil {
		return DriverPollResult{}, err
	}
	state := SessionState(strings.ToLower(strings.TrimSpace(string(out.State))))
	return DriverPollResult{
		State:         state,
		CredentialRef: strings.TrimSpace(out.CredentialRef),
		ErrorMessage:  strings.TrimSpace(out.ErrorMessage),
	}, nil
}

func (d *ChatGPTMethodDriver) Cancel(_ context.Context, _ string) error {
	// chatgpt login sessions currently do not expose cancellation.
	return nil
}

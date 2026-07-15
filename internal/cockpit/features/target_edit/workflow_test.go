package target_edit

import (
	"context"
	"errors"
	"testing"

	"github.com/swobuforge/swobu/internal/cockpit/ports"
	"github.com/swobuforge/swobu/internal/cockpit/readmodel"
	"github.com/swobuforge/swobu/internal/cockpit/testkit"
)

func TestTargetEdit_OpenFormSeedsExistingTarget(t *testing.T) {
	workflow := NewEditWorkflow("dev", targetEditRoute(), targetEditRoute().Targets[0], nil, nil, nil, nil, nil)

	if got, want := workflow.Provider.Get(), "openai_compatible"; got != want {
		t.Fatalf("provider = %q, want %q", got, want)
	}
	if got, want := workflow.Rank.Get(), "1"; got != want {
		t.Fatalf("rank = %q, want %q", got, want)
	}
	if got, want := workflow.Weight.Get(), "1"; got != want {
		t.Fatalf("weight = %q, want %q", got, want)
	}
}

func TestTargetEdit_OpenCreateStartsEmptyWithRouteDefaults(t *testing.T) {
	workflow := NewCreateWorkflow("dev", targetEditRoute(), nil, nil, nil, nil)

	if got := workflow.Provider.Get(); got != "" {
		t.Fatalf("provider = %q, want empty", got)
	}
	if got, want := workflow.Model.Get(), "gpt-4.1"; got != want {
		t.Fatalf("model = %q, want %q", got, want)
	}
	if got, want := workflow.Rank.Get(), "2"; got != want {
		t.Fatalf("rank = %q, want %q", got, want)
	}
}

func TestTargetEdit_SubmitValidationFailure(t *testing.T) {
	workflow := NewCreateWorkflow("dev", targetEditRoute(), nil, nil, nil, nil)

	workflow.Submit(context.Background())
	if got, want := workflow.Error.Get(), "enter a provider"; got != want {
		t.Fatalf("error = %q, want %q", got, want)
	}
	if got := workflow.Phase.Get(); got != PhaseFailed {
		t.Fatalf("phase = %v, want PhaseFailed", got)
	}
}

func TestTargetEdit_SubmitSuccessEmitsSavedTargetAndCloses(t *testing.T) {
	var request ports.SaveTargetRequest
	closed := false
	var saved readmodel.TargetReadModel
	workflow := NewCreateWorkflow("dev", targetEditRoute(), func(_ context.Context, req ports.SaveTargetRequest) (readmodel.TargetReadModel, error) {
		request = req
		return readmodel.TargetReadModel{ID: "target-2", Provider: req.Provider, Model: req.Model, Rank: req.Rank, Weight: req.Weight}, nil
	}, nil, func(target readmodel.TargetReadModel) {
		saved = target
	}, func() {
		closed = true
	})
	workflow.Provider.Set("openai_compatible")
	workflow.BaseURL.Set("https://api.example/v1")
	workflow.Credential.Set("env:API_KEY")

	workflow.Submit(context.Background())

	if request.TargetID != "" {
		t.Fatalf("create target id = %q, want empty", request.TargetID)
	}
	if request.Rank != 2 || request.Weight != 1 {
		t.Fatalf("rank/weight = %d/%d, want 2/1", request.Rank, request.Weight)
	}
	if saved.ID != "target-2" || !closed {
		t.Fatalf("saved=%#v closed=%v, want saved target and closed", saved, closed)
	}
}

func TestTargetEdit_RankWeightValidation(t *testing.T) {
	workflow := NewEditWorkflow("dev", targetEditRoute(), targetEditRoute().Targets[0], nil, nil, nil, nil, nil)
	workflow.Rank.Set("0")

	if _, err := workflow.SaveRequest(); err == nil || err.Error() != "rank must be at least 1" {
		t.Fatalf("rank error = %v, want rank validation", err)
	}

	workflow.Rank.Set("1")
	workflow.Weight.Set("nope")
	if _, err := workflow.SaveRequest(); err == nil || err.Error() != "weight must be at least 1" {
		t.Fatalf("weight error = %v, want weight validation", err)
	}
}

func TestTargetEdit_DeleteConfirmationRequiresTwoEnters(t *testing.T) {
	deleteErr := errors.New("delete should not be called before confirm")
	workflow := NewEditWorkflow("dev", targetEditRoute(), targetEditRoute().Targets[0], nil, func(context.Context, ports.DeleteTargetRequest) error {
		return deleteErr
	}, nil, nil, nil)

	workflow.ActivateDelete()
	if got := workflow.Phase.Get(); got != PhaseConfirmingDelete {
		t.Fatalf("phase after first delete = %v, want PhaseConfirmingDelete", got)
	}
	if workflow.Error.Get() != "" {
		t.Fatalf("delete error after arm = %q, want empty", workflow.Error.Get())
	}

	workflow.ActivateDelete()
	if got := workflow.Error.Get(); got != deleteErr.Error() {
		t.Fatalf("delete error after confirm = %q, want %q", got, deleteErr.Error())
	}
}

func TestTargetEdit_CancelClosesForm(t *testing.T) {
	closed := false
	workflow := NewEditWorkflow("dev", targetEditRoute(), targetEditRoute().Targets[0], nil, nil, nil, nil, func() {
		closed = true
	})

	if !workflow.Back() {
		t.Fatal("Back should close open workflow")
	}
	if !closed {
		t.Fatal("workflow did not call OnClose")
	}
}

func TestTargetEdit_GenericProviderRows(t *testing.T) {
	tests := []struct {
		name       string
		provider   string
		baseURL    string
		credential string
	}{
		{name: "openai_compatible", provider: "openai_compatible", baseURL: "https://api.example/v1", credential: "env:API_KEY"},
		{name: "bedrock", provider: "bedrock"},
		{name: "chatgpt_browser", provider: "chatgpt", credential: "browser"},
		{name: "chatgpt_device", provider: "chatgpt", credential: "device:ABCD-EFGH"},
		{name: "azure", provider: "azure", baseURL: "contact-8837-resource", credential: "env:AZURE_OPENAI_API_KEY"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			workflow := workflowForProvider(tt.provider, tt.baseURL, tt.credential)
			rendered := testkit.RenderTrimmed(workflow.Render(nil), 100, 14)
			testkit.AssertVisual(tt.name).
				Fixture("testdata/target_edit_vendor_rows/fixture/"+tt.name+".txt").
				Viewport(100, 14).
				Now(t, rendered)
		})
	}
}

func workflowForProvider(provider string, baseURL string, credential string) *Workflow {
	route := targetEditRoute()
	target := route.Targets[0]
	target.Provider = provider
	target.BaseURL = baseURL
	target.CredentialRef = credential
	return NewEditWorkflow("dev", route, target, nil, nil, nil, nil, nil)
}

func targetEditRoute() readmodel.RouteReadModel {
	return readmodel.RouteReadModel{
		ID:        "gpt-4.1",
		ModelName: "gpt-4.1",
		Targets: []readmodel.TargetReadModel{{
			ID:            "target-1",
			Name:          "fast",
			Provider:      "openai_compatible",
			Model:         "gpt-4.1",
			BaseURL:       "https://api.example/v1",
			CredentialRef: "env:API_KEY",
			Rank:          1,
			Weight:        1,
		}},
	}
}

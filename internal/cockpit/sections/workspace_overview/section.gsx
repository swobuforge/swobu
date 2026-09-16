package workspace_overview

import (
	"context"
	"net/url"
	"time"
	tui "github.com/grindlemire/go-tui"
	"github.com/swobuforge/swobu/internal/app/operator/shares"
	workspace_delete "github.com/swobuforge/swobu/internal/cockpit/features/workspace_delete"
	workspace_edit "github.com/swobuforge/swobu/internal/cockpit/features/workspace_edit"
	"github.com/swobuforge/swobu/internal/cockpit/ports"
	"github.com/swobuforge/swobu/internal/cockpit/readmodel"
	"github.com/swobuforge/swobu/internal/cockpit/ui"
	"github.com/swobuforge/swobu/internal/sharestate"
)

// ---------------------------------------------------------------------------
// Section state
// ---------------------------------------------------------------------------

type SectionView struct {
	Model                    readmodel.WorkspaceReadModel
	Expanded                 *tui.State[bool]
	RenameWorkspace          workspace_edit.RenameFunc
	DeleteWorkspace          workspace_delete.DeleteFunc
	OnWorkspaceSaved         func(readmodel.WorkspaceReadModel)
	OnWorkspaceDeleted       func(readmodel.WorkspaceID)
	OnWorkspaceDiscarded     func()
	ShareCommands            ports.ShareCommands
	SharePending             *tui.State[bool]
	ShareCopied              *tui.State[bool]
	ShareFeedback            *tui.State[shareFeedback]
	WorkspaceWarning         *tui.State[string]
	app                      *tui.App
	shareCopyGeneration      uint64
	// PendingDeleteWorkspaceID seeds the delete confirmation child while the
	// delete row is armed. The parent keeps the request here so Back() can clear
	// it without holding a persistent child reference.
	PendingDeleteWorkspaceID *tui.State[readmodel.WorkspaceID]
	headerRef *tui.Ref
}

func Section(model readmodel.WorkspaceReadModel, commands ...ports.WorkspaceCommands) *SectionView {
	section := &SectionView{
		Model:                    model,
		Expanded:                 tui.NewState(true),
		PendingDeleteWorkspaceID: tui.NewState(readmodel.WorkspaceID("")),
		SharePending:             tui.NewState(false),
		ShareCopied:              tui.NewState(false),
		ShareFeedback:            tui.NewState(shareFeedback{}),
		WorkspaceWarning:         tui.NewState(""),
		headerRef:                tui.NewRef(),
	}
	if len(commands) > 0 && commands[0] != nil {
		section.RenameWorkspace = commands[0].RenameWorkspace
		section.DeleteWorkspace = commands[0].DeleteWorkspace
	}
	return section
}

// ---------------------------------------------------------------------------
// Lifecycle callbacks
// ---------------------------------------------------------------------------

func (s *SectionView) workspaceSaved(workspace readmodel.WorkspaceReadModel) {
	s.Model = workspace
	s.resetTransientState()
	if s.OnWorkspaceSaved != nil {
		s.OnWorkspaceSaved(workspace)
	}
}

func (s *SectionView) workspaceDeleted(workspaceID readmodel.WorkspaceID) {
	if workspaceID == s.Model.ID {
		// Summary-only rendering keeps the section visible but suppresses
		// destructive actions after the workspace it owned is gone.
	}
	if s.OnWorkspaceDeleted != nil {
		s.OnWorkspaceDeleted(workspaceID)
	}
}

func (s *SectionView) workspaceDiscarded() error {
	if s.OnWorkspaceDiscarded != nil {
		s.OnWorkspaceDiscarded()
	}
	return nil
}

func (s *SectionView) resetTransientState() {
	s.PendingDeleteWorkspaceID.Set("")
	s.SharePending.Set(false)
	s.ShareCopied.Set(false)
	s.ShareFeedback.Set(shareFeedback{})
}

// ---------------------------------------------------------------------------
// Back / navigation
// ---------------------------------------------------------------------------

func (s *SectionView) Back() bool {
	if s.Model.IsDraft() || !s.Expanded.Get() {
		return false
	}
	s.Expanded.Set(false)
	return true
}

func (s *SectionView) deleteIsOpen() bool { return s.PendingDeleteWorkspaceID.Get() != "" }
func (s *SectionView) closeDelete()      { s.PendingDeleteWorkspaceID.Set("") }

func (s *SectionView) OpenDeleteConfirmation(workspaceID readmodel.WorkspaceID) {
	s.PendingDeleteWorkspaceID.Set(workspaceID)
}

// ---------------------------------------------------------------------------
// Feature components
// ---------------------------------------------------------------------------

func WorkspaceEdit(s *SectionView) *workspace_edit.Workflow {
	return workspace_edit.NewWorkflow(
		s.Model,
		s.RenameWorkspace,
		s.workspaceSaved,
	)
}

func DeleteConfirmation(s *SectionView) *workspace_delete.ConfirmationView {
	confirmation := workspace_delete.Confirmation(
		s.Model,
		s.DeleteWorkspace,
		s.workspaceDeleted,
	)
	if s.PendingDeleteWorkspaceID.Get() != "" {
		confirmation.Request(s.PendingDeleteWorkspaceID.Get())
	}
	confirmation.OnArm = func(_ readmodel.WorkspaceID) {
		s.OpenDeleteConfirmation(s.Model.ID)
	}
	confirmation.OnCancel = func(_ readmodel.WorkspaceID) {
		s.closeDelete()
	}
	return confirmation
}

func DraftDiscardComponent(s *SectionView) *ui.ConfirmActionRow {
	copy := ui.ConfirmActionCopy{
		Label: "discard", IdleValue: "setup", IdleAction: "discard ↵",
		ConfirmValue: "discard " + s.Model.Slug + "?", ConfirmAction: "confirm ↵",
		SubmittingValue: "discarding setup…", SubmittingHint: "wait",
		FailedValue: "discard failed", FailedAction: "retry ↵",
	}
	return ui.NewConfirmActionRow("workspace-discard:+", copy, s.workspaceDiscarded)
}

func workspaceShareKey(s *SectionView) string { return "workspace-share:" + workspaceIdentity(s) }

func (s *SectionView) issueShare(expiry sharestate.Expiry) {
	if s.ShareCommands == nil || s.SharePending.Get() { return }
	s.SharePending.Set(true)
	s.ShareCopied.Set(false)
	s.ShareFeedback.Set(shareFeedback{})
	complete := func(result shares.Result, err error) {
		s.SharePending.Set(false)
		if err != nil { s.setShareFailure(err.Error()); return }
		parsed, parseErr := url.Parse(result.ShareURL)
		if parseErr != nil || parsed.Hostname() == "" { s.setShareFailure("share response is invalid"); return }
		share := &readmodel.ShareReadModel{Hostname: parsed.Hostname(), Never: result.ExpiresAt == "never"}
		if !share.Never {
			share.ExpiresAt, parseErr = time.Parse(time.RFC3339, result.ExpiresAt)
			if parseErr != nil { s.setShareFailure("share expiry is invalid"); return }
		}
		s.Model.Share = share
	}
	if s.app == nil { result, err := s.ShareCommands.IssueShare(context.Background(), s.Model.Slug, expiry); complete(result, err); return }
	go func() { result, err := s.ShareCommands.IssueShare(context.Background(), s.Model.Slug, expiry); s.app.QueueUpdate(func(){ complete(result, err) }) }()
}

func (s *SectionView) copyShare() {
	if s.ShareCommands == nil { return }
	s.shareCopyGeneration++
	generation := s.shareCopyGeneration
	result, err := s.ShareCommands.RevealShare(context.Background(), s.Model.Slug)
	if err != nil { s.setShareFailure(err.Error()); return }
	copyResult := ui.CopyToClipboard(result.ShareURL)
	s.ShareCopied.Set(copyResult.Status == ui.CopyOK)
	s.ShareFeedback.Set(shareClipboardFeedback(copyResult))
	if copyResult.Status == ui.CopyOK { s.expireShareCopied(generation) }
}

func (s *SectionView) expireShareCopied(generation uint64) {
	if s.app == nil { return }
	app := s.app
	go func() {
		time.Sleep(1200 * time.Millisecond)
		app.QueueUpdate(func() { s.clearShareCopied(generation) })
	}()
}

func (s *SectionView) clearShareCopied(generation uint64) {
	if s.shareCopyGeneration == generation { s.ShareCopied.Set(false) }
}

func (s *SectionView) revokeShare() error {
	if s.ShareCommands == nil { return nil }
	if err := s.ShareCommands.RevokeShare(context.Background(), s.Model.Slug); err != nil { return err }
	s.Model.Share = nil
	s.ShareCopied.Set(false)
	s.ShareFeedback.Set(shareFeedback{})
	return nil
}

type shareFeedback struct {
	Message string
	Tone    ui.Tone
}

func (s *SectionView) setShareFailure(message string) {
	s.ShareCopied.Set(false)
	s.ShareFeedback.Set(shareFeedback{Message: message, Tone: ui.ToneFailure})
}

func shareClipboardFeedback(result ui.ClipboardResult) shareFeedback {
	switch result.Status {
	case ui.CopyUnavailable:
		return shareFeedback{Message: "clipboard unavailable", Tone: ui.ToneWarning}
	case ui.CopyFailed:
		return shareFeedback{Message: "copy failed", Tone: ui.ToneFailure}
	default:
		return shareFeedback{}
	}
}

func WorkspaceShareRowComponent(s *SectionView) tui.Component {
	props := ui.SelectProps{ID: workspaceShareKey(s), Label: "share"}
	if s.SharePending.Get() {
		props.Value = "setting up HTTPS…"
		props.OnActivate = func() {}
		return ui.NewSelect(props)
	}
	if s.Model.Share != nil {
		value := s.Model.Share.EndpointValue()
		if s.ShareCopied.Get() { value = "copied" }
		props.Value = value
		props.Action = "copy ↵"
		if s.ShareFeedback.Get().Message != "" { props.Action = "retry ↵" }
		props.OnActivate = s.copyShare
		return ui.NewSelect(props)
	}
	props.Value = "not shared"
	props.Action = "share ↵"
	props.Body = func(backout func()) tui.Component {
		options := []ui.ChoiceOption{{ID:string(sharestate.ExpiryOneDay), Label:"1 day"}, {ID:string(sharestate.ExpirySevenDays), Label:"7 days · preview"}, {ID:string(sharestate.ExpiryThirtyDays), Label:"30 days · preview"}, {ID:string(sharestate.ExpiryNever), Label:"until revoked · preview"}}
		return ui.NewChoicePicker(workspaceShareKey(s)+":expiry", options, string(sharestate.ExpiryOneDay), func(value string){ backout(); s.issueShare(sharestate.Expiry(value)) }, backout)
	}
	return ui.NewSelect(props)
}

func WorkspaceShareRevokeComponent(s *SectionView) *ui.ConfirmActionRow {
	copy := ui.ConfirmActionCopy{Label:"expires", IdleValue:s.Model.Share.ExpiryValue(), IdleAction:"revoke ↵", ConfirmValue:"Revoke shared workspace?", ConfirmAction:"revoke ↵", SubmittingValue:"revoking…", SubmittingHint:"wait", FailedValue:"revoke failed", FailedAction:"retry ↵"}
	return ui.NewConfirmActionRow(workspaceShareKey(s)+":revoke", copy, s.revokeShare)
}

// ---------------------------------------------------------------------------
// Mount keys
// ---------------------------------------------------------------------------

func workspaceEditKey(s *SectionView) string {
	return "workspace-edit:" + workspaceIdentity(s)
}

func workspaceDeleteKey(s *SectionView) string {
	return "workspace-delete:" + workspaceIdentity(s)
}

func workspaceIdentity(s *SectionView) string {
	if s.Model.ID != "" {
		return string(s.Model.ID)
	}
	if s.Model.Slug != "" {
		return s.Model.Slug
	}
	return "+"
}

func sectionHeaderKey(s *SectionView) string { return "workspace-header" }

func WorkspaceDisclosureComponent(s *SectionView) tui.Component {
	disclosure := ui.NewSectionDisclosure(sectionHeaderKey(s), "workspace", s.Expanded)
	disclosure.AutoFocus = true
	disclosure.UseRef(s.headerRef)
	return disclosure
}

func (s *SectionView) BackRef() *tui.Ref { return s.headerRef }

func (s *SectionView) UpdateProps(fresh tui.Component) {
	headerRef := s.headerRef
	shareCopyGeneration := s.shareCopyGeneration
	s.updatePropsFields(fresh)
	s.headerRef = headerRef
	s.shareCopyGeneration = shareCopyGeneration
}

// ---------------------------------------------------------------------------
// Section render
// ---------------------------------------------------------------------------

templ (s *SectionView) Render() {
	<div class="flex-col w-full">
		if s.Model.IsDraft() {
			@DraftWorkspaceHeader()
		} else {
			<div key={sectionHeaderKey(s)} class="w-full">
				@WorkspaceDisclosureComponent(s)
			</div>
		}
		if s.Expanded.Get() {
			<div class="pl-3 w-full">
			if s.WorkspaceWarning.Get() != "" {
				<div class="w-full" textStyle={ui.ToneStyle(ui.ToneWarning)}>@ui.FlowText(s.WorkspaceWarning.Get())</div>
			}
			if s.Model.IsDraft() {
				<div key={workspaceEditKey(s)} class="w-full">
					@WorkspaceEdit(s)
				</div>
					if s.Model.Slug != "" {
						<div key={"workspace-discard:+"} class="w-full">
							@DraftDiscardComponent(s)
						</div>
					}
				} else if s.Model.IsBootstrap() {
					<div key={endpointRowKey(s)} class="w-full">
						@EndpointRowComponent(s)
					</div>
				} else {
					<div key={workspaceEditKey(s)} class="w-full">
						@WorkspaceEdit(s)
					</div>
					<div key={endpointRowKey(s)} class="w-full">
						@EndpointRowComponent(s)
					</div>
					<div key={workspaceShareKey(s)} class="w-full">
						@WorkspaceShareRowComponent(s)
					</div>
					if s.ShareFeedback.Get().Message != "" {
						<div class="pl-3 w-full" textStyle={ui.ToneStyle(s.ShareFeedback.Get().Tone)}>@ui.FlowText(s.ShareFeedback.Get().Message)</div>
					}
					if s.Model.Share != nil {
						<div class="pl-3 w-full">
							<div key={workspaceShareKey(s)+":revoke"} class="w-full">
								@WorkspaceShareRevokeComponent(s)
							</div>
						</div>
					}
					<div key={workspaceDeleteKey(s)} class="w-full">
						@DeleteConfirmation(s)
					</div>
				}
			</div>
		}
	</div>
}

// ---------------------------------------------------------------------------
// Layout helpers
// ---------------------------------------------------------------------------

templ DraftWorkspaceHeader() {
	<div class="flex-row w-full">
		<span class="w-2"></span>
		<span>new workspace</span>
	</div>
}

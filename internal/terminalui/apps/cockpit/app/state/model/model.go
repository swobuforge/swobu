package model

import "github.com/swobuforge/swobu/internal/profile"

type Model struct {
	HeaderStatus     string
	DaemonState      string
	DaemonHint       string
	ControlPlane     *ControlPlaneMismatch
	InteractionMode  string
	HelpTabOpen      bool
	FooterVerb       string
	FooterBaseVerb   string
	FooterAllowSpace bool
	FooterShowTabs   bool

	Endpoints         []string
	EndpointSnapshots []EndpointSnapshot
	CurrentEndpoint   string

	Catalog                        []CatalogEntry
	CatalogError                   string
	StreamEnabled                  bool
	CreateDraftName                string
	CreateDraftProviderConfig      ProviderConfigSnapshot
	CreateDraftModelDeployments    []profile.ProviderDeploymentRecord
	CreateDraftModelError          string
	CreateDraftModelProbePending   bool
	CreateDraftModelProviderSpec   string
	CreateDraftModelAuthHeader     string
	CreateDraftModelBaseURL        string
	CreateDraftModelCredentialRef  string
	CreateDraftModelTestProtocol   string
	CreateDraftModelTestPassed     bool
	AddModelDraftModelDeployments  []profile.ProviderDeploymentRecord
	AddModelDraftModelError        string
	AddModelDraftModelProbePending bool
	AddModelDraftProviderSpec      string
	AddModelDraftAuthHeader        string
	AddModelDraftProviderProtocol  string
	AddModelDraftBaseURL           string
	AddModelDraftCredentialRef     string
	WorkspaceSaveError             string
	WorkspaceCopyNote              string
	ClientCopyNote                 string
	ClientLaunchNote               string
	ClientAccessStatus             string
	ClientAccessNote               string
	// SelectedClientID is the operator-chosen client preset for the current workspace.
	// Empty means no explicit selection; the view shows a picker.
	SelectedClientID          string
	SaveErrors                map[string]string
	LastStoredKeyProviderSpec string
	LastStoredKeySlotName     string
	// Invariant: AuthSessions payloads are canonicalized at write seams.
	// UI readers must not re-trim session fields.
	AuthSessions map[string]AuthSessionViewState
	TrafficRows  []TrafficRow
	TrafficError string
	// TrafficSectionOffset is the scroll offset for the traffic row viewport.
	TrafficSectionOffset int
	// OpenTrafficRowIDs tracks which traffic rows have their detail disclosure
	// expanded. Key is request_id; value is always true.
	OpenTrafficRowIDs map[string]bool
	// SectionOpen tracks collapsible section open state per section title.
	SectionOpen map[string]bool
	// WorkspaceEditing controls whether the workspace name editor is active.
	WorkspaceEditing bool
	// WorkspaceDraft is the current draft value in the workspace name editor.
	WorkspaceDraft string
	// WorkspaceErrMsg is the validation error for workspace name editing.
	WorkspaceErrMsg string
	// ClientPickerOpen controls whether the client preset picker is visible.
	ClientPickerOpen bool
	// ClientPickerCursor is the selected index in the client picker.
	ClientPickerCursor int
	// ExpandedActionID tracks which client action row has its detail open.
	ExpandedActionID string
	// PayloadScrollOffset is the scroll offset for client payload details.
	PayloadScrollOffset int
	HelpNote            string
}

type AuthSessionViewState struct {
	SessionID    string
	URL          string
	UserCode     string
	SessionState string
	SessionError string
	CopyNote     string
}

type ControlPlaneMismatch struct {
	ExpectedProtocol  int    `json:"expected_protocol"`
	DaemonProtocol    int    `json:"daemon_protocol"`
	HasDaemonProtocol bool   `json:"has_daemon_protocol"`
	TUIVersion        string `json:"tui_version"`
	DaemonVersion     string `json:"daemon_version"`
	Reason            string `json:"reason"`
	RecoveryCommand   string `json:"recovery_command"`
	Note              string `json:"note"`
	NoteAction        string `json:"note_action"`
}

const (
	InteractionModeNAV        = "NAV"
	InteractionModeEditText   = "EDIT_TEXT"
	InteractionModePickOne    = "PICK_ONE"
	InteractionModeManageList = "MANAGE_LIST"
	InteractionModeBusySave   = "BUSY_SAVE"
	InteractionModeBusyLaunch = "BUSY_LAUNCH"
)

type CatalogSnapshot struct {
	Entries []CatalogEntry `json:"entries"`
}

type EndpointSnapshot struct {
	Name                      string                   `json:"name"`
	SelectedProviderConfigRef string                   `json:"selected_provider_config_ref"`
	ProviderConfigs           []ProviderConfigSnapshot `json:"provider_configs"`
}

type ProviderConfigSnapshot struct {
	Ref              string `json:"ref"`
	ProviderSpec     string `json:"provider_spec"`
	Region           string `json:"region,omitempty"`
	BaseURL          string `json:"base_url,omitempty"`
	AuthHeader       string `json:"auth_header,omitempty"`
	CredentialRef    string `json:"credential_ref,omitempty"`
	ModelID          string `json:"model_id,omitempty"`
	TargetAlias      string `json:"target_alias,omitempty"`
	ProviderProtocol string `json:"provider_protocol,omitempty"`
}

type CatalogEntry struct {
	EndpointName      string   `json:"endpoint_name"`
	ProviderConfigRef string   `json:"provider_config_ref"`
	ProviderSpec      string   `json:"provider_spec"`
	ModelIDs          []string `json:"model_ids,omitempty"`
	Error             string   `json:"error,omitempty"`
}

type StatusProjectionSnapshot struct {
	RecentTraffic []TrafficRow `json:"recent_traffic"`
}

type TrafficRow struct {
	RequestID           string        `json:"request_id"`
	OperationFamily     string        `json:"operation_family"`
	Target              string        `json:"target"`
	Result              string        `json:"result"`
	StatusCode          int           `json:"status_code"`
	ObservedAt          string        `json:"observed_at,omitempty"`
	TTFBMillis          *int          `json:"ttfb_millis,omitempty"`
	DurMillis           *int          `json:"dur_millis,omitempty"`
	InputTokens         *int          `json:"input_tokens,omitempty"`
	OutputTokens        *int          `json:"output_tokens,omitempty"`
	CacheReadTokens     *int          `json:"cache_read_tokens,omitempty"`
	CacheWriteTokens    *int          `json:"cache_write_tokens,omitempty"`
	Mutations           []Mutation    `json:"wire_patch_mutations,omitempty"`
	ExchangeDiagnostics []string      `json:"exchange_diagnostics,omitempty"`
	StageReports        []StageReport `json:"exchange_stage_reports,omitempty"`
}

type Mutation struct {
	Stage         string   `json:"leg"`
	PatchID       string   `json:"patch_id"`
	Changed       bool     `json:"changed"`
	ChangedFields []string `json:"changed_fields,omitempty"`
}

func (m Mutation) HasChanges() bool {
	return m.Changed
}

type StageReport struct {
	Stage   string   `json:"stage"`
	Carrier string   `json:"carrier"`
	Applied []string `json:"applied,omitempty"`
	Mutated bool     `json:"mutated"`
}

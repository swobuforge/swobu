package model

type Model struct {
	HeaderStatus     string
	DaemonState      string
	DaemonHint       string
	ControlPlane     *ControlPlaneMismatch
	InteractionMode  string
	HelpTabOpen      bool
	FooterVerb       string
	FooterAllowSpace bool
	FooterShowTabs   bool

	Endpoints         []string
	EndpointSnapshots []EndpointSnapshot
	CurrentEndpoint   string

	Catalog                   []CatalogEntry
	CatalogError              string
	StreamEnabled             bool
	CreateDraftName           string
	CreateDraftProviderConfig ProviderConfigSnapshot
	CreateDraftModelIDs       []string
	CreateDraftModelError     string
	WorkspaceSaveError        string
	WorkspaceCopyNote         string
	ClientCopyNote            string
	ClientLaunchNote          string
	ClientAccessStatus        string
	ClientAccessNote          string
	RoutingSaveError          string
	LastStoredKeyProviderSpec string
	LastStoredKeySlotName     string
	TrafficRows               []TrafficRow
	TrafficError              string
	HelpNote                  string
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
	Ref           string `json:"ref"`
	ProviderSpec  string `json:"provider_spec"`
	BaseURL       string `json:"base_url,omitempty"`
	CredentialRef string `json:"credential_ref,omitempty"`
	ModelID       string `json:"model_id,omitempty"`
	TargetAlias   string `json:"target_alias,omitempty"`
	ProtocolKind  string `json:"protocol_kind"`
}

type CatalogEntry struct {
	EndpointName      string   `json:"endpoint_name"`
	ProviderConfigRef string   `json:"provider_config_ref"`
	ProviderSpec      string   `json:"provider_spec"`
	ProtocolKind      string   `json:"protocol_kind"`
	ModelIDs          []string `json:"model_ids,omitempty"`
	Error             string   `json:"error,omitempty"`
}

type StatusProjectionSnapshot struct {
	RecentTraffic []TrafficRow `json:"recent_traffic"`
}

type TrafficRow struct {
	RequestID        string `json:"request_id"`
	OperationFamily  string `json:"operation_family"`
	Target           string `json:"target"`
	Result           string `json:"result"`
	StatusCode       int    `json:"status_code"`
	ObservedAt       string `json:"observed_at,omitempty"`
	TTFBMillis       *int   `json:"ttfb_millis,omitempty"`
	DurMillis        *int   `json:"dur_millis,omitempty"`
	InputTokens      *int   `json:"input_tokens,omitempty"`
	OutputTokens     *int   `json:"output_tokens,omitempty"`
	CacheReadTokens  *int   `json:"cache_read_tokens,omitempty"`
	CacheWriteTokens *int   `json:"cache_write_tokens,omitempty"`
}

package protocol

type UpdateCheck struct {
	Type      string `json:"type"`
	RequestID string `json:"request_id"`
}

type UpdateComponentStatus struct {
	Component    string `json:"component"`
	CurrentRef   string `json:"current_ref,omitempty"`
	LatestRef    string `json:"latest_ref,omitempty"`
	Available    bool   `json:"available"`
	CanApply     bool   `json:"can_apply"`
	SourceDir    string `json:"source_dir,omitempty"`
	CurrentBuild string `json:"current_build,omitempty"`
	Error        string `json:"error,omitempty"`
}

type UpdateCheckResult struct {
	Type      string                `json:"type"`
	RequestID string                `json:"request_id"`
	OK        bool                  `json:"ok"`
	Status    UpdateComponentStatus `json:"status"`
	Error     string                `json:"error,omitempty"`
}

type UpdateApply struct {
	Type      string `json:"type"`
	RequestID string `json:"request_id"`
	Force     bool   `json:"force,omitempty"`
}

type UpdateApplyResult struct {
	Type      string                `json:"type"`
	RequestID string                `json:"request_id"`
	OK        bool                  `json:"ok"`
	Status    UpdateComponentStatus `json:"status"`
	Error     string                `json:"error,omitempty"`
	LogLines  []string              `json:"log_lines,omitempty"`
}

package permissions

import "strings"

const (
	DevicesRead     = "devices.read"
	DevicesWrite    = "devices.write"
	AccountAdmin    = "account.admin"
	ShellExecute    = "shell.execute" // reserved; default DENY
	DeployRead      = "deploy.read"
	DeployWrite     = "deploy.write"
	SourceRead      = "source.read"
	SourceWrite     = "source.write"
	BuildRead       = "build.read"
	BuildWrite      = "build.write"
	ReleaseRead     = "release.read"
	ReleaseWrite    = "release.write"
	ReleaseActivate = "release.activate"
	TrafficRead     = "traffic.read"
	TrafficWrite    = "traffic.write"
	LogsRead        = "logs.read"
	LogsWrite       = "logs.write"
	SecretsRead     = "secrets.read"
	SecretsWrite    = "secrets.write"
	ComputeRead     = "compute.read"
	ComputeWrite    = "compute.write"
	CredentialsRW   = "credentials.write"
	ActivityRead    = "activity.read"
	AuditRead       = "audit.read"
	NetworkTransfer = "network.transfer"
	StorageRead     = "storage.read"
	StorageWrite    = "storage.write"
	ServicesRead    = "services.read"
	ServicesWrite   = "services.write"
)

// AllKnown lists scopes that can be granted via API credentials.
var AllKnown = []string{
	DevicesRead,
	DevicesWrite,
	AccountAdmin,
	CredentialsRW,
	ActivityRead,
	AuditRead,
	NetworkTransfer,
	StorageRead,
	StorageWrite,
	ServicesRead,
	ServicesWrite,
	DeployRead,
	DeployWrite,
	SourceRead,
	SourceWrite,
	BuildRead,
	BuildWrite,
	ReleaseRead,
	ReleaseWrite,
	ReleaseActivate,
	TrafficRead,
	TrafficWrite,
	LogsRead,
	LogsWrite,
	SecretsRead,
	SecretsWrite,
	ComputeRead,
	ComputeWrite,
}

// Check returns true if granted contains required (or AccountAdmin for most ops).
func Check(granted []string, required string) bool {
	if required == ShellExecute || required == DeployRead || required == DeployWrite || required == ComputeWrite || required == SecretsRead || required == SecretsWrite || required == SourceRead || required == SourceWrite || required == BuildRead || required == BuildWrite || required == ReleaseRead || required == ReleaseWrite || required == ReleaseActivate || required == TrafficRead || required == TrafficWrite || required == LogsRead || required == LogsWrite || required == AuditRead {
		// Never implied by admin; must be explicit (like shell.execute).
		return has(granted, required)
	}
	if has(granted, AccountAdmin) {
		return true
	}
	return has(granted, required)
}

func has(scopes []string, want string) bool {
	for _, s := range scopes {
		if s == want {
			return true
		}
	}
	return false
}

// ParseCSV parses comma-separated scopes.
func ParseCSV(s string) []string {
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func CanGrant(granted []string, required string) bool {
	if !IsKnown(required) {
		return false
	}
	if !AllowedForAI(required) {
		return false
	}
	return Check(granted, required)
}

func IsKnown(scope string) bool {
	for _, s := range AllKnown {
		if s == scope {
			return true
		}
	}
	return false
}

func AllowedForAI(scope string) bool {
	switch scope {
	case AccountAdmin, ShellExecute, CredentialsRW:
		return false
	default:
		return IsKnown(scope)
	}
}

func FilterGrantable(granted, requested []string) (ok []string, err string) {
	if len(requested) == 0 {
		return nil, "scopes required"
	}
	seen := map[string]bool{}
	for _, sc := range requested {
		sc = strings.TrimSpace(sc)
		if sc == "" || seen[sc] {
			continue
		}
		seen[sc] = true
		if !IsKnown(sc) {
			return nil, "unknown scope: " + sc
		}
		if !AllowedForAI(sc) {
			return nil, "scope not allowed for AI session: " + sc
		}
		if !Check(granted, sc) {
			return nil, "cannot expand rights: " + sc
		}
		ok = append(ok, sc)
	}
	if len(ok) == 0 {
		return nil, "scopes required"
	}
	return ok, ""
}

// JoinCSV joins scopes for storage.
func JoinCSV(scopes []string) string {
	return strings.Join(scopes, ",")
}

// SessionScopes is the full set for logged-in human users.
func SessionScopes() []string {
	return []string{
		DevicesRead,
		DevicesWrite,
		AccountAdmin,
		CredentialsRW,
		ActivityRead,
		AuditRead,
		NetworkTransfer,
		StorageRead,
		StorageWrite,
		ServicesRead,
		ServicesWrite,
		DeployRead,
		DeployWrite,
		SourceRead,
		SourceWrite,
		BuildRead,
		BuildWrite,
		ReleaseRead,
		ReleaseWrite,
		ReleaseActivate,
		TrafficRead,
		TrafficWrite,
		LogsRead,
		LogsWrite,
		SecretsRead,
		SecretsWrite,
		ComputeRead,
		ComputeWrite,
	}
}

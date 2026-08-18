package protocol

import (
	"strings"
	"time"
)

const Version = 1
const AgentVersion = "0.8.2"

const (
	TypeHeartbeat  = "heartbeat"
	TypeHello      = "hello"
	TypeChallenge  = "challenge"
	TypeChallengeR = "challenge_response"
	TypeSession    = "session"
	TypeWelcome    = "welcome"
	TypeError      = "error"

	TypeTransferOffer    = "transfer_offer"
	TypeTransferAccept   = "transfer_accept"
	TypeTransferReject   = "transfer_reject"
	TypeTransferStart    = "transfer_start"
	TypeTransferChunk    = "transfer_chunk"
	TypeTransferAck      = "transfer_ack"
	TypeTransferComplete = "transfer_complete"
	TypeTransferAbort    = "transfer_abort"

	TypePathNegotiate = "path_negotiate"
	TypePathCandidate = "path_candidate"
	TypePathSelected  = "path_selected"

	TypeStorageOp       = "storage_op"
	TypeStorageOpResult = "storage_op_result"

	TypeOfflinePending     = "offline_pending"
	TypeOfflineFlushResult = "offline_flush_result"

	TypeEdgeHTTP         = "edge_http"        // deprecated single-frame (7.2)
	TypeEdgeHTTPResult   = "edge_http_result" // deprecated single-frame (7.2)
	TypeEdgeHTTPBegin    = "edge_http_begin"
	TypeEdgeHTTPBody     = "edge_http_body"
	TypeEdgeHTTPRespHead = "edge_http_resp_head"
	TypeEdgeHTTPRespBody = "edge_http_resp_body"
	TypeEdgeHTTPAck      = "edge_http_ack"
	TypeEdgeHTTPFail     = "edge_http_fail"
	TypeEdgeProbe        = "edge_probe"
	TypeEdgeProbeResult  = "edge_probe_result"

	TypeDeployApply       = "deploy_apply"
	TypeDeployApplyResult = "deploy_apply_result"
	TypeDeployLogLine     = "deploy_log_line"

	TypeJobRun     = "job_run"
	TypeJobCancel  = "job_cancel"
	TypeJobResult  = "job_result"
	TypeJobLogLine = "job_log_line"

	TypeBuildRun      = "build_run"
	TypeBuildCancel   = "build_cancel"
	TypeBuildResult   = "build_result"
	TypeBuildLogLine  = "build_log_line"
	TypeBuildProgress = "build_progress"

	TypeUpdateCheck       = "update_check"
	TypeUpdateCheckResult = "update_check_result"
	TypeUpdateApply       = "update_apply"
	TypeUpdateApplyResult = "update_apply_result"

	// Stage 7.5 — raw byte stream for TLS passthrough (separate from HTTP framing).
	TypeEdgeStreamOpen  = "edge_stream_open"
	TypeEdgeStreamData  = "edge_stream_data"
	TypeEdgeStreamAck   = "edge_stream_ack"
	TypeEdgeStreamClose = "edge_stream_close"
	TypeEdgeStreamFail  = "edge_stream_fail"
	TypeEdgeStreamReady = "edge_stream_ready"
)

// MaxTransferBytes is the Stage 2 network.transfer size limit (16 MiB).
const MaxTransferBytes = 16 << 20

// MaxStorageTransferBytes is the Stage 4.0 Storage engine size limit (256 MiB).
const MaxStorageTransferBytes = 256 << 20

// MaxEdgeRequestBytes is the cumulative request body limit over all chunks (Stage 7.3).
const MaxEdgeRequestBytes = 256 << 20

// MaxEdgeResponseBytes was the 7.2 single-frame cap; streaming has no total response cap.
// MaxEdgeBufferPerStream bounds in-flight bytes buffered on Control Plane per stream.
const MaxEdgeBufferPerStream = 256 << 10

// EdgeChunkBytes is the preferred chunk size for tunneled HTTP bodies.
const EdgeChunkBytes = DefaultChunkBytes

// MaxEdgeInflightChunks is unacked chunks allowed per direction (backpressure).
const MaxEdgeInflightChunks = 2

// MaxEdgeConcurrentPerDevice / MaxEdgeConcurrentPerService cap parallel Edge HTTP streams.
const MaxEdgeConcurrentPerDevice = 32
const MaxEdgeConcurrentPerService = 16

const DeployApplyTimeout = 3 * time.Minute

const (
	DefaultDeployCPUs     = 1.0
	DefaultDeployMemoryMB = int64(512)
	DefaultDeployPids     = int64(256)

	DefaultJobCPUs        = 1.0
	DefaultJobMemoryMB    = int64(512)
	DefaultJobPids        = int64(256)
	DefaultJobDiskMB      = int64(1024)
	DefaultJobTimeout     = 300
	DefaultJobConcurrent  = 4
	MaxJobTimeout         = 3600
	MaxJobArgs            = 32
	MaxJobPids            = int64(4096)
	MinJobPids            = int64(16)
	MaxJobDiskMB          = int64(64 * 1024)
	MinJobDiskMB          = int64(1)
	DefaultJobPolicyMemMB = int64(8192)

	DefaultMaxArtifactBytes = int64(256 << 20)
	DefaultMaxArtifactFiles = 256
	DefaultMaxArtifactDirs  = 64
	DefaultMaxArtifactDepth = 8
	DefaultJobRetries       = 1
)

const (
	JobStatusQueued             = "queued"
	JobStatusWaitingForResource = "waiting_for_resource"
	JobStatusAssigned           = "assigned"
	JobStatusRunning            = "running"
	JobStatusSucceeded          = "succeeded"
	JobStatusArtifactsCommitted = "artifacts_committed"
	JobStatusFailed             = "failed"
	JobStatusTimeout            = "timeout"
	JobStatusCanceled           = "canceled"
	JobStatusRejected           = "rejected"
)

const (
	JobReasonResourceExceeded   = "resource_exceeded"
	JobReasonGPUUnavailable     = "gpu_unavailable"
	JobReasonPolicyExceeded     = "policy_exceeded"
	JobReasonSlotUnavailable    = "slot_unavailable"
	JobReasonArtifactLimit      = "artifact_limit"
	JobReasonUnsatisfiable      = "unsatisfiable"
	JobReasonWaitingForResource = "waiting_for_resource"
)

const (
	JobPlacementPinned    = "pinned"
	JobPlacementScheduled = "scheduled"
)

// TLS route modes (Stage 7.5).
const (
	TLSModeEdgeTerminate = "edge_terminate"
	TLSModeOriginTLS     = "origin_tls"
)

// Edge stream limits reuse HTTP chunk/backpressure defaults (Stage 7.5).
const (
	MaxEdgeStreamBytes      = 256 << 20 // per-direction cumulative cap
	EdgeStreamChunkBytes    = EdgeChunkBytes
	MaxEdgeStreamInflight   = MaxEdgeInflightChunks
	MaxEdgeStreamConcurrent = MaxEdgeConcurrentPerDevice
)

// Deprecated: 7.2 single-frame response cap (tests may reference).
const MaxEdgeResponseBytes = 2 << 20

// DefaultChunkBytes is the preferred chunk size for relay frames.
const DefaultChunkBytes = 64 << 10

// PartSuffixPrefix builds per-upload part files: path + ".knot.part." + fileID
const PartSuffixPrefix = ".knot.part."

func PartPath(finalPath, fileID string) string {
	if fileID == "" {
		return finalPath + ".knot.part"
	}
	return finalPath + PartSuffixPrefix + fileID
}

type Heartbeat struct {
	Type      string    `json:"type"`
	Telemetry Telemetry `json:"telemetry"`
}

type Telemetry struct {
	Hostname  string            `json:"hostname"`
	OS        string            `json:"os"`
	Arch      string            `json:"arch"`
	CPUs      int               `json:"cpus"`
	RAMMB     uint64            `json:"ram_mb"`
	UptimeSec int64             `json:"uptime_sec,omitempty"`
	Version   string            `json:"version,omitempty"`
	Compute   *ComputeInventory `json:"compute,omitempty"`
}

// ComputeInventory is a point-in-time hardware snapshot (Stage 8.1).
// GPU is a pointer so JSON null means “could not detect”, not “zero GPUs”.
type ComputeInventory struct {
	CPU    ComputeCPU    `json:"cpu"`
	Memory ComputeMemory `json:"memory"`
	GPU    *[]ComputeGPU `json:"gpu"`
	Disks  []ComputeDisk `json:"disks"`
}

type ComputeCPU struct {
	Cores        int      `json:"cores"`
	Architecture string   `json:"architecture"`
	UsagePercent *float64 `json:"usage_percent,omitempty"`
}

type ComputeMemory struct {
	TotalBytes     uint64   `json:"total_bytes"`
	AvailableBytes uint64   `json:"available_bytes"`
	UsedBytes      uint64   `json:"used_bytes"`
	UsagePercent   *float64 `json:"usage_percent,omitempty"`
}

type ComputeGPU struct {
	Vendor    string  `json:"vendor"`
	Model     string  `json:"model"`
	VRAMBytes *uint64 `json:"vram_bytes"`
	Available *bool   `json:"available,omitempty"`
}

type ComputeDisk struct {
	Mount      string `json:"mount"`
	Name       string `json:"name,omitempty"`
	FSType     string `json:"fstype,omitempty"`
	TotalBytes uint64 `json:"total_bytes"`
	FreeBytes  uint64 `json:"free_bytes"`
	UsedBytes  uint64 `json:"used_bytes"`
}

const (
	ComputeStatusAvailable = "available"
	ComputeStatusStale     = "stale"
	ComputeStatusOffline   = "offline"
)

type RegisterRequest struct {
	RegistrationToken string `json:"registration_token"`
	PublicKey         string `json:"public_key"`
	Name              string `json:"name,omitempty"`
	Hostname          string `json:"hostname"`
	OS                string `json:"os"`
	Arch              string `json:"arch"`
}

type RegisterResponse struct {
	DeviceID    string `json:"device_id"`
	DeviceToken string `json:"device_token"`
	Name        string `json:"name"`
}

type ChallengeMessage struct {
	Type  string `json:"type"`
	Nonce string `json:"nonce"`
}

type ChallengeResponse struct {
	Type      string `json:"type"`
	Nonce     string `json:"nonce"`
	Signature string `json:"signature"`
}

type SessionMessage struct {
	Type          string `json:"type"`
	DeviceSession string `json:"device_session"`
	ExpiresIn     int64  `json:"expires_in"`
}

type ServerMessage struct {
	Type            string `json:"type"`
	Message         string `json:"message,omitempty"`
	Code            string `json:"code,omitempty"`
	AgentVersion    string `json:"agent_version,omitempty"`
	MinAgentVersion string `json:"min_agent_version,omitempty"`
}

// VersionOlder reports whether have is a lower dotted numeric version than min.
func VersionOlder(have, min string) bool {
	if min == "" {
		return false
	}
	if have == "" {
		return true
	}
	a := splitVer(have)
	b := splitVer(min)
	n := len(a)
	if len(b) > n {
		n = len(b)
	}
	for i := 0; i < n; i++ {
		var x, y int
		if i < len(a) {
			x = a[i]
		}
		if i < len(b) {
			y = b[i]
		}
		if x < y {
			return true
		}
		if x > y {
			return false
		}
	}
	return false
}

func splitVer(s string) []int {
	parts := strings.Split(s, ".")
	out := make([]int, 0, len(parts))
	for _, p := range parts {
		n := 0
		for _, c := range p {
			if c < '0' || c > '9' {
				break
			}
			n = n*10 + int(c-'0')
		}
		out = append(out, n)
	}
	return out
}

// TransferOffer is sent by Control Plane to source and destination agents.
type TransferOffer struct {
	Type         string `json:"type"`
	TransferID   string `json:"transfer_id"`
	Role         string `json:"role"` // "source" | "dest"
	FromDeviceID string `json:"from_device_id"`
	ToDeviceID   string `json:"to_device_id"`
	Filename     string `json:"filename"`
	SourcePath   string `json:"source_path,omitempty"` // relative path on source outbox or storage
	Size         int64  `json:"size"`
	SHA256       string `json:"sha256"`
	ChunkBytes   int    `json:"chunk_bytes"`
	// SourceFromStorage: source reads SourcePath under knot-storage (not outbox).
	SourceFromStorage bool `json:"source_from_storage,omitempty"`
	// DestStoragePath: dest writes under knot-storage at this relative path (not inbox).
	DestStoragePath string `json:"dest_storage_path,omitempty"`
	// FileID links the transfer to a storage_files row (Stage 4.0).
	FileID string `json:"file_id,omitempty"`
	// ResumeOffset: source skips this many bytes; dest appends to existing .part.
	ResumeOffset int64 `json:"resume_offset,omitempty"`
}

type TransferAccept struct {
	Type       string `json:"type"`
	TransferID string `json:"transfer_id"`
	DeviceID   string `json:"device_id"`
}

type TransferStart struct {
	Type       string `json:"type"`
	TransferID string `json:"transfer_id"`
	Path       string `json:"path,omitempty"` // direct | relay
}

type PathNegotiate struct {
	Type          string   `json:"type"`
	TransferID    string   `json:"transfer_id"`
	Role          string   `json:"role"`
	PeerDeviceID  string   `json:"peer_device_id"`
	PeerPublicKey string   `json:"peer_public_key"` // base64url raw
	ForceRelay    bool     `json:"force_relay"`
	STUNURLs      []string `json:"stun_urls,omitempty"`
}

type PathCandidateMsg struct {
	Type       string `json:"type"`
	TransferID string `json:"transfer_id"`
	DeviceID   string `json:"device_id"`
	Addr       string `json:"addr"`
	Kind       string `json:"kind"`
}

type PathSelected struct {
	Type       string `json:"type"`
	TransferID string `json:"transfer_id"`
	DeviceID   string `json:"device_id"`
	Path       string `json:"path"` // direct | relay
}

type TransferReject struct {
	Type       string `json:"type"`
	TransferID string `json:"transfer_id"`
	DeviceID   string `json:"device_id"`
	Reason     string `json:"reason,omitempty"`
}

type TransferChunk struct {
	Type       string `json:"type"`
	TransferID string `json:"transfer_id"`
	Index      int    `json:"index"`
	DataB64    string `json:"data_b64"`
	Last       bool   `json:"last"`
}

type TransferAck struct {
	Type          string `json:"type"`
	TransferID    string `json:"transfer_id"`
	Index         int    `json:"index"`
	BytesReceived int64  `json:"bytes_received,omitempty"` // dest total so far (for client progress)
}

type TransferComplete struct {
	Type       string `json:"type"`
	TransferID string `json:"transfer_id"`
	DeviceID   string `json:"device_id"`
	SHA256     string `json:"sha256"`
}

type TransferAbort struct {
	Type       string `json:"type"`
	TransferID string `json:"transfer_id"`
	DeviceID   string `json:"device_id,omitempty"`
	Reason     string `json:"reason,omitempty"`
}

// Storage FS control-plane ops (metadata only; bytes use Transfer).
const (
	StorageOpList        = "list"
	StorageOpStat        = "stat"
	StorageOpMkdir       = "mkdir"
	StorageOpDelete      = "delete"
	StorageOpEnsure      = "ensure"
	StorageOpPartial     = "partial" // bytes in .knot.part for path (+ optional file_id)
	StorageOpMove        = "move"
	StorageOpCopy        = "copy"
	StorageOpWriteStart  = "write_start" // Stage 5.2 browser/HTTP put
	StorageOpWriteChunk  = "write_chunk"
	StorageOpWriteCommit = "write_commit"
	StorageOpWriteAbort  = "write_abort"
	StorageOpRead        = "read" // small content for preview/download (base64)
	StorageOpPreview     = "preview"
)

type StorageOp struct {
	Type      string `json:"type"`
	RequestID string `json:"request_id"`
	Op        string `json:"op"`
	Path      string `json:"path,omitempty"`
	FromPath  string `json:"from_path,omitempty"`
	ToPath    string `json:"to_path,omitempty"`
	FileID    string `json:"file_id,omitempty"` // for partial / resume identity
	Size      int64  `json:"size,omitempty"`
	SHA256    string `json:"sha256,omitempty"`
	Offset    int64  `json:"offset,omitempty"`
	DataB64   string `json:"data_b64,omitempty"`
	MaxBytes  int64  `json:"max_bytes,omitempty"` // read limit
	Preview   string `json:"preview,omitempty"`   // thumb | preview
	MaxPixels int    `json:"max_pixels,omitempty"`
}

type StorageEntry struct {
	Name     string `json:"name"`
	Path     string `json:"path"`
	IsDir    bool   `json:"is_directory"`
	Size     int64  `json:"size,omitempty"`
	Mtime    string `json:"mtime,omitempty"`
	SHA256   string `json:"sha256,omitempty"`
	MimeType string `json:"mime_type,omitempty"`
	FileID   string `json:"file_id,omitempty"`
}

type StorageStat struct {
	FileID   string `json:"file_id,omitempty"`
	Name     string `json:"name,omitempty"`
	Path     string `json:"path"`
	IsDir    bool   `json:"is_directory"`
	Size     int64  `json:"size"`
	Mtime    string `json:"mtime"`
	Mode     string `json:"mode,omitempty"`
	SHA256   string `json:"sha256,omitempty"`
	MimeType string `json:"mime_type,omitempty"`
}

type StorageOpResult struct {
	Type         string         `json:"type"`
	RequestID    string         `json:"request_id"`
	OK           bool           `json:"ok"`
	Error        string         `json:"error,omitempty"`
	Entries      []StorageEntry `json:"entries,omitempty"`
	Stat         *StorageStat   `json:"stat,omitempty"`
	PartialBytes int64          `json:"partial_bytes,omitempty"`
	DataB64      string         `json:"data_b64,omitempty"`
	MimeType     string         `json:"mime_type,omitempty"`
	Size         int64          `json:"size,omitempty"`
	CacheKey     string         `json:"cache_key,omitempty"`
	PreviewKind  string         `json:"preview_kind,omitempty"`
}

// OfflinePending is sent by the agent after reconnect when the local queue has work.
type OfflinePending struct {
	Type    string   `json:"type"`
	Pending int      `json:"pending"`
	Paths   []string `json:"paths,omitempty"`
}

// OfflineFlushResult is sent by the Control Plane after draining sync jobs for a device.
type OfflineFlushResult struct {
	Type          string   `json:"type"`
	OK            bool     `json:"ok"`
	JobIDs        []string `json:"job_ids,omitempty"`
	ConflictPaths []string `json:"conflict_paths,omitempty"`
	Error         string   `json:"error,omitempty"`
}

// EdgeHTTPBegin starts a streamed HTTP request over the agent tunnel (Stage 7.3).
type EdgeHTTPBegin struct {
	Type      string     `json:"type"`
	RequestID string     `json:"request_id"`
	Method    string     `json:"method"`
	Path      string     `json:"path"`
	Port      int        `json:"port"`
	Headers   []HeaderKV `json:"headers,omitempty"`
}

// EdgeHTTPBody is one request-body chunk CP → Agent.
type EdgeHTTPBody struct {
	Type      string `json:"type"`
	RequestID string `json:"request_id"`
	Seq       int    `json:"seq"`
	DataB64   string `json:"data_b64,omitempty"`
	Last      bool   `json:"last"`
}

// EdgeHTTPRespHead carries origin status/headers Agent → CP.
type EdgeHTTPRespHead struct {
	Type      string     `json:"type"`
	RequestID string     `json:"request_id"`
	OK        bool       `json:"ok"`
	Status    int        `json:"status,omitempty"`
	Headers   []HeaderKV `json:"headers,omitempty"`
	Error     string     `json:"error,omitempty"`
}

// EdgeHTTPRespBody is one response-body chunk Agent → CP.
type EdgeHTTPRespBody struct {
	Type      string `json:"type"`
	RequestID string `json:"request_id"`
	Seq       int    `json:"seq"`
	DataB64   string `json:"data_b64,omitempty"`
	Last      bool   `json:"last"`
}

// EdgeHTTPAck acknowledges a chunk for flow control (bidirectional).
type EdgeHTTPAck struct {
	Type      string `json:"type"`
	RequestID string `json:"request_id"`
	Direction string `json:"direction"` // req | resp
	Seq       int    `json:"seq"`
}

// EdgeHTTPFail aborts an in-flight streamed request.
type EdgeHTTPFail struct {
	Type      string `json:"type"`
	RequestID string `json:"request_id"`
	Error     string `json:"error,omitempty"`
}

// EdgeHTTP is a legacy single-frame request (Stage 7.2 — do not use for new traffic).
type EdgeHTTP struct {
	Type      string     `json:"type"`
	RequestID string     `json:"request_id"`
	Method    string     `json:"method"`
	Path      string     `json:"path"`
	Port      int        `json:"port"`
	Headers   []HeaderKV `json:"headers,omitempty"`
	BodyB64   string     `json:"body_b64,omitempty"`
}

type HeaderKV struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

type EdgeHTTPResult struct {
	Type      string     `json:"type"`
	RequestID string     `json:"request_id"`
	OK        bool       `json:"ok"`
	Error     string     `json:"error,omitempty"`
	Status    int        `json:"status,omitempty"`
	Headers   []HeaderKV `json:"headers,omitempty"`
	BodyB64   string     `json:"body_b64,omitempty"`
}

type EdgeProbe struct {
	Type      string `json:"type"`
	RequestID string `json:"request_id"`
	Port      int    `json:"port"`
}

type EdgeProbeResult struct {
	Type      string `json:"type"`
	RequestID string `json:"request_id"`
	OK        bool   `json:"ok"`
	Error     string `json:"error,omitempty"`
	LatencyMS int64  `json:"latency_ms,omitempty"`
}

// Deploy actions (Stage 7.4 — structured workload lifecycle; no arbitrary shell).
const (
	DeployActionApply   = "apply"
	DeployActionStop    = "stop"
	DeployActionRestart = "restart"
	DeployActionRemove  = "remove"
	DeployActionLogs    = "logs"
)

// DeploySpec is the sandboxed workload declaration sent to the agent runner.
type DeploySpec struct {
	Name       string            `json:"name"`
	Runtime    string            `json:"runtime"` // docker
	Image      string            `json:"image"`
	Port       int               `json:"port"`
	Bind       string            `json:"bind"`
	HealthPath string            `json:"health_path"`
	Env        map[string]string `json:"env,omitempty"`
	Limits     DeployLimits      `json:"limits,omitempty"`
}

// DeployLimits are Stage 7.6 sandbox caps applied by the agent runner.
type DeployLimits struct {
	CPUs     float64 `json:"cpus,omitempty"`
	MemoryMB int64   `json:"memory_mb,omitempty"`
	Pids     int64   `json:"pids,omitempty"`
	ReadOnly bool    `json:"read_only"`
}

// DeployApply is CP → Agent structured lifecycle RPC.
type DeployApply struct {
	Type              string     `json:"type"`
	RequestID         string     `json:"request_id"`
	Action            string     `json:"action"`
	DeploymentID      string     `json:"deployment_id"`
	RemoveContainerID string     `json:"remove_container_id,omitempty"`
	Spec              DeploySpec `json:"spec"`
}

// DeployApplyResult is Agent → CP reply.
type DeployApplyResult struct {
	Type        string   `json:"type"`
	RequestID   string   `json:"request_id"`
	OK          bool     `json:"ok"`
	Error       string   `json:"error,omitempty"`
	ContainerID string   `json:"container_id,omitempty"`
	Status      string   `json:"status,omitempty"`
	HealthOK    bool     `json:"health_ok"`
	LogLines    []string `json:"log_lines,omitempty"`
}

// DeployLogLine is an optional streaming log frame (sanitized on agent).
type DeployLogLine struct {
	Type         string `json:"type"`
	DeploymentID string `json:"deployment_id"`
	Stream       string `json:"stream"`
	Message      string `json:"message"`
}

// EdgeStreamOpen starts a raw TCP byte tunnel to loopback:port (Stage 7.5 TLS passthrough).
type EdgeStreamOpen struct {
	Type      string `json:"type"`
	RequestID string `json:"request_id"`
	Port      int    `json:"port"`
	Hostname  string `json:"hostname,omitempty"`
}

// EdgeStreamReady is Agent → CP after origin TCP dial succeeds.
type EdgeStreamReady struct {
	Type      string `json:"type"`
	RequestID string `json:"request_id"`
	OK        bool   `json:"ok"`
	Error     string `json:"error,omitempty"`
}

// EdgeStreamData carries opaque bytes (TLS records). Direction: up = client→origin, down = origin→client.
type EdgeStreamData struct {
	Type      string `json:"type"`
	RequestID string `json:"request_id"`
	Direction string `json:"direction"`
	Seq       int    `json:"seq"`
	DataB64   string `json:"data_b64,omitempty"`
	Last      bool   `json:"last"`
}

// EdgeStreamAck acknowledges a stream chunk for backpressure.
type EdgeStreamAck struct {
	Type      string `json:"type"`
	RequestID string `json:"request_id"`
	Direction string `json:"direction"`
	Seq       int    `json:"seq"`
}

// EdgeStreamClose ends one direction or the whole stream.
type EdgeStreamClose struct {
	Type      string `json:"type"`
	RequestID string `json:"request_id"`
	Direction string `json:"direction,omitempty"`
	Reason    string `json:"reason,omitempty"`
}

// EdgeStreamFail aborts an in-flight byte stream.
type EdgeStreamFail struct {
	Type      string `json:"type"`
	RequestID string `json:"request_id"`
	Error     string `json:"error,omitempty"`
}

// JobResources are sandbox caps for a one-shot compute job (Stage 8.2/8.3).
type JobResources struct {
	CPU      float64 `json:"cpu,omitempty"`
	MemoryMB int64   `json:"memory_mb,omitempty"`
	GPU      int     `json:"gpu,omitempty"`
	Pids     int64   `json:"pids,omitempty"`
	DiskMB   int64   `json:"disk_mb,omitempty"`
}

// JobSpec is the structured job declaration. Never a shell string.
type JobSpec struct {
	JobID          string            `json:"job_id"`
	Image          string            `json:"image"`
	Command        []string          `json:"command,omitempty"`
	Env            map[string]string `json:"env,omitempty"`
	Resources      JobResources      `json:"resources"`
	TimeoutSeconds int               `json:"timeout_seconds"`
	InputPath      string            `json:"input_path,omitempty"`
	OutputPath     string            `json:"output_path,omitempty"`
	SourcePath     string            `json:"source_path,omitempty"`
}

// JobRun is CP → Agent: start a one-shot container.
type JobRun struct {
	Type      string  `json:"type"`
	RequestID string  `json:"request_id"`
	Action    string  `json:"action"`
	JobID     string  `json:"job_id"`
	Spec      JobSpec `json:"spec"`
}

// JobCancel is CP → Agent: kill an in-flight job.
type JobCancel struct {
	Type      string `json:"type"`
	RequestID string `json:"request_id"`
	JobID     string `json:"job_id"`
}

// JobArtifact is one committed output file (ordinary Storage object + job link).
type JobArtifact struct {
	Name     string `json:"name"`
	Path     string `json:"path"`
	Size     int64  `json:"size"`
	SHA256   string `json:"sha256"`
	MimeType string `json:"mime_type,omitempty"`
}

// JobResult is Agent → CP after the container exits (or is killed).
type JobResult struct {
	Type        string        `json:"type"`
	RequestID   string        `json:"request_id"`
	JobID       string        `json:"job_id"`
	OK          bool          `json:"ok"`
	Status      string        `json:"status"`
	Reason      string        `json:"reason,omitempty"`
	ExitCode    *int          `json:"exit_code,omitempty"`
	Error       string        `json:"error,omitempty"`
	LogLines    []string      `json:"log_lines,omitempty"`
	OutputPath  string        `json:"output_path,omitempty"`
	OutputFiles []string      `json:"output_files,omitempty"`
	Artifacts   []JobArtifact `json:"artifacts,omitempty"`
	ContainerID string        `json:"container_id,omitempty"`
}

// JobSucceeded reports whether the job finished and committed artifacts.
func JobSucceeded(status string) bool {
	return status == JobStatusArtifactsCommitted || status == JobStatusSucceeded
}

// JobTerminal reports whether the job will not change further.
func JobTerminal(status string) bool {
	switch status {
	case JobStatusSucceeded, JobStatusArtifactsCommitted, JobStatusFailed, JobStatusTimeout, JobStatusCanceled, JobStatusRejected:
		return true
	}
	return false
}

// JobLogLine is a sanitized stdout/stderr line while the job runs.
type JobLogLine struct {
	Type    string `json:"type"`
	JobID   string `json:"job_id"`
	Stream  string `json:"stream"`
	Message string `json:"message"`
}

// Stage 9.2 — Git source → Dockerfile build on a pinned node → image tag/push.
const (
	BuildStatusQueued      = "queued"
	BuildStatusCloning     = "cloning"
	BuildStatusBuilding    = "building"
	BuildStatusPushing     = "pushing"
	BuildStatusCompleted   = "completed"
	BuildStatusFailedClone = "failed_clone"
	BuildStatusFailedBuild = "failed_build"
	BuildStatusFailedPush  = "failed_push"
	BuildStatusFailed      = "failed"
	BuildStatusCanceled    = "canceled"

	DefaultBuildTimeout = 600
	MaxBuildTimeout     = 3600
)

// BuildSpec is the sandboxed Dockerfile build sent to the agent (no shell).
type BuildSpec struct {
	BuildID        string `json:"build_id"`
	URL            string `json:"url"`
	Branch         string `json:"branch,omitempty"`
	GitTag         string `json:"git_tag,omitempty"`
	Revision       string `json:"revision,omitempty"`
	Dockerfile     string `json:"dockerfile"`
	Context        string `json:"context"`
	Tag            string `json:"tag"`
	TimeoutSeconds int    `json:"timeout_seconds"`
	GitToken       string `json:"git_token,omitempty"`
	RegistryUser   string `json:"registry_user,omitempty"`
	RegistryToken  string `json:"registry_token,omitempty"`
}

// BuildRun is CP → Agent: clone + docker build + push.
type BuildRun struct {
	Type      string    `json:"type"`
	RequestID string    `json:"request_id"`
	BuildID   string    `json:"build_id"`
	Spec      BuildSpec `json:"spec"`
}

// BuildCancel is CP → Agent: abort an in-flight build.
type BuildCancel struct {
	Type      string `json:"type"`
	RequestID string `json:"request_id"`
	BuildID   string `json:"build_id"`
}

// BuildProgress is Agent → CP while the build moves between lifecycle stages.
type BuildProgress struct {
	Type    string `json:"type"`
	BuildID string `json:"build_id"`
	Status  string `json:"status"`
}

// BuildResult is Agent → CP after clone/build/push finishes or fails.
type BuildResult struct {
	Type      string   `json:"type"`
	RequestID string   `json:"request_id"`
	BuildID   string   `json:"build_id"`
	OK        bool     `json:"ok"`
	Status    string   `json:"status"`
	Error     string   `json:"error,omitempty"`
	Image     string   `json:"image,omitempty"`
	Revision  string   `json:"revision,omitempty"`
	LogLines  []string `json:"log_lines,omitempty"`
}

// BuildLogLine is a sanitized stdout/stderr line while the build runs.
type BuildLogLine struct {
	Type    string `json:"type"`
	BuildID string `json:"build_id"`
	Stream  string `json:"stream"`
	Message string `json:"message"`
}

// BuildTerminal reports whether the build will not change further.
func BuildTerminal(status string) bool {
	switch status {
	case BuildStatusCompleted, BuildStatusFailedClone, BuildStatusFailedBuild, BuildStatusFailedPush, BuildStatusFailed, BuildStatusCanceled:
		return true
	}
	return false
}

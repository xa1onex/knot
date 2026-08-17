/** Stage 5.0 Node Client SDK — shared types for all app shells. */

export type Identity = {
  kind: string
  user_id: string
  email: string
  actor: string
  scopes: string[]
}

export type User = { id: string; email: string }

export type LoginResult = {
  access_token: string
  refresh_token: string
  expires_in: number
  token?: string
  user: User
}

export type Device = {
  id: string
  name: string
  hostname: string
  os: string
  arch: string
  cpus: number
  ram_mb: number
  agent_version?: string
  online: boolean
  revoked_at?: string | null
  last_seen_at?: string | null
  created_at: string
}

export type Overview = {
  devices_total: number
  devices_online: number
  devices_offline: number
}

export type Credential = {
  id: string
  name: string
  token_prefix: string
  scopes: string[]
  expires_at?: string | null
  revoked_at?: string | null
  created_at: string
  token?: string
}

export type AISession = {
  id: string
  credential_id: string
  name: string
  created_by: string
  parent: string
  actor?: string
  scopes: string[]
  status: string
  expires_at: string
  created_at: string
  revoked_at?: string | null
  token?: string
}

export type CreateAISessionRequest = {
  name?: string
  scopes: string[]
  ttl_minutes?: number
  expires_in?: string
}

export type AuditEvent = {
  id?: string
  action: string
  actor_type: string
  actor: string
  actor_id?: string
  parent?: string
  parent_actor?: string
  time?: string
  created_at?: string
  target?: string
  resource?: string
  result?: string
  trace_id?: string
  ai_session_id?: string
  mcp_client?: string
  workflow_id?: string
  detail?: string
  route?: string
  release?: string
}

export type AuditQuery = {
  action?: string
  actor_type?: string
  actor_id?: string
  ai_session_id?: string
  workflow_id?: string
  trace_id?: string
  mcp_client?: string
  q?: string
  limit?: number
}

export type AIActivityStep = {
  name: string
  ok: boolean
  status: string
}

export type AIActivity = {
  time: string
  actor_type: string
  actor: string
  parent: string
  ai_session_id: string
  mcp_client?: string
  workflow_id?: string
  trace_id?: string
  ran?: string
  service?: string
  target?: string
  steps?: AIActivityStep[]
  result: string
  action?: string
}

export type TransferStatus =
  | 'pending'
  | 'offered'
  | 'negotiating'
  | 'transferring'
  | 'completed'
  | 'failed'
  | 'aborted'

export type Transfer = {
  id: string
  from_device_id: string
  to_device_id: string
  filename: string
  source_path: string
  size: number
  sha256: string
  status: TransferStatus | string
  error: string
  path: string
  file_id: string
  resume_offset: number
  bytes_received: number
  file_status?: string
  created_at: string
  updated_at: string
  completed_at?: string | null
}

export type Progress = {
  transfer_id: string
  status: string
  bytes_received: number
  size: number
  percent: number
  path?: string
  file_id?: string
  error?: string
}

export type StorageEntry = {
  name: string
  path: string
  is_directory: boolean
  size?: number
  mtime?: string
  sha256?: string
  mime_type?: string
  file_id?: string
}

export type StorageStat = {
  file_id?: string
  name?: string
  path: string
  is_directory: boolean
  size: number
  mtime: string
  mode?: string
  sha256?: string
  mime_type?: string
}

export type StoragePreview = {
  data: Blob
  contentType: string
  cacheKey?: string
  sourceMime?: string
}

export type StorageFile = {
  id: string
  device_id: string
  path: string
  size: number
  sha256: string
  status: string
  transfer_id: string
  bytes_received: number
  created_at: string
  updated_at: string
}

export type StorageUploadRequest = {
  device_id: string
  path: string
  from_device_id: string
  source_path: string
  size: number
  sha256: string
  resume?: boolean
}

/** Stage 6.1 one-way sync job. */
export type SyncJob = {
  id: string
  name: string
  mode: string
  source_device_id: string
  source_path: string
  dest_device_id: string
  dest_path: string
  status: string
  files_total: number
  files_done: number
  bytes_total: number
  bytes_done: number
  current_path: string
  current_transfer_id?: string
  last_error: string
  conflicts_open?: number
  last_run_at?: string
  created_at: string
  updated_at: string
}

export type CreateSyncJobRequest = {
  name?: string
  mode?: 'one_way' | 'two_way'
  source_device_id: string
  source_path: string
  dest_device_id: string
  dest_path: string
}

export type SyncConflict = {
  id: string
  job_id: string
  rel_path: string
  status: string
  a_exists: boolean
  a_deleted: boolean
  a_size: number
  a_mtime: string
  a_sha256: string
  b_exists: boolean
  b_deleted: boolean
  b_size: number
  b_mtime: string
  b_sha256: string
  base_sha256: string
  base_size: number
  resolution: string
  created_at: string
  resolved_at?: string
  a_device_id?: string
  b_device_id?: string
  a_device_name?: string
  b_device_name?: string
  a_root?: string
  b_root?: string
  keep_both_suggested_name?: string
}

export type SyncFlushResult = {
  device_id: string
  job_ids: string[]
  conflict_paths: string[]
  errors?: string[]
}

/** Stage 6.5 metadata search hit. Bytes stay on the node. */
export type FileHit = {
  id: string
  file_id: string
  device_id: string
  device_name: string
  path: string
  name: string
  size: number
  mtime: string
  sha256: string
  mime_type: string
  is_directory: boolean
  indexed_at: string
}

export type FileSearchQuery = {
  q?: string
  device_id?: string
  folder?: string
  type?: string
  min_size?: number
  max_size?: number
  modified_after?: string
  modified_before?: string
  is_directory?: boolean
  limit?: number
}

export type FilesReindexResult = {
  device_ids: string[]
  entries: number
  skipped?: string[]
  errors?: string[]
}

/** Stage 7.1 service registry row. Process still lives on the node. */
export type HostedService = {
  id: string
  device_id: string
  device_name: string
  device_online: boolean
  name: string
  kind: string
  protocol: string
  port: number
  bind: string
  listen: string
  status: string
  registered?: boolean
  agent_online?: boolean
  tunnel_connected?: boolean
  backend_reachable?: boolean
  edge_device_id?: string
  edge_device_name?: string
  edge_online?: boolean
  hostnames?: string[]
  health_error?: string
  created_at: string
  updated_at: string
}

export type ServiceNode = {
  device_id: string
  device_name: string
  online: boolean
  services: HostedService[]
}

export type RegisterServiceRequest = {
  device_id: string
  name: string
  kind?: string
  protocol?: string
  port: number
  bind?: string
}

export type UpdateServiceRequest = {
  name?: string
  kind?: string
  protocol?: string
  port?: number
  bind?: string
}

/** Stage 7.2 — public hostname routed onto a node loopback via the agent tunnel. */
export type EdgeRoute = {
  id: string
  hostname: string
  service_id: string
  service_name: string
  device_id: string
  device_name: string
  edge_device_id?: string
  edge_device_name?: string
  tls_mode?: string
  listen: string
  active_release_id?: string
  prev_release_id?: string
  created_at: string
}

export type RouteTrafficTarget = {
  release_id: string
  number: number
  image: string
  status: string
  weight: number
  current: boolean
  port?: number
  device_id?: string
}

export type RouteTrafficEvent = {
  id: string
  action: string
  from_release_id?: string
  to_release_id?: string
  weights?: Record<string, number>
  created_by?: string
  created_at: string
}

export type RouteTraffic = {
  route_id: string
  hostname: string
  service_id: string
  service: string
  tls_mode: string
  active_release_id?: string
  prev_release_id?: string
  targets: RouteTrafficTarget[]
  history: RouteTrafficEvent[]
}

export type CreateRouteRequest = {
  hostname: string
  service_id: string
  edge_device_id?: string
}

/** Stage 8.1 — last telemetry snapshot. gpu is null when undetectable. */
export type ComputeStatus = 'available' | 'stale' | 'offline' | string

export type ComputeCPU = {
  cores: number
  architecture: string
  usage_percent?: number | null
}

export type ComputeMemory = {
  total_bytes: number
  available_bytes: number
  used_bytes: number
  usage_percent?: number | null
}

export type ComputeGPU = {
  vendor: string
  model: string
  vram_bytes: number | null
  available?: boolean | null
}

export type ComputeDisk = {
  mount: string
  name?: string
  fstype?: string
  total_bytes: number
  free_bytes: number
  used_bytes: number
}

export type ComputeDevice = {
  device_id: string
  name: string
  hostname: string
  os: string
  arch: string
  agent_version: string
  online: boolean
  status: ComputeStatus
  last_seen_at?: string | null
  last_telemetry_at?: string | null
  cpu: ComputeCPU | null
  memory: ComputeMemory | null
  gpu: ComputeGPU[] | null
  disks: ComputeDisk[]
  labels?: Record<string, string>
}

export type JobStatus = 'queued' | 'waiting_for_resource' | 'assigned' | 'running' | 'succeeded' | 'artifacts_committed' | 'failed' | 'timeout' | 'canceled' | 'rejected'

export type JobResources = {
  cpu: number
  memory_mb: number
  gpu: number
  pids?: number
  disk_mb?: number
}

export type JobArtifact = {
  artifact_id: string
  job_id: string
  file_id: string
  path: string
  name: string
  size: number
  sha256: string
  mime_type: string
  created_at: string
}

export type ComputeJob = {
  id: string
  job_id: string
  device_id: string
  device_name: string
  device_online: boolean
  image: string
  command: string[]
  env?: Record<string, string>
  resources: JobResources
  timeout_seconds: number
  input_path: string
  output_path: string
  status: JobStatus | string
  reason?: string
  exit_code: number | null
  error: string
  container_id: string
  created_at: string
  started_at?: string | null
  finished_at?: string | null
  artifacts?: JobArtifact[]
  placement?: string
  require?: Record<string, string>
  prefer?: Record<string, string>
  attempts?: number
  max_retries?: number
}

export type CreateJobRequest = {
  device_id?: string
  image: string
  command?: string[]
  env?: Record<string, string>
  resources?: Partial<JobResources>
  timeout_seconds?: number
  input_path?: string
  output_path?: string
  require?: Record<string, string>
  prefer?: Record<string, string>
  retry_max?: number
}

export type JobLog = {
  id: string
  stream: string
  message: string
  created_at: string
}

export type Secret = {
  id: string
  name: string
  version: number
  created_at: string
  updated_at: string
}

export type EnvironmentSecretRef = {
  key: string
  secret_id: string
  name: string
  version?: number
}

export type AppEnvironment = {
  id: string
  project: string
  name: string
  vars: Record<string, string>
  secrets: EnvironmentSecretRef[]
  policy?: Record<string, string>
  created_at: string
  updated_at: string
}

export type CreateEnvironmentRequest = {
  project?: string
  name: string
  vars?: Record<string, string>
  secrets?: Record<string, string>
  policy?: Record<string, string>
}

export type AppSource = {
  id: string
  type: string
  name: string
  url: string
  branch: string
  git_tag?: string
  revision?: string
  credential_secret_id?: string
  created_at: string
  updated_at: string
}

export type CreateSourceRequest = {
  type?: string
  name?: string
  url: string
  branch?: string
  git_tag?: string
  revision?: string
  credential_secret_id?: string
}

export type BuildStatus =
  | 'queued'
  | 'cloning'
  | 'building'
  | 'pushing'
  | 'completed'
  | 'failed_clone'
  | 'failed_build'
  | 'failed_push'
  | 'failed'
  | 'canceled'

export type AppBuild = {
  id: string
  source_id: string
  device_id: string
  device_name?: string
  device_online?: boolean
  dockerfile: string
  context: string
  tag: string
  image: string
  status: BuildStatus | string
  error: string
  revision?: string
  timeout_seconds?: number
  created_at: string
  started_at?: string | null
  finished_at?: string | null
  trace_id?: string
}

export type CreateBuildRequest = {
  source_id: string
  device_id: string
  dockerfile?: string
  context?: string
  tag: string
  timeout_seconds?: number
  registry_secret_id?: string
}

export type BuildLog = {
  id: string
  stream: string
  message: string
  created_at: string
}

export type ReleaseStatus =
  | 'created'
  | 'deploying'
  | 'testing'
  | 'active'
  | 'failed'
  | 'rolled_back'
  | 'cancelled'

export type SecretPin = {
  secret_id: string
  name: string
  version: number
}

export type AppRelease = {
  id: string
  number: number
  service: string
  image: string
  environment_id?: string
  environment?: string
  config_version?: string
  secrets?: Record<string, SecretPin>
  status: ReleaseStatus | string
  created_by?: string
  device_id?: string
  device_name?: string
  port?: number
  bind?: string
  health_path?: string
  health_timeout_seconds?: number
  health_retries?: number
  health_expected_status?: number
  build_id?: string
  job_id?: string
  deployment_id?: string
  prev_release_id?: string
  current: boolean
  error?: string
  trace_id?: string
  created_at: string
  updated_at: string
}

export type CreateReleaseRequest = {
  service: string
  image?: string
  environment?: string
  environment_id?: string
  project?: string
  device_id?: string
  port?: number
  bind?: string
  health_path?: string
  health_timeout_seconds?: number
  health_retries?: number
  health_expected_status?: number
  hostname?: string
  edge_device_id?: string
  build_id?: string
  job_id?: string
}

export type ReleaseLog = {
  id: string
  stream: string
  source: string
  message: string
  created_at: string
}

export type OpsLog = {
  id: string
  timestamp: string
  level: string
  source: string
  message: string
  trace_id?: string
  device_id?: string
  service_id?: string
  service?: string
  release_id?: string
  build_id?: string
  job_id?: string
  deployment_id?: string
  metadata?: Record<string, unknown>
}

export type ListLogsQuery = {
  service?: string
  service_id?: string
  release_id?: string
  build_id?: string
  job_id?: string
  source?: string
  trace_id?: string
  level?: string
  q?: string
  after?: string
  since?: string
  until?: string
  limit?: number
}

export type IngestLogRequest = {
  level?: string
  source?: string
  message: string
  trace_id?: string
  device_id?: string
  service_id?: string
  service?: string
  release_id?: string
  build_id?: string
  job_id?: string
  deployment_id?: string
  metadata?: Record<string, unknown>
}

export type OpsRelease = {
  id: string
  number: number
  image: string
  status: string
}

export type OpsTraffic = {
  hostname: string
  route_id: string
  active_release_id: string
  prev_release_id?: string
  weight: number
}

export type OpsDeploy = {
  id: string
  revision: number
  status: string
  health_ok: boolean
  created_at: string
}

export type OpsContext = {
  service: string
  service_id?: string
  node?: string
  node_id?: string
  status: string
  environment?: string
  current_release?: OpsRelease
  latest_release?: OpsRelease
  traffic?: OpsTraffic
  last_deploy?: OpsDeploy
  last_deploy_at?: string
  recent_errors: number
  health?: Record<string, unknown>
  trace_id?: string
  visible: string[]
  summary: string
}

export type WorkflowCatalogEntry = {
  name: string
  title: string
  steps: string[]
  mutating: boolean
}

export type WorkflowStep = {
  id: string
  seq: number
  name: string
  status: string
  scope: string
  error?: string
  output?: Record<string, unknown>
  trace_id?: string
  started_at?: string
  finished_at?: string
}

export type Workflow = {
  id: string
  name: string
  title?: string
  actor?: string
  status: string
  trace_id: string
  error?: string
  result?: Record<string, unknown>
  steps?: WorkflowStep[]
  created_at: string
  updated_at: string
  finished_at?: string
}

export type WorkflowList = {
  catalog: WorkflowCatalogEntry[]
  workflows: Workflow[]
}

export type RunWorkflowRequest = {
  name: string
  service?: string
  device_id?: string
  image?: string
  build_id?: string
  port?: number
  hostname?: string
  environment?: string
  query?: string
  path?: string
  from_device_id?: string
  to_device_id?: string
  to_path?: string
  job_image?: string
}

export type PlanCatalogEntry = {
  name: string
  title: string
  intent: string
  steps: string[]
  risk_level: string
  requires_approval: boolean
}

export type PlanStep = {
  id: string
  seq: number
  name: string
  title: string
  status: string
  scope: string
  risk_level: string
  error?: string
  output?: Record<string, unknown>
  trace_id?: string
  started_at?: string
  finished_at?: string
}

export type Plan = {
  id: string
  name: string
  title: string
  intent: string
  created_by: string
  ai_session_id?: string
  actor?: string
  trace_id: string
  risk_level: string
  status: string
  requires_approval: boolean
  error?: string
  result?: Record<string, unknown>
  input?: Record<string, unknown>
  approved_by?: string
  approved_at?: string
  created_at: string
  updated_at: string
  expires_at: string
  finished_at?: string
  steps?: PlanStep[]
}

export type PlanList = {
  catalog: PlanCatalogEntry[]
  plans: Plan[]
}

export type CreatePlanRequest = {
  intent?: string
  name?: string
  service?: string
  device_id?: string
  image?: string
  build_id?: string
  port?: number
  hostname?: string
  environment?: string
  query?: string
  path?: string
  from_device_id?: string
  to_device_id?: string
  to_path?: string
  job_image?: string
  ttl_minutes?: number
  expires_in?: string
  auto_execute?: boolean
}

/** Well-known API error codes (see pkg/apierrors). */
export const ErrorCodes = {
  unauthorized: 'unauthorized',
  forbidden: 'forbidden',
  not_found: 'not_found',
  invalid_credentials: 'invalid_credentials',
  token_expired: 'token_expired',
  token_revoked: 'token_revoked',
  validation_error: 'validation_error',
  conflict: 'conflict',
  quota_exceeded: 'quota_exceeded',
  internal: 'internal',
} as const

/** Recommended app scopes for a storage-capable shell. */
export const AppScopes = {
  storageViewer: ['devices.read', 'storage.read'] as const,
  storageEditor: ['devices.read', 'storage.read', 'storage.write'] as const,
  fullClient: [
    'devices.read',
    'devices.write',
    'storage.read',
    'storage.write',
    'services.read',
    'services.write',
    'compute.read',
    'network.transfer',
    'activity.read',
    'audit.read',
  ] as const,
}

import { NodeAPIError } from './errors.js'
import type {
  Credential,
  Device,
  Identity,
  LoginResult,
  Overview,
  Progress,
  StorageEntry,
  StorageFile,
  StoragePreview,
  StorageStat,
  StorageUploadRequest,
  SyncJob,
  CreateSyncJobRequest,
  SyncConflict,
  SyncFlushResult,
  FileHit,
  FileSearchQuery,
  FilesReindexResult,
  HostedService,
  ServiceNode,
  RegisterServiceRequest,
  UpdateServiceRequest,
  EdgeRoute,
  CreateRouteRequest,
  RouteTraffic,
  Transfer,
  ComputeDevice,
  ComputeJob,
  CreateJobRequest,
  JobArtifact,
  JobLog,
  Secret,
  AppEnvironment,
  CreateEnvironmentRequest,
  AppSource,
  CreateSourceRequest,
  AppBuild,
  CreateBuildRequest,
  BuildLog,
  AppRelease,
  CreateReleaseRequest,
  ReleaseLog,
  OpsLog,
  ListLogsQuery,
  IngestLogRequest,
  OpsContext,
  Workflow,
  WorkflowList,
  WorkflowStep,
  RunWorkflowRequest,
  AISession,
  CreateAISessionRequest,
  AuditEvent,
  AuditQuery,
  AIActivity,
  Plan,
  PlanList,
  CreatePlanRequest,
} from './types.js'

export type NodeClientOptions = {
  baseUrl: string
  token?: string
  fetch?: typeof fetch
}

function progressFrom(t: Transfer): Progress {
  let bytes = t.bytes_received || t.resume_offset || 0
  let percent = 0
  if (t.status === 'completed') {
    bytes = t.size
    percent = 100
  } else if (t.size > 0) {
    percent = Math.min(100, (bytes * 100) / t.size)
  }
  return {
    transfer_id: t.id,
    status: t.status,
    bytes_received: bytes,
    size: t.size,
    percent,
    path: t.path,
    file_id: t.file_id,
    error: t.error,
  }
}

function isTerminal(status: string): boolean {
  return status === 'completed' || status === 'failed' || status === 'aborted'
}

export class NodeClient {
  baseUrl: string
  token: string
  private readonly fetchImpl: typeof fetch

  constructor(opts: NodeClientOptions) {
    this.baseUrl = opts.baseUrl.replace(/\/+$/, '')
    this.token = opts.token ?? ''
    this.fetchImpl = opts.fetch ?? globalThis.fetch.bind(globalThis)
  }

  withToken(token: string): NodeClient {
    return new NodeClient({ baseUrl: this.baseUrl, token, fetch: this.fetchImpl })
  }

  private async request<T>(
    method: string,
    path: string,
    body?: unknown,
    auth = true,
  ): Promise<T> {
    const headers: Record<string, string> = {}
    if (body !== undefined) headers['Content-Type'] = 'application/json'
    if (auth && this.token) headers.Authorization = `Bearer ${this.token}`

    let lastErr: NodeAPIError | undefined
    for (let attempt = 0; attempt < 3; attempt++) {
      const res = await this.fetchImpl(`${this.baseUrl}${path}`, {
        method,
        headers,
        body: body !== undefined ? JSON.stringify(body) : undefined,
      })
      const raw = await res.text()
      let data: unknown = {}
      if (raw) {
        try {
          data = JSON.parse(raw)
        } catch {
          data = { error: { message: raw } }
        }
      }

      const retryable =
        res.status === 429 || (res.status >= 500 && res.status !== 507)
      if (retryable) {
        const errBody = data as { error?: { code?: string; message?: string } }
        lastErr = new NodeAPIError(
          res.status,
          errBody.error?.code ?? '',
          errBody.error?.message ?? raw,
        )
        await new Promise((r) => setTimeout(r, (attempt + 1) * 200))
        continue
      }
      if (!res.ok) {
        const errBody = data as { error?: { code?: string; message?: string } }
        throw new NodeAPIError(
          res.status,
          errBody.error?.code ?? '',
          errBody.error?.message ?? res.statusText,
        )
      }
      return data as T
    }
    throw lastErr ?? new NodeAPIError(500, 'internal', 'request failed')
  }

  async healthz(): Promise<void> {
    const res = await this.fetchImpl(`${this.baseUrl}/healthz`)
    if (!res.ok) throw new NodeAPIError(res.status, 'internal', 'healthz failed')
  }

  async login(email: string, password: string): Promise<LoginResult> {
    const out = await this.request<LoginResult>(
      'POST',
      '/v1/auth/login',
      { email, password },
      false,
    )
    this.token = out.access_token || out.token || ''
    return out
  }

  async refresh(refreshToken: string): Promise<LoginResult> {
    const out = await this.request<LoginResult>(
      'POST',
      '/v1/auth/refresh',
      { refresh_token: refreshToken },
      false,
    )
    this.token = out.access_token || out.token || ''
    return out
  }

  async logout(): Promise<void> {
    await this.request('POST', '/v1/auth/logout', {})
  }

  async me(): Promise<Identity> {
    return this.request('GET', '/v1/auth/me')
  }

  async listDevices(): Promise<Device[]> {
    const out = await this.request<{ devices: Device[] }>('GET', '/v1/devices')
    return out.devices ?? []
  }

  async getDevice(id: string): Promise<Device> {
    return this.request('GET', `/v1/devices/${id}`)
  }

  async overview(): Promise<Overview> {
    return this.request('GET', '/v1/overview')
  }

  async createRegToken(nameHint: string, ttlHours = 24): Promise<string> {
    const out = await this.request<{ token: string }>(
      'POST',
      '/v1/devices/registration-tokens',
      { name_hint: nameHint, ttl_hours: ttlHours },
    )
    return out.token
  }

  async listCredentials(): Promise<Credential[]> {
    const out = await this.request<{ credentials: Credential[] }>('GET', '/v1/credentials')
    return out.credentials ?? []
  }

  async createCredential(
    name: string,
    scopes: string[],
    ttlDays = 0,
  ): Promise<{ id: string; token: string }> {
    const out = await this.request<Credential>('POST', '/v1/credentials', {
      name,
      scopes,
      ttl_days: ttlDays,
    })
    return { id: out.id, token: out.token ?? '' }
  }

  async listAISessions(): Promise<AISession[]> {
    const out = await this.request<{ sessions: AISession[] }>('GET', '/v1/ai/sessions')
    return out.sessions ?? []
  }

  async createAISession(req: CreateAISessionRequest): Promise<AISession> {
    return this.request('POST', '/v1/ai/sessions', req)
  }

  async getAISession(id: string): Promise<AISession> {
    return this.request('GET', `/v1/ai/sessions/${id}`)
  }

  async currentAISession(): Promise<AISession> {
    return this.request('GET', '/v1/ai/sessions/current')
  }

  async revokeAISession(id: string): Promise<void> {
    await this.request('DELETE', `/v1/ai/sessions/${id}`)
  }

  async getTransfer(id: string): Promise<Transfer> {
    return this.request('GET', `/v1/transfers/${id}`)
  }

  async listTransfers(): Promise<Transfer[]> {
    const out = await this.request<{ transfers: Transfer[] }>('GET', '/v1/transfers')
    return out.transfers ?? []
  }

  async abortTransfer(id: string): Promise<void> {
    await this.request('POST', `/v1/transfers/${id}/abort`, {})
  }

  async watchTransfer(
    id: string,
    opts: { pollMs?: number; onProgress?: (p: Progress) => void; signal?: AbortSignal } = {},
  ): Promise<Transfer> {
    const poll = opts.pollMs ?? 300
    let lastBytes = -1
    let lastStatus = ''
    for (;;) {
      if (opts.signal?.aborted) throw new DOMException('Aborted', 'AbortError')
      const t = await this.getTransfer(id)
      const p = progressFrom(t)
      if (opts.onProgress && (p.bytes_received !== lastBytes || t.status !== lastStatus)) {
        opts.onProgress(p)
        lastBytes = p.bytes_received
        lastStatus = t.status
      }
      if (isTerminal(t.status)) return t
      await new Promise((r) => setTimeout(r, poll))
    }
  }

  async storageList(deviceId: string, path = ''): Promise<StorageEntry[]> {
    const q = new URLSearchParams({ device_id: deviceId })
    if (path) q.set('path', path)
    const out = await this.request<{ entries: StorageEntry[] }>(
      'GET',
      `/v1/storage/list?${q}`,
    )
    return out.entries ?? []
  }

  async storageStat(deviceId: string, path: string): Promise<StorageStat> {
    const q = new URLSearchParams({ device_id: deviceId, path })
    return this.request('GET', `/v1/storage/stat?${q}`)
  }

  async storageMkdir(deviceId: string, path: string): Promise<StorageStat> {
    return this.request('POST', '/v1/storage/mkdir', { device_id: deviceId, path })
  }

  async storageDelete(deviceId: string, path: string): Promise<void> {
    const q = new URLSearchParams({ device_id: deviceId, path })
    await this.request('DELETE', `/v1/storage/object?${q}`)
  }

  async storageMove(deviceId: string, fromPath: string, toPath: string): Promise<StorageStat> {
    return this.request('POST', '/v1/storage/move', {
      device_id: deviceId,
      from_path: fromPath,
      to_path: toPath,
    })
  }

  async storageCopy(deviceId: string, fromPath: string, toPath: string): Promise<StorageStat> {
    return this.request('POST', '/v1/storage/copy', {
      device_id: deviceId,
      from_path: fromPath,
      to_path: toPath,
    })
  }

  /** Cross-node storage→storage (or same-node copy). */
  async storageTransfer(
    fromDeviceId: string,
    fromPath: string,
    toDeviceId: string,
    toPath: string,
  ): Promise<Transfer & { mode?: string }> {
    return this.request('POST', '/v1/storage/transfer', {
      from_device_id: fromDeviceId,
      from_path: fromPath,
      to_device_id: toDeviceId,
      to_path: toPath,
    })
  }

  async storageUpload(req: StorageUploadRequest): Promise<Transfer> {
    return this.request('POST', '/v1/storage/upload', {
      device_id: req.device_id,
      path: req.path,
      from_device_id: req.from_device_id,
      source_path: req.source_path,
      size: req.size,
      sha256: req.sha256,
      resume: !!req.resume,
    })
  }

  async storageRead(deviceId: string, path: string, toDeviceId: string): Promise<Transfer> {
    const q = new URLSearchParams({
      device_id: deviceId,
      path,
      to_device_id: toDeviceId,
    })
    return this.request('GET', `/v1/storage/read?${q}`)
  }

  async storageGetFile(fileId: string): Promise<StorageFile> {
    return this.request('GET', `/v1/storage/files/${fileId}`)
  }

  async storageContent(deviceId: string, path: string): Promise<{ data: Blob; contentType: string }> {
    const q = new URLSearchParams({ device_id: deviceId, path })
    const headers: Record<string, string> = {}
    if (this.token) headers.Authorization = `Bearer ${this.token}`
    const res = await this.fetchImpl(`${this.baseUrl}/v1/storage/content?${q}`, { headers })
    if (!res.ok) {
      const raw = await res.text()
      let code = ''
      let message = raw
      try {
        const j = JSON.parse(raw) as { error?: { code?: string; message?: string } }
        code = j.error?.code ?? ''
        message = j.error?.message ?? raw
      } catch { /* */ }
      throw new NodeAPIError(res.status, code, message)
    }
    return { data: await res.blob(), contentType: res.headers.get('Content-Type') || 'application/octet-stream' }
  }

  async storagePreview(
    deviceId: string,
    path: string,
    opts?: { variant?: 'thumb' | 'preview'; maxPixels?: number },
  ): Promise<StoragePreview> {
    const q = new URLSearchParams({ device_id: deviceId, path })
    if (opts?.variant) q.set('variant', opts.variant)
    if (opts?.maxPixels) q.set('max_pixels', String(opts.maxPixels))
    const headers: Record<string, string> = {}
    if (this.token) headers.Authorization = `Bearer ${this.token}`
    const res = await this.fetchImpl(`${this.baseUrl}/v1/storage/preview?${q}`, { headers })
    if (!res.ok) {
      const raw = await res.text()
      let code = ''
      let message = raw
      try {
        const j = JSON.parse(raw) as { error?: { code?: string; message?: string } }
        code = j.error?.code ?? ''
        message = j.error?.message ?? raw
      } catch { /* */ }
      throw new NodeAPIError(res.status, code, message)
    }
    return {
      data: await res.blob(),
      contentType: res.headers.get('Content-Type') || 'application/octet-stream',
      cacheKey: res.headers.get('X-Knot-Preview-Cache-Key') || undefined,
      sourceMime: res.headers.get('X-Knot-Preview-Source-Mime') || undefined,
    }
  }

  /** Direct byte put (≤ agent write path). Used by the Web shell for local upload. */
  async storagePutContent(
    deviceId: string,
    path: string,
    body: ArrayBuffer | Uint8Array | Blob,
    opts: {
      sha256: string
      overwrite?: boolean
      conflict?: 'rename' | 'overwrite'
      contentType?: string
    },
  ): Promise<void> {
    const q = new URLSearchParams({
      device_id: deviceId,
      path,
      sha256: opts.sha256,
    })
    if (opts.overwrite || opts.conflict === 'overwrite') q.set('overwrite', 'true')
    if (opts.conflict === 'rename') q.set('conflict', 'rename')
    const headers: Record<string, string> = {
      'Content-Type': opts.contentType || 'application/octet-stream',
    }
    if (this.token) headers.Authorization = `Bearer ${this.token}`
    const res = await this.fetchImpl(`${this.baseUrl}/v1/storage/content?${q}`, {
      method: 'PUT',
      headers,
      body: body as BodyInit,
    })
    if (!res.ok) {
      const raw = await res.text()
      let code = ''
      let message = raw
      try {
        const j = JSON.parse(raw) as { error?: { code?: string; message?: string } }
        code = j.error?.code ?? ''
        message = j.error?.message ?? raw
      } catch { /* */ }
      throw new NodeAPIError(res.status, code, message)
    }
  }

  async listSyncJobs(): Promise<SyncJob[]> {
    const out = await this.request<{ jobs: SyncJob[] }>('GET', '/v1/sync/jobs')
    return out.jobs ?? []
  }

  async createSyncJob(req: CreateSyncJobRequest): Promise<SyncJob> {
    return this.request('POST', '/v1/sync/jobs', {
      name: req.name,
      mode: req.mode ?? 'one_way',
      source_device_id: req.source_device_id,
      source_path: req.source_path,
      dest_device_id: req.dest_device_id,
      dest_path: req.dest_path,
    })
  }

  async getSyncJob(id: string): Promise<SyncJob> {
    return this.request('GET', `/v1/sync/jobs/${id}`)
  }

  async runSyncJob(id: string): Promise<SyncJob> {
    return this.request('POST', `/v1/sync/jobs/${id}/run`, {})
  }

  async cancelSyncJob(id: string): Promise<SyncJob> {
    return this.request('POST', `/v1/sync/jobs/${id}/cancel`, {})
  }

  async deleteSyncJob(id: string): Promise<void> {
    await this.request('DELETE', `/v1/sync/jobs/${id}`)
  }

  async listSyncConflicts(jobId: string, opts?: { openOnly?: boolean }): Promise<SyncConflict[]> {
    const q = opts?.openOnly === false ? '?open=false' : ''
    const out = await this.request<{ conflicts: SyncConflict[] }>('GET', `/v1/sync/jobs/${jobId}/conflicts${q}`)
    return out.conflicts ?? []
  }

  async resolveSyncConflict(conflictId: string, resolution: 'keep_a' | 'keep_b' | 'keep_both'): Promise<SyncConflict> {
    return this.request('POST', `/v1/sync/conflicts/${conflictId}/resolve`, { resolution })
  }

  async batchResolveSyncConflicts(
    conflictIds: string[],
    resolution: 'keep_a' | 'keep_b' | 'keep_both',
  ): Promise<{ resolved: SyncConflict[]; errors: string[] }> {
    return this.request('POST', '/v1/sync/conflicts/batch-resolve', {
      conflict_ids: conflictIds,
      resolution,
    })
  }

  async flushSync(deviceId: string): Promise<SyncFlushResult> {
    return this.request('POST', '/v1/sync/flush', { device_id: deviceId })
  }

  async getSyncFlushStatus(deviceId: string): Promise<{
    device_id: string
    job_ids: string[]
    conflict_paths: string[]
    conflicts_open: number
  }> {
    return this.request('GET', `/v1/sync/flush/${deviceId}`)
  }

  async filesSearch(query: FileSearchQuery = {}): Promise<FileHit[]> {
    const q = new URLSearchParams()
    if (query.q) q.set('q', query.q)
    if (query.device_id) q.set('device_id', query.device_id)
    if (query.folder) q.set('folder', query.folder)
    if (query.type) q.set('type', query.type)
    if (query.min_size != null) q.set('min_size', String(query.min_size))
    if (query.max_size != null) q.set('max_size', String(query.max_size))
    if (query.modified_after) q.set('modified_after', query.modified_after)
    if (query.modified_before) q.set('modified_before', query.modified_before)
    if (query.is_directory != null) q.set('is_directory', query.is_directory ? 'true' : 'false')
    if (query.limit != null) q.set('limit', String(query.limit))
    const qs = q.toString()
    const out = await this.request<{ files: FileHit[] }>('GET', `/v1/files/search${qs ? `?${qs}` : ''}`)
    return out.files ?? []
  }

  async filesReindex(deviceId?: string): Promise<FilesReindexResult> {
    return this.request('POST', '/v1/files/reindex', deviceId ? { device_id: deviceId } : {})
  }

  async listServices(deviceId?: string): Promise<HostedService[]> {
    const q = new URLSearchParams()
    if (deviceId) q.set('device_id', deviceId)
    const qs = q.toString()
    const out = await this.request<{ services: HostedService[] }>('GET', `/v1/services${qs ? `?${qs}` : ''}`)
    return out.services ?? []
  }

  async servicesTree(): Promise<ServiceNode[]> {
    const out = await this.request<{ nodes: ServiceNode[] }>('GET', '/v1/services/tree')
    return out.nodes ?? []
  }

  async registerService(req: RegisterServiceRequest): Promise<HostedService> {
    return this.request('POST', '/v1/services', req)
  }

  async getService(id: string): Promise<HostedService> {
    return this.request('GET', `/v1/services/${id}`)
  }

  async updateService(id: string, req: UpdateServiceRequest): Promise<HostedService> {
    return this.request('PATCH', `/v1/services/${id}`, req)
  }

  async deleteService(id: string): Promise<void> {
    await this.request('DELETE', `/v1/services/${id}`)
  }

  async serviceHealth(id: string): Promise<HostedService> {
    return this.request('GET', `/v1/services/${id}/health`)
  }

  async listRoutes(): Promise<EdgeRoute[]> {
    const out = await this.request<{ routes: EdgeRoute[] }>('GET', '/v1/routes')
    return out.routes ?? []
  }

  async createRoute(req: CreateRouteRequest): Promise<EdgeRoute> {
    return this.request('POST', '/v1/routes', req)
  }

  async deleteRoute(id: string): Promise<void> {
    await this.request('DELETE', `/v1/routes/${id}`)
  }

  async listComputeDevices(): Promise<ComputeDevice[]> {
    const out = await this.request<{ devices: ComputeDevice[] }>('GET', '/v1/compute/devices')
    return out.devices ?? []
  }

  async getComputeDevice(deviceId: string): Promise<ComputeDevice> {
    return this.request('GET', `/v1/compute/devices/${deviceId}`)
  }

  async setComputeLabels(deviceId: string, labels: Record<string, string>): Promise<ComputeDevice> {
    return this.request('PUT', `/v1/compute/devices/${deviceId}/labels`, { labels })
  }

  async listJobs(deviceId?: string): Promise<ComputeJob[]> {
    const q = deviceId ? `?device_id=${encodeURIComponent(deviceId)}` : ''
    const out = await this.request<{ jobs: ComputeJob[] }>('GET', `/v1/compute/jobs${q}`)
    return out.jobs ?? []
  }

  async createJob(req: CreateJobRequest): Promise<ComputeJob> {
    return this.request('POST', '/v1/compute/jobs', req)
  }

  async getJob(id: string): Promise<ComputeJob> {
    return this.request('GET', `/v1/compute/jobs/${id}`)
  }

  async cancelJob(id: string): Promise<ComputeJob> {
    return this.request('POST', `/v1/compute/jobs/${id}/cancel`, {})
  }

  async jobLogs(id: string, limit = 200): Promise<JobLog[]> {
    const q = limit > 0 ? `?limit=${limit}` : ''
    const out = await this.request<{ logs: JobLog[] }>('GET', `/v1/compute/jobs/${id}/logs${q}`)
    return out.logs ?? []
  }

  async jobArtifacts(id: string): Promise<JobArtifact[]> {
    const out = await this.request<{ artifacts: JobArtifact[] }>('GET', `/v1/compute/jobs/${id}/artifacts`)
    return out.artifacts ?? []
  }

  async listSecrets(): Promise<Secret[]> {
    const out = await this.request<{ secrets: Secret[] }>('GET', '/v1/secrets')
    return out.secrets ?? []
  }

  async createSecret(name: string, value: string): Promise<Secret> {
    return this.request('POST', '/v1/secrets', { name, value })
  }

  async getSecret(id: string): Promise<Secret> {
    return this.request('GET', `/v1/secrets/${id}`)
  }

  async rotateSecret(id: string, value: string): Promise<Secret> {
    return this.request('PUT', `/v1/secrets/${id}`, { value })
  }

  async listEnvironments(project?: string): Promise<AppEnvironment[]> {
    const q = project ? `?project=${encodeURIComponent(project)}` : ''
    const out = await this.request<{ environments: AppEnvironment[] }>('GET', `/v1/environments${q}`)
    return out.environments ?? []
  }

  async createEnvironment(req: CreateEnvironmentRequest): Promise<AppEnvironment> {
    return this.request('POST', '/v1/environments', req)
  }

  async getEnvironment(id: string): Promise<AppEnvironment> {
    return this.request('GET', `/v1/environments/${id}`)
  }

  async listSources(): Promise<AppSource[]> {
    const out = await this.request<{ sources: AppSource[] }>('GET', '/v1/sources')
    return out.sources ?? []
  }

  async createSource(req: CreateSourceRequest): Promise<AppSource> {
    return this.request('POST', '/v1/sources', req)
  }

  async getSource(id: string): Promise<AppSource> {
    return this.request('GET', `/v1/sources/${id}`)
  }

  async listBuilds(opts?: { sourceId?: string; deviceId?: string }): Promise<AppBuild[]> {
    const q = new URLSearchParams()
    if (opts?.sourceId) q.set('source_id', opts.sourceId)
    if (opts?.deviceId) q.set('device_id', opts.deviceId)
    const suffix = q.toString() ? `?${q.toString()}` : ''
    const out = await this.request<{ builds: AppBuild[] }>('GET', `/v1/builds${suffix}`)
    return out.builds ?? []
  }

  async createBuild(req: CreateBuildRequest): Promise<AppBuild> {
    return this.request('POST', '/v1/builds', req)
  }

  async getBuild(id: string): Promise<AppBuild> {
    return this.request('GET', `/v1/builds/${id}`)
  }

  async buildLogs(id: string, limit?: number): Promise<BuildLog[]> {
    const q = limit ? `?limit=${encodeURIComponent(String(limit))}` : ''
    const out = await this.request<{ logs: BuildLog[] }>('GET', `/v1/builds/${id}/logs${q}`)
    return out.logs ?? []
  }

  async listReleases(service?: string): Promise<AppRelease[]> {
    const q = service ? `?service=${encodeURIComponent(service)}` : ''
    const out = await this.request<{ releases: AppRelease[] }>('GET', `/v1/releases${q}`)
    return out.releases ?? []
  }

  async createRelease(req: CreateReleaseRequest): Promise<AppRelease> {
    return this.request('POST', '/v1/releases', req)
  }

  async getRelease(id: string): Promise<AppRelease> {
    return this.request('GET', `/v1/releases/${id}`)
  }

  async deployRelease(id: string, opts?: { deviceId?: string; port?: number }): Promise<AppRelease> {
    const body: Record<string, unknown> = {}
    if (opts?.deviceId) body.device_id = opts.deviceId
    if (opts?.port) body.port = opts.port
    return this.request('POST', `/v1/releases/${id}/deploy`, body)
  }

  async rollbackRelease(id: string): Promise<AppRelease> {
    return this.request('POST', `/v1/releases/${id}/rollback`, {})
  }

  async releaseLogs(id: string, limit?: number): Promise<ReleaseLog[]> {
    const q = limit ? `?limit=${encodeURIComponent(String(limit))}` : ''
    const out = await this.request<{ logs: ReleaseLog[] }>('GET', `/v1/releases/${id}/logs${q}`)
    return out.logs ?? []
  }

  async getRouteTraffic(idOrHost: string): Promise<RouteTraffic> {
    return this.request('GET', `/v1/routes/${encodeURIComponent(idOrHost)}/traffic`)
  }

  async switchRouteTraffic(idOrHost: string, releaseId: string, weight?: number): Promise<RouteTraffic> {
    return this.request('POST', `/v1/routes/${encodeURIComponent(idOrHost)}/switch`, {
      release_id: releaseId,
      weight: weight ?? 100,
    })
  }

  async rollbackRouteTraffic(idOrHost: string): Promise<RouteTraffic> {
    return this.request('POST', `/v1/routes/${encodeURIComponent(idOrHost)}/rollback`, {})
  }

  async listLogs(q: ListLogsQuery = {}): Promise<OpsLog[]> {
    const v = new URLSearchParams()
    if (q.service) v.set('service', q.service)
    if (q.service_id) v.set('service_id', q.service_id)
    if (q.release_id) v.set('release_id', q.release_id)
    if (q.build_id) v.set('build_id', q.build_id)
    if (q.job_id) v.set('job_id', q.job_id)
    if (q.source) v.set('source', q.source)
    if (q.trace_id) v.set('trace_id', q.trace_id)
    if (q.level) v.set('level', q.level)
    if (q.q) v.set('q', q.q)
    if (q.after) v.set('after', q.after)
    if (q.since) v.set('since', q.since)
    if (q.until) v.set('until', q.until)
    if (q.limit) v.set('limit', String(q.limit))
    const qs = v.toString()
    const out = await this.request<{ logs: OpsLog[] }>('GET', `/v1/logs${qs ? `?${qs}` : ''}`)
    return out.logs ?? []
  }

  async ingestLog(req: IngestLogRequest): Promise<OpsLog> {
    return this.request('POST', '/v1/logs', req)
  }

  async opsContext(service: string, deviceId?: string): Promise<OpsContext> {
    const v = new URLSearchParams({ service })
    if (deviceId) v.set('device_id', deviceId)
    return this.request('GET', `/v1/ops/context?${v.toString()}`)
  }

  async listWorkflows(): Promise<WorkflowList> {
    const out = await this.request<WorkflowList>('GET', '/v1/workflows')
    return {
      catalog: out.catalog ?? [],
      workflows: out.workflows ?? [],
    }
  }

  async runWorkflow(req: RunWorkflowRequest): Promise<Workflow> {
    return this.request('POST', '/v1/workflows/run', req)
  }

  async getWorkflow(id: string): Promise<Workflow> {
    return this.request('GET', `/v1/workflows/${id}`)
  }

  async workflowSteps(id: string): Promise<WorkflowStep[]> {
    const out = await this.request<{ steps: WorkflowStep[] }>('GET', `/v1/workflows/${id}/steps`)
    return out.steps ?? []
  }

  async searchAudit(q: AuditQuery = {}): Promise<AuditEvent[]> {
    const v = new URLSearchParams()
    if (q.action) v.set('action', q.action)
    if (q.actor_type) v.set('actor_type', q.actor_type)
    if (q.actor_id) v.set('actor_id', q.actor_id)
    if (q.ai_session_id) v.set('ai_session_id', q.ai_session_id)
    if (q.workflow_id) v.set('workflow_id', q.workflow_id)
    if (q.trace_id) v.set('trace_id', q.trace_id)
    if (q.mcp_client) v.set('mcp_client', q.mcp_client)
    if (q.q) v.set('q', q.q)
    if (q.limit) v.set('limit', String(q.limit))
    const qs = v.toString()
    const out = await this.request<{ events: AuditEvent[] }>('GET', `/v1/audit${qs ? `?${qs}` : ''}`)
    return out.events ?? []
  }

  async aiActivity(q: Pick<AuditQuery, 'ai_session_id' | 'workflow_id' | 'trace_id' | 'mcp_client' | 'limit'> = {}): Promise<AIActivity[]> {
    const v = new URLSearchParams()
    if (q.ai_session_id) v.set('ai_session_id', q.ai_session_id)
    if (q.workflow_id) v.set('workflow_id', q.workflow_id)
    if (q.trace_id) v.set('trace_id', q.trace_id)
    if (q.mcp_client) v.set('mcp_client', q.mcp_client)
    if (q.limit) v.set('limit', String(q.limit))
    const qs = v.toString()
    const out = await this.request<{ activities: AIActivity[] }>('GET', `/v1/audit/ai${qs ? `?${qs}` : ''}`)
    return out.activities ?? []
  }

  async auditTrace(traceId: string): Promise<AuditEvent[]> {
    const out = await this.request<{ events: AuditEvent[] }>('GET', `/v1/audit/trace/${encodeURIComponent(traceId)}`)
    return out.events ?? []
  }

  async listPlans(): Promise<PlanList> {
    const out = await this.request<PlanList>('GET', '/v1/plans')
    return { catalog: out.catalog ?? [], plans: out.plans ?? [] }
  }

  async createPlan(req: CreatePlanRequest): Promise<Plan> {
    return this.request('POST', '/v1/plans', req)
  }

  async getPlan(id: string): Promise<Plan> {
    return this.request('GET', `/v1/plans/${id}`)
  }

  async approvePlan(id: string): Promise<Plan> {
    return this.request('POST', `/v1/plans/${id}/approve`, {})
  }

  async executePlan(id: string): Promise<Plan> {
    return this.request('POST', `/v1/plans/${id}/execute`, {})
  }

  async cancelPlan(id: string): Promise<Plan> {
    return this.request('POST', `/v1/plans/${id}/cancel`, {})
  }
}

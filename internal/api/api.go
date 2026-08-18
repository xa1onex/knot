package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/knot-infra/knot/internal/agentws"
	"github.com/knot-infra/knot/internal/aisessions"
	"github.com/knot-infra/knot/internal/audit"
	"github.com/knot-infra/knot/internal/auth"
	"github.com/knot-infra/knot/internal/builds"
	"github.com/knot-infra/knot/internal/compute"
	"github.com/knot-infra/knot/internal/config"
	"github.com/knot-infra/knot/internal/deploy"
	"github.com/knot-infra/knot/internal/devices"
	"github.com/knot-infra/knot/internal/edge"
	"github.com/knot-infra/knot/internal/environments"
	"github.com/knot-infra/knot/internal/files"
	"github.com/knot-infra/knot/internal/hardening"
	"github.com/knot-infra/knot/internal/jobs"
	"github.com/knot-infra/knot/internal/oplogs"
	"github.com/knot-infra/knot/internal/ops"
	"github.com/knot-infra/knot/internal/plans"
	"github.com/knot-infra/knot/internal/releases"
	"github.com/knot-infra/knot/internal/secrets"
	"github.com/knot-infra/knot/internal/services"
	"github.com/knot-infra/knot/internal/selfupdate"
	"github.com/knot-infra/knot/internal/storage"
	"github.com/knot-infra/knot/internal/store"
	syncjob "github.com/knot-infra/knot/internal/sync"
	"github.com/knot-infra/knot/internal/traffic"
	"github.com/knot-infra/knot/internal/transfers"
	"github.com/knot-infra/knot/internal/workflows"
	"github.com/knot-infra/knot/pkg/apierrors"
	"github.com/knot-infra/knot/pkg/permissions"
	"github.com/knot-infra/knot/pkg/protocol"
)

type Server struct {
	Cfg          config.Config
	Store        *store.Store
	Auth         *auth.Service
	Devices      *devices.Service
	Audit        *audit.Logger
	Hub          *agentws.Hub
	Transfers    *transfers.Service
	Storage      *storage.Service
	Sync         *syncjob.Service
	Files        *files.Service
	Services     *services.Service
	Edge         *edge.Proxy
	Environments *environments.Service
	Secrets      *secrets.Service
	Deploy       *deploy.Service
	Releases     *releases.Service
	Traffic      *traffic.Service
	Builds       *builds.Service
	Compute      *compute.Service
	Jobs         *jobs.Service
	Logs         *oplogs.Service
	Ops          *ops.Service
	Updates      *selfupdate.Service
	Workflows    *workflows.Service
	Plans        *plans.Service
	AI           *aisessions.Service
	Gate         *hardening.Gate
	Metrics      *hardening.Metrics
	StartedAt    time.Time
}

type ctxKey int

const identityKey ctxKey = 1

func IdentityFrom(ctx context.Context) *auth.Identity {
	v, _ := ctx.Value(identityKey).(*auth.Identity)
	return v
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("POST /v1/auth/login", s.handleLogin)
	mux.HandleFunc("POST /v1/auth/refresh", s.handleRefresh)
	mux.HandleFunc("POST /v1/auth/logout", s.withAuth(s.handleLogout, permissions.DevicesRead))
	mux.HandleFunc("POST /v1/auth/logout-all", s.withAuth(s.handleLogoutAll, permissions.AccountAdmin))
	mux.HandleFunc("GET /v1/auth/me", s.withAuth(s.handleMe, permissions.DevicesRead))

	mux.HandleFunc("GET /v1/devices", s.withAuth(s.handleListDevices, permissions.DevicesRead))
	mux.HandleFunc("GET /v1/devices/{id}", s.withAuth(s.handleGetDevice, permissions.DevicesRead))
	mux.HandleFunc("POST /v1/devices/{id}/revoke", s.withAuth(s.handleRevokeDevice, permissions.DevicesWrite))
	mux.HandleFunc("POST /v1/devices/registration-tokens", s.withAuth(s.handleCreateRegToken, permissions.DevicesWrite))

	mux.HandleFunc("GET /v1/credentials", s.withAuth(s.handleListCredentials, permissions.CredentialsRW))
	mux.HandleFunc("POST /v1/credentials", s.withAuth(s.handleCreateCredential, permissions.CredentialsRW))
	mux.HandleFunc("POST /v1/credentials/{id}/revoke", s.withAuth(s.handleRevokeCredential, permissions.CredentialsRW))
	mux.HandleFunc("POST /v1/credentials/{id}/rotate", s.withAuth(s.handleRotateCredential, permissions.CredentialsRW))

	mux.HandleFunc("GET /v1/activity", s.withAuth(s.handleActivity, permissions.ActivityRead))
	mux.HandleFunc("GET /v1/audit", s.withAuth(s.handleAuditSearch, permissions.AuditRead))
	mux.HandleFunc("GET /v1/audit/ai", s.withAuth(s.handleAuditAI, permissions.AuditRead))
	mux.HandleFunc("GET /v1/audit/trace/{id}", s.withAuth(s.handleAuditTrace, permissions.AuditRead))
	mux.HandleFunc("GET /v1/overview", s.withAuth(s.handleOverview, permissions.DevicesRead))

	mux.HandleFunc("GET /v1/transfers", s.withAuthAny(s.handleListTransfers, permissions.NetworkTransfer, permissions.StorageRead, permissions.StorageWrite))
	mux.HandleFunc("POST /v1/transfers", s.withAuth(s.handleCreateTransfer, permissions.NetworkTransfer))
	mux.HandleFunc("GET /v1/transfers/{id}", s.withAuthAny(s.handleGetTransfer, permissions.NetworkTransfer, permissions.StorageRead, permissions.StorageWrite))
	mux.HandleFunc("POST /v1/transfers/{id}/abort", s.withAuthAny(s.handleAbortTransfer, permissions.NetworkTransfer, permissions.StorageWrite))

	mux.HandleFunc("GET /v1/storage/list", s.withAuth(s.handleStorageList, permissions.StorageRead))
	mux.HandleFunc("GET /v1/storage/stat", s.withAuth(s.handleStorageStat, permissions.StorageRead))
	mux.HandleFunc("GET /v1/storage/read", s.withAuth(s.handleStorageRead, permissions.StorageRead))
	mux.HandleFunc("GET /v1/storage/files/{id}", s.withAuth(s.handleStorageGetFile, permissions.StorageRead))
	mux.HandleFunc("POST /v1/storage/upload", s.withAuth(s.handleStorageUpload, permissions.StorageWrite))
	mux.HandleFunc("POST /v1/storage/mkdir", s.withAuth(s.handleStorageMkdir, permissions.StorageWrite))
	mux.HandleFunc("POST /v1/storage/move", s.withAuth(s.handleStorageMove, permissions.StorageWrite))
	mux.HandleFunc("POST /v1/storage/copy", s.withAuth(s.handleStorageCopy, permissions.StorageWrite))
	mux.HandleFunc("POST /v1/storage/transfer", s.withAuth(s.handleStorageTransfer, permissions.StorageWrite))
	mux.HandleFunc("PUT /v1/storage/content", s.withAuth(s.handleStoragePut, permissions.StorageWrite))
	mux.HandleFunc("GET /v1/storage/content", s.withAuth(s.handleStorageContent, permissions.StorageRead))
	mux.HandleFunc("GET /v1/storage/preview", s.withAuth(s.handleStoragePreview, permissions.StorageRead))
	mux.HandleFunc("DELETE /v1/storage/object", s.withAuth(s.handleStorageDelete, permissions.StorageWrite))

	mux.HandleFunc("GET /v1/sync/jobs", s.withAuth(s.handleListSyncJobs, permissions.StorageRead))
	mux.HandleFunc("POST /v1/sync/jobs", s.withAuth(s.handleCreateSyncJob, permissions.StorageWrite))
	mux.HandleFunc("GET /v1/sync/jobs/{id}", s.withAuth(s.handleGetSyncJob, permissions.StorageRead))
	mux.HandleFunc("POST /v1/sync/jobs/{id}/run", s.withAuth(s.handleRunSyncJob, permissions.StorageWrite))
	mux.HandleFunc("POST /v1/sync/jobs/{id}/cancel", s.withAuth(s.handleCancelSyncJob, permissions.StorageWrite))
	mux.HandleFunc("DELETE /v1/sync/jobs/{id}", s.withAuth(s.handleDeleteSyncJob, permissions.StorageWrite))
	mux.HandleFunc("GET /v1/sync/jobs/{id}/files", s.withAuth(s.handleListSyncFiles, permissions.StorageRead))
	mux.HandleFunc("GET /v1/sync/jobs/{id}/conflicts", s.withAuth(s.handleListSyncConflicts, permissions.StorageRead))
	mux.HandleFunc("POST /v1/sync/conflicts/{conflict_id}/resolve", s.withAuth(s.handleResolveSyncConflict, permissions.StorageWrite))
	mux.HandleFunc("POST /v1/sync/conflicts/batch-resolve", s.withAuth(s.handleBatchResolveSyncConflicts, permissions.StorageWrite))
	mux.HandleFunc("POST /v1/sync/flush", s.withAuth(s.handleFlushSync, permissions.StorageWrite))
	mux.HandleFunc("GET /v1/sync/flush/{device_id}", s.withAuth(s.handleGetFlushSync, permissions.StorageRead))

	mux.HandleFunc("GET /v1/files/search", s.withAuth(s.handleFilesSearch, permissions.StorageRead))
	mux.HandleFunc("POST /v1/files/reindex", s.withAuth(s.handleFilesReindex, permissions.StorageWrite))

	mux.HandleFunc("GET /v1/services", s.withAuth(s.handleListServices, permissions.ServicesRead))
	mux.HandleFunc("GET /v1/services/tree", s.withAuth(s.handleServicesTree, permissions.ServicesRead))
	mux.HandleFunc("POST /v1/services", s.withAuth(s.handleCreateService, permissions.ServicesWrite))
	mux.HandleFunc("GET /v1/services/{id}", s.withAuth(s.handleGetService, permissions.ServicesRead))
	mux.HandleFunc("GET /v1/services/{id}/health", s.withAuth(s.handleServiceHealth, permissions.ServicesRead))
	mux.HandleFunc("PATCH /v1/services/{id}", s.withAuth(s.handleUpdateService, permissions.ServicesWrite))
	mux.HandleFunc("DELETE /v1/services/{id}", s.withAuth(s.handleDeleteService, permissions.ServicesWrite))

	mux.HandleFunc("GET /v1/routes", s.withAuth(s.handleListRoutes, permissions.ServicesRead))
	mux.HandleFunc("POST /v1/routes", s.withAuth(s.handleCreateRoute, permissions.ServicesWrite))
	mux.HandleFunc("DELETE /v1/routes/{id}", s.withAuth(s.handleDeleteRoute, permissions.ServicesWrite))
	mux.HandleFunc("GET /v1/routes/{id}/traffic", s.withAuth(s.handleRouteTraffic, permissions.TrafficRead))
	mux.HandleFunc("POST /v1/routes/{id}/switch", s.withAuth(s.handleRouteTrafficSwitch, permissions.TrafficWrite))
	mux.HandleFunc("POST /v1/routes/{id}/rollback", s.withAuth(s.handleRouteTrafficRollback, permissions.TrafficWrite))

	mux.HandleFunc("GET /v1/deployments", s.withAuth(s.handleListDeployments, permissions.DeployRead))
	mux.HandleFunc("POST /v1/deployments", s.withAuth(s.handleCreateDeployment, permissions.DeployWrite))
	mux.HandleFunc("GET /v1/deployments/{id}", s.withAuth(s.handleGetDeployment, permissions.DeployRead))
	mux.HandleFunc("POST /v1/deployments/{id}/stop", s.withAuth(s.handleDeploymentStop, permissions.DeployWrite))
	mux.HandleFunc("POST /v1/deployments/{id}/restart", s.withAuth(s.handleDeploymentRestart, permissions.DeployWrite))
	mux.HandleFunc("POST /v1/deployments/{id}/rollback", s.withAuth(s.handleDeploymentRollback, permissions.DeployWrite))
	mux.HandleFunc("GET /v1/deployments/{id}/logs", s.withAuth(s.handleDeploymentLogs, permissions.DeployRead))

	mux.HandleFunc("GET /v1/environments", s.withAuth(s.handleListEnvironments, permissions.DeployRead))
	mux.HandleFunc("POST /v1/environments", s.withAuth(s.handleCreateEnvironment, permissions.DeployWrite))
	mux.HandleFunc("GET /v1/environments/{id}", s.withAuth(s.handleGetEnvironment, permissions.DeployRead))
	mux.HandleFunc("PUT /v1/environments/{id}", s.withAuth(s.handleUpdateEnvironment, permissions.DeployWrite))

	mux.HandleFunc("GET /v1/secrets", s.withAuth(s.handleListSecrets, permissions.SecretsRead))
	mux.HandleFunc("POST /v1/secrets", s.withAuth(s.handleCreateSecret, permissions.SecretsWrite))
	mux.HandleFunc("GET /v1/secrets/{id}", s.withAuth(s.handleGetSecret, permissions.SecretsRead))
	mux.HandleFunc("PUT /v1/secrets/{id}", s.withAuth(s.handleRotateSecret, permissions.SecretsWrite))

	mux.HandleFunc("GET /v1/sources", s.withAuth(s.handleListSources, permissions.SourceRead))
	mux.HandleFunc("POST /v1/sources", s.withAuth(s.handleCreateSource, permissions.SourceWrite))
	mux.HandleFunc("GET /v1/sources/{id}", s.withAuth(s.handleGetSource, permissions.SourceRead))

	mux.HandleFunc("GET /v1/builds", s.withAuth(s.handleListBuilds, permissions.BuildRead))
	mux.HandleFunc("POST /v1/builds", s.withAuth(s.handleCreateBuild, permissions.BuildWrite))
	mux.HandleFunc("GET /v1/builds/{id}", s.withAuth(s.handleGetBuild, permissions.BuildRead))
	mux.HandleFunc("GET /v1/builds/{id}/logs", s.withAuth(s.handleBuildLogs, permissions.BuildRead))

	mux.HandleFunc("GET /v1/releases", s.withAuth(s.handleListReleases, permissions.ReleaseRead))
	mux.HandleFunc("POST /v1/releases", s.withAuth(s.handleCreateRelease, permissions.ReleaseWrite))
	mux.HandleFunc("GET /v1/releases/{id}", s.withAuth(s.handleGetRelease, permissions.ReleaseRead))
	mux.HandleFunc("POST /v1/releases/{id}/deploy", s.withAuth(s.handleDeployRelease, permissions.ReleaseWrite))
	mux.HandleFunc("POST /v1/releases/{id}/rollback", s.withAuth(s.handleRollbackRelease, permissions.ReleaseActivate))
	mux.HandleFunc("GET /v1/releases/{id}/logs", s.withAuth(s.handleReleaseLogs, permissions.ReleaseRead))

	mux.HandleFunc("GET /v1/logs", s.withAuth(s.handleListLogs, permissions.LogsRead))
	mux.HandleFunc("POST /v1/logs", s.withAuth(s.handleIngestLog, permissions.LogsWrite))
	mux.HandleFunc("GET /v1/logs/follow", s.withAuth(s.handleLogsFollow, permissions.LogsRead))

	mux.HandleFunc("GET /v1/ops/context", s.withAuthAny(s.handleOpsContext, opsContextScopes()...))
	mux.HandleFunc("GET /v1/system/update", s.withAuth(s.handleUpdateStatus, permissions.AccountAdmin))
	mux.HandleFunc("POST /v1/system/update/control-plane", s.withAuth(s.handleUpdateControlPlane, permissions.AccountAdmin))
	mux.HandleFunc("POST /v1/system/update/devices/{id}", s.withAuth(s.handleUpdateDevice, permissions.AccountAdmin))

	mux.HandleFunc("GET /v1/workflows", s.withAuthAny(s.handleListWorkflows, workflows.RunScopes()...))
	mux.HandleFunc("POST /v1/workflows/run", s.withAuthAny(s.handleRunWorkflow, workflows.RunScopes()...))
	mux.HandleFunc("GET /v1/workflows/{id}/steps", s.withAuthAny(s.handleWorkflowSteps, workflows.RunScopes()...))
	mux.HandleFunc("GET /v1/workflows/{id}", s.withAuthAny(s.handleGetWorkflow, workflows.RunScopes()...))

	mux.HandleFunc("GET /v1/plans", s.withBearer(s.handleListPlans))
	mux.HandleFunc("POST /v1/plans", s.withBearer(s.handleCreatePlan))
	mux.HandleFunc("POST /v1/plans/{id}/approve", s.withBearer(s.handleApprovePlan))
	mux.HandleFunc("POST /v1/plans/{id}/execute", s.withBearer(s.handleExecutePlan))
	mux.HandleFunc("POST /v1/plans/{id}/cancel", s.withBearer(s.handleCancelPlan))
	mux.HandleFunc("GET /v1/plans/{id}", s.withBearer(s.handleGetPlan))

	mux.HandleFunc("GET /v1/ai/sessions", s.withAuth(s.handleListAISessions, permissions.CredentialsRW))
	mux.HandleFunc("POST /v1/ai/sessions", s.withAuth(s.handleCreateAISession, permissions.CredentialsRW))
	mux.HandleFunc("GET /v1/ai/sessions/current", s.withBearer(s.handleCurrentAISession))
	mux.HandleFunc("GET /v1/ai/sessions/{id}", s.withAuth(s.handleGetAISession, permissions.CredentialsRW))
	mux.HandleFunc("DELETE /v1/ai/sessions/{id}", s.withAuth(s.handleRevokeAISession, permissions.CredentialsRW))

	mux.HandleFunc("GET /v1/compute/devices", s.withAuth(s.handleListCompute, permissions.ComputeRead))
	mux.HandleFunc("GET /v1/compute/devices/{device_id}", s.withAuth(s.handleGetCompute, permissions.ComputeRead))
	mux.HandleFunc("PUT /v1/compute/devices/{device_id}/labels", s.withAuth(s.handlePutComputeLabels, permissions.ComputeWrite))

	mux.HandleFunc("GET /v1/compute/jobs", s.withAuth(s.handleListJobs, permissions.ComputeRead))
	mux.HandleFunc("POST /v1/compute/jobs", s.withAuth(s.handleCreateJob, permissions.ComputeWrite))
	mux.HandleFunc("GET /v1/compute/jobs/{id}", s.withAuth(s.handleGetJob, permissions.ComputeRead))
	mux.HandleFunc("GET /v1/compute/jobs/{id}/artifacts", s.withAuth(s.handleJobArtifacts, permissions.ComputeRead))
	mux.HandleFunc("POST /v1/compute/jobs/{id}/cancel", s.withAuth(s.handleCancelJob, permissions.ComputeWrite))
	mux.HandleFunc("GET /v1/compute/jobs/{id}/logs", s.withAuth(s.handleJobLogs, permissions.ComputeRead))

	mux.HandleFunc("POST /v1/agent/register", s.handleAgentRegister)
	mux.HandleFunc("GET /v1/agent/connect", s.Hub.HandleConnect)

	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "product": "Node"})
	})
	mux.HandleFunc("GET /readyz", s.handleReadyz)
	mux.HandleFunc("GET /metrics", s.handleMetrics)
	mux.HandleFunc("POST /v1/ops/backup", s.withAuth(s.handleBackup, permissions.AccountAdmin))

	h := s.WrapEdge(s.cors(mux))
	if s.Gate != nil {
		h = hardening.Middleware(s.Gate, s.Metrics)(h)
	}
	return h
}

// WrapEdge sends public Hostnames to the origin service via the agent tunnel.
func (s *Server) WrapEdge(next http.Handler) http.Handler {
	if s.Edge == nil {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if rt, err := s.Edge.LookupHost(r.Host); err == nil && rt.TLSMode != protocol.TLSModeOriginTLS {
			s.Edge.ServeHTTP(w, r)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) cors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := s.Cfg.CORSOrigin
		if origin != "" {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Credentials", "true")
			w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, X-Knot-Trace, X-Knot-MCP-Client")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
		}
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) withAuth(next http.HandlerFunc, scope string) http.HandlerFunc {
	return s.withAuthAny(next, scope)
}

func (s *Server) withAuthAny(next http.HandlerFunc, scopes ...string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		raw := bearerToken(r)
		id, err := s.Auth.ResolveBearer(r.Context(), raw)
		if err != nil {
			writeAuthErr(w, err)
			return
		}
		if id.Kind == auth.KindDevice {
			apierrors.WriteCode(w, http.StatusForbidden, apierrors.CodeForbidden, "device tokens cannot access this API")
			return
		}
		trace := strings.TrimSpace(r.Header.Get("X-Knot-Trace"))
		if len(trace) < 6 || len(trace) > 64 {
			trace = oplogs.NewTraceID()
		}
		ctx := context.WithValue(r.Context(), identityKey, id)
		ctx = oplogs.WithTrace(ctx, trace)
		ctx = audit.BindIdentity(ctx, id, r.Header.Get("X-Knot-MCP-Client"))
		ok := false
		for _, scope := range scopes {
			if id.Has(scope) {
				ok = true
				break
			}
		}
		if !ok {
			need := scopes[0]
			s.Audit.Log(ctx, id.UserID, id.Actor, "auth.denied", need, r.URL.Path, "DENIED")
			apierrors.WriteCode(w, http.StatusForbidden, apierrors.CodeForbidden, "missing scope: "+need)
			return
		}
		w.Header().Set("X-Knot-Trace", trace)
		next(w, r.WithContext(ctx))
	}
}

func (s *Server) withBearer(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		raw := bearerToken(r)
		id, err := s.Auth.ResolveBearer(r.Context(), raw)
		if err != nil {
			writeAuthErr(w, err)
			return
		}
		if id.Kind == auth.KindDevice {
			apierrors.WriteCode(w, http.StatusForbidden, apierrors.CodeForbidden, "device tokens cannot access this API")
			return
		}
		trace := strings.TrimSpace(r.Header.Get("X-Knot-Trace"))
		if len(trace) < 6 || len(trace) > 64 {
			trace = oplogs.NewTraceID()
		}
		ctx := context.WithValue(r.Context(), identityKey, id)
		ctx = oplogs.WithTrace(ctx, trace)
		ctx = audit.BindIdentity(ctx, id, r.Header.Get("X-Knot-MCP-Client"))
		w.Header().Set("X-Knot-Trace", trace)
		next(w, r.WithContext(ctx))
	}
}

func writeAuthErr(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, auth.ErrExpired):
		apierrors.WriteCode(w, http.StatusUnauthorized, apierrors.CodeTokenExpired, "token expired")
	case errors.Is(err, auth.ErrRevoked):
		apierrors.WriteCode(w, http.StatusUnauthorized, apierrors.CodeTokenRevoked, "token revoked")
	default:
		apierrors.WriteCode(w, http.StatusUnauthorized, apierrors.CodeUnauthorized, "unauthorized")
	}
}

func bearerToken(r *http.Request) string {
	h := r.Header.Get("Authorization")
	if len(h) > 7 && strings.EqualFold(h[:7], "Bearer ") {
		return strings.TrimSpace(h[7:])
	}
	if c, err := r.Cookie("knot_session"); err == nil {
		return c.Value
	}
	return ""
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		apierrors.Write(w, apierrors.Validation("invalid json"))
		return
	}
	pair, user, err := s.Auth.Login(r.Context(), body.Email, body.Password)
	if err != nil {
		if errors.Is(err, auth.ErrLocked) {
			s.Audit.Log(r.Context(), "", body.Email, "auth.login", "", "locked", "DENIED")
			apierrors.WriteCode(w, http.StatusTooManyRequests, hardening.CodeLocked, "too many failed logins")
			return
		}
		if errors.Is(err, auth.ErrInvalidCredentials) {
			s.Audit.Log(r.Context(), "", body.Email, "auth.login", "", "", "FAILURE")
			apierrors.Write(w, apierrors.InvalidCredentials("invalid email or password"))
			return
		}
		apierrors.Write(w, apierrors.Internal("login failed"))
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     "knot_session",
		Value:    pair.AccessToken,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Expires:  time.Now().Add(s.Cfg.AccessTokenTTL),
	})
	s.Audit.Log(r.Context(), user.ID, user.Email, "auth.login", "", "", "SUCCESS")
	writeJSON(w, http.StatusOK, map[string]any{
		"access_token":  pair.AccessToken,
		"refresh_token": pair.RefreshToken,
		"expires_in":    pair.ExpiresIn,
		"token":         pair.AccessToken, // backward compatible
		"user":          map[string]string{"id": user.ID, "email": user.Email},
	})
}

func (s *Server) handleRefresh(w http.ResponseWriter, r *http.Request) {
	var body struct {
		RefreshToken string `json:"refresh_token"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.RefreshToken == "" {
		apierrors.Write(w, apierrors.Validation("refresh_token required"))
		return
	}
	pair, err := s.Auth.Refresh(r.Context(), body.RefreshToken)
	if err != nil {
		s.Audit.Log(r.Context(), "", "refresh", "auth.refresh", "", "", "FAILURE")
		writeAuthErr(w, err)
		return
	}
	s.Audit.Log(r.Context(), "", "refresh", "auth.refresh", "", "", "SUCCESS")
	writeJSON(w, http.StatusOK, map[string]any{
		"access_token":  pair.AccessToken,
		"refresh_token": pair.RefreshToken,
		"expires_in":    pair.ExpiresIn,
		"token":         pair.AccessToken,
	})
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	var body struct {
		RefreshToken string `json:"refresh_token"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	raw := bearerToken(r)
	_ = s.Auth.Logout(r.Context(), raw, body.RefreshToken)
	http.SetCookie(w, &http.Cookie{Name: "knot_session", Value: "", Path: "/", MaxAge: -1})
	id := IdentityFrom(r.Context())
	if id != nil {
		s.Audit.Log(r.Context(), id.UserID, id.Actor, "auth.logout", "", "", "SUCCESS")
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleLogoutAll(w http.ResponseWriter, r *http.Request) {
	id := IdentityFrom(r.Context())
	if err := s.Auth.LogoutAll(r.Context(), id.UserID); err != nil {
		apierrors.Write(w, apierrors.Internal("logout-all failed"))
		return
	}
	s.Audit.Log(r.Context(), id.UserID, id.Actor, "auth.logout_all", "", "", "SUCCESS")
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	id := IdentityFrom(r.Context())
	out := map[string]any{
		"kind":    id.Kind,
		"user_id": id.UserID,
		"email":   id.Email,
		"actor":   id.Actor,
		"scopes":  id.Scopes,
	}
	if id.Kind == auth.KindAI {
		out["parent"] = id.ParentEmail
		if id.ExpiresAt != nil {
			out["expires_at"] = id.ExpiresAt.UTC()
		}
		if !id.CreatedAt.IsZero() {
			out["created_at"] = id.CreatedAt.UTC()
		}
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleListDevices(w http.ResponseWriter, r *http.Request) {
	id := IdentityFrom(r.Context())
	list, err := s.Devices.List(r.Context(), id.UserID)
	if err != nil {
		apierrors.Write(w, apierrors.Internal("failed to list devices"))
		return
	}
	s.Audit.Log(r.Context(), id.UserID, id.Actor, "devices.list", "", "", "SUCCESS")
	out := make([]map[string]any, 0, len(list))
	for _, d := range list {
		out = append(out, deviceJSON(d))
	}
	writeJSON(w, http.StatusOK, map[string]any{"devices": out})
}

func (s *Server) handleGetDevice(w http.ResponseWriter, r *http.Request) {
	id := IdentityFrom(r.Context())
	devID := r.PathValue("id")
	d, err := s.Devices.Get(r.Context(), id.UserID, devID)
	if err != nil {
		if store.IsNotFound(err) {
			apierrors.Write(w, apierrors.NotFound("device not found"))
			return
		}
		apierrors.Write(w, apierrors.Internal("failed to get device"))
		return
	}
	s.Audit.Log(r.Context(), id.UserID, id.Actor, "devices.get", devID, "", "SUCCESS")
	writeJSON(w, http.StatusOK, deviceJSON(*d))
}

func (s *Server) handleRevokeDevice(w http.ResponseWriter, r *http.Request) {
	id := IdentityFrom(r.Context())
	devID := r.PathValue("id")
	if err := s.Devices.Revoke(r.Context(), id.UserID, devID); err != nil {
		if store.IsNotFound(err) {
			apierrors.Write(w, apierrors.NotFound("device not found"))
			return
		}
		apierrors.Write(w, apierrors.Internal("revoke failed"))
		return
	}
	s.Audit.Log(r.Context(), id.UserID, id.Actor, "devices.revoke", devID, "", "SUCCESS")
	writeJSON(w, http.StatusOK, map[string]string{"status": "revoked"})
}

func (s *Server) handleCreateRegToken(w http.ResponseWriter, r *http.Request) {
	id := IdentityFrom(r.Context())
	var body struct {
		NameHint string `json:"name_hint"`
		TTLHours int    `json:"ttl_hours"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	ttl := 24 * time.Hour
	if body.TTLHours > 0 {
		ttl = time.Duration(body.TTLHours) * time.Hour
	}
	raw, tok, err := s.Auth.CreateRegistrationToken(r.Context(), id.UserID, body.NameHint, ttl)
	if err != nil {
		apierrors.Write(w, apierrors.Internal("failed to create token"))
		return
	}
	s.Audit.Log(r.Context(), id.UserID, id.Actor, "devices.registration_token.create", tok.ID, body.NameHint, "SUCCESS")
	writeJSON(w, http.StatusCreated, map[string]any{
		"id":         tok.ID,
		"token":      raw,
		"name_hint":  tok.NameHint,
		"expires_at": tok.ExpiresAt,
		"prefix":     tok.TokenPrefix,
	})
}

func (s *Server) handleListCredentials(w http.ResponseWriter, r *http.Request) {
	id := IdentityFrom(r.Context())
	list, err := s.Store.ListCredentials(r.Context(), id.UserID)
	if err != nil {
		apierrors.Write(w, apierrors.Internal("failed to list credentials"))
		return
	}
	out := make([]map[string]any, 0, len(list))
	for _, c := range list {
		out = append(out, map[string]any{
			"id":           c.ID,
			"name":         c.Name,
			"token_prefix": c.TokenPrefix,
			"scopes":       c.Scopes,
			"expires_at":   c.ExpiresAt,
			"revoked_at":   c.RevokedAt,
			"created_at":   c.CreatedAt,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"credentials": out})
}

func (s *Server) handleCreateCredential(w http.ResponseWriter, r *http.Request) {
	id := IdentityFrom(r.Context())
	var body struct {
		Name    string   `json:"name"`
		Scopes  []string `json:"scopes"`
		TTLDays int      `json:"ttl_days"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Name == "" {
		apierrors.Write(w, apierrors.Validation("name required"))
		return
	}
	if len(body.Scopes) == 0 {
		body.Scopes = []string{permissions.DevicesRead}
	}
	var exp *time.Time
	if body.TTLDays > 0 {
		t := time.Now().UTC().Add(time.Duration(body.TTLDays) * 24 * time.Hour)
		exp = &t
	}
	raw, cred, err := s.Auth.CreateAPICredential(r.Context(), id.UserID, body.Name, body.Scopes, exp)
	if err != nil {
		apierrors.Write(w, apierrors.Internal("failed to create credential"))
		return
	}
	s.Audit.Log(r.Context(), id.UserID, id.Actor, "credentials.create", cred.ID, body.Name, "SUCCESS")
	writeJSON(w, http.StatusCreated, map[string]any{
		"id":           cred.ID,
		"name":         cred.Name,
		"token":        raw,
		"token_prefix": cred.TokenPrefix,
		"scopes":       cred.Scopes,
		"expires_at":   cred.ExpiresAt,
		"created_at":   cred.CreatedAt,
	})
}

func (s *Server) handleRevokeCredential(w http.ResponseWriter, r *http.Request) {
	id := IdentityFrom(r.Context())
	cid := r.PathValue("id")
	if err := s.Store.RevokeCredential(r.Context(), id.UserID, cid); err != nil {
		if store.IsNotFound(err) {
			apierrors.Write(w, apierrors.NotFound("credential not found"))
			return
		}
		apierrors.Write(w, apierrors.Internal("revoke failed"))
		return
	}
	s.Audit.Log(r.Context(), id.UserID, id.Actor, "credentials.revoke", cid, "", "SUCCESS")
	writeJSON(w, http.StatusOK, map[string]string{"status": "revoked"})
}

func (s *Server) handleRotateCredential(w http.ResponseWriter, r *http.Request) {
	id := IdentityFrom(r.Context())
	cid := r.PathValue("id")
	raw, err := s.Auth.RotateAPICredential(r.Context(), id.UserID, cid)
	if err != nil {
		if store.IsNotFound(err) {
			apierrors.Write(w, apierrors.NotFound("credential not found"))
			return
		}
		apierrors.Write(w, apierrors.Internal("rotate failed"))
		return
	}
	s.Audit.Log(r.Context(), id.UserID, id.Actor, "credentials.rotate", cid, "", "SUCCESS")
	writeJSON(w, http.StatusOK, map[string]any{
		"id":    cid,
		"token": raw,
	})
}

func (s *Server) handleActivity(w http.ResponseWriter, r *http.Request) {
	id := IdentityFrom(r.Context())
	list, err := s.Store.ListAudit(r.Context(), id.UserID, 100)
	if err != nil {
		apierrors.Write(w, apierrors.Internal("failed to list activity"))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"events": list})
}

func (s *Server) handleOverview(w http.ResponseWriter, r *http.Request) {
	id := IdentityFrom(r.Context())
	list, err := s.Devices.List(r.Context(), id.UserID)
	if err != nil {
		apierrors.Write(w, apierrors.Internal("failed"))
		return
	}
	online, offline := 0, 0
	for _, d := range list {
		if d.Online {
			online++
		} else {
			offline++
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"devices_total":   len(list),
		"devices_online":  online,
		"devices_offline": offline,
	})
}

func (s *Server) handleAgentRegister(w http.ResponseWriter, r *http.Request) {
	var req protocol.RegisterRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apierrors.Write(w, apierrors.Validation("invalid json"))
		return
	}
	resp, userID, err := s.Devices.Register(r.Context(), req)
	if err != nil {
		switch {
		case errors.Is(err, devices.ErrInvalidRegToken):
			apierrors.WriteCode(w, http.StatusUnauthorized, apierrors.CodeUnauthorized, err.Error())
		case errors.Is(err, devices.ErrRegTokenUsed), errors.Is(err, devices.ErrRegTokenExpired):
			apierrors.WriteCode(w, http.StatusGone, apierrors.CodeConflict, err.Error())
		default:
			apierrors.Write(w, apierrors.Validation(err.Error()))
		}
		return
	}
	s.Audit.Log(r.Context(), userID, "agent", "devices.register", resp.DeviceID, resp.Name, "SUCCESS")
	writeJSON(w, http.StatusCreated, resp)
}

func (s *Server) handleCreateTransfer(w http.ResponseWriter, r *http.Request) {
	if s.Transfers == nil {
		apierrors.Write(w, apierrors.Internal("transfers unavailable"))
		return
	}
	id := IdentityFrom(r.Context())
	var body struct {
		FromDeviceID string `json:"from_device_id"`
		ToDeviceID   string `json:"to_device_id"`
		Filename     string `json:"filename"`
		SourcePath   string `json:"source_path"`
		Size         int64  `json:"size"`
		SHA256       string `json:"sha256"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		apierrors.Write(w, apierrors.Validation("invalid json"))
		return
	}
	t, err := s.Transfers.Create(r.Context(), transfers.CreateRequest{
		UserID: id.UserID, FromDeviceID: body.FromDeviceID, ToDeviceID: body.ToDeviceID,
		Filename: body.Filename, SourcePath: body.SourcePath, Size: body.Size, SHA256: body.SHA256,
	})
	if err != nil {
		s.Audit.Log(r.Context(), id.UserID, id.Actor, "transfers.create", "", err.Error(), "FAILURE")
		switch {
		case errors.Is(err, transfers.ErrTooLarge):
			apierrors.Write(w, apierrors.Validation(err.Error()))
		case errors.Is(err, transfers.ErrDeviceOffline):
			apierrors.WriteCode(w, http.StatusConflict, apierrors.CodeConflict, err.Error())
		default:
			apierrors.Write(w, apierrors.Validation(err.Error()))
		}
		return
	}
	s.Audit.Log(r.Context(), id.UserID, id.Actor, "transfers.create", t.ID, t.Filename, "SUCCESS")
	writeJSON(w, http.StatusCreated, transferJSON(t))
}

func (s *Server) handleGetTransfer(w http.ResponseWriter, r *http.Request) {
	id := IdentityFrom(r.Context())
	t, err := s.Transfers.Get(r.Context(), id.UserID, r.PathValue("id"))
	if err != nil {
		if errors.Is(err, transfers.ErrNotFound) {
			apierrors.Write(w, apierrors.NotFound("transfer not found"))
			return
		}
		apierrors.Write(w, apierrors.Internal("failed"))
		return
	}
	writeJSON(w, http.StatusOK, transferJSON(t))
}

func (s *Server) handleListTransfers(w http.ResponseWriter, r *http.Request) {
	id := IdentityFrom(r.Context())
	list, err := s.Transfers.List(r.Context(), id.UserID)
	if err != nil {
		apierrors.Write(w, apierrors.Internal("failed"))
		return
	}
	out := make([]map[string]any, 0, len(list))
	for i := range list {
		out = append(out, transferJSON(&list[i]))
	}
	writeJSON(w, http.StatusOK, map[string]any{"transfers": out})
}

func (s *Server) handleAbortTransfer(w http.ResponseWriter, r *http.Request) {
	id := IdentityFrom(r.Context())
	tid := r.PathValue("id")
	if err := s.Transfers.Abort(r.Context(), id.UserID, tid, "aborted by client"); err != nil {
		if errors.Is(err, transfers.ErrNotFound) {
			apierrors.Write(w, apierrors.NotFound("transfer not found"))
			return
		}
		apierrors.Write(w, apierrors.Internal("abort failed"))
		return
	}
	s.Audit.Log(r.Context(), id.UserID, id.Actor, "transfers.abort", tid, "", "SUCCESS")
	writeJSON(w, http.StatusOK, map[string]string{"status": "aborted"})
}

func transferJSON(t *store.Transfer) map[string]any {
	return map[string]any{
		"id":             t.ID,
		"from_device_id": t.FromDeviceID,
		"to_device_id":   t.ToDeviceID,
		"filename":       t.Filename,
		"source_path":    t.SourcePath,
		"size":           t.Size,
		"sha256":         t.SHA256,
		"status":         t.Status,
		"error":          t.Error,
		"path":           t.TransportPath,
		"file_id":        t.FileID,
		"resume_offset":  t.ResumeOffset,
		"bytes_received": t.ResumeOffset, // alias for client progress UX
		"created_at":     t.CreatedAt,
		"updated_at":     t.UpdatedAt,
		"completed_at":   t.CompletedAt,
	}
}

func (s *Server) handleStorageList(w http.ResponseWriter, r *http.Request) {
	if s.Storage == nil {
		apierrors.Write(w, apierrors.Internal("storage unavailable"))
		return
	}
	id := IdentityFrom(r.Context())
	deviceID := r.URL.Query().Get("device_id")
	path := r.URL.Query().Get("path")
	ents, err := s.Storage.List(r.Context(), id.UserID, deviceID, path)
	if err != nil {
		writeStorageErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"entries": ents})
}

func (s *Server) handleStorageStat(w http.ResponseWriter, r *http.Request) {
	if s.Storage == nil {
		apierrors.Write(w, apierrors.Internal("storage unavailable"))
		return
	}
	id := IdentityFrom(r.Context())
	st, err := s.Storage.Stat(r.Context(), id.UserID, r.URL.Query().Get("device_id"), r.URL.Query().Get("path"))
	if err != nil {
		writeStorageErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, st)
}

func (s *Server) handleStorageRead(w http.ResponseWriter, r *http.Request) {
	if s.Storage == nil {
		apierrors.Write(w, apierrors.Internal("storage unavailable"))
		return
	}
	id := IdentityFrom(r.Context())
	t, err := s.Storage.Read(r.Context(), storage.ReadRequest{
		UserID:     id.UserID,
		DeviceID:   r.URL.Query().Get("device_id"),
		Path:       r.URL.Query().Get("path"),
		ToDeviceID: r.URL.Query().Get("to_device_id"),
	})
	if err != nil {
		s.Audit.Log(r.Context(), id.UserID, id.Actor, "storage.read", "", err.Error(), "FAILURE")
		writeStorageErr(w, err)
		return
	}
	s.Audit.Log(r.Context(), id.UserID, id.Actor, "storage.read", t.ID, r.URL.Query().Get("path"), "SUCCESS")
	writeJSON(w, http.StatusAccepted, transferJSON(t))
}

func (s *Server) handleStorageUpload(w http.ResponseWriter, r *http.Request) {
	if s.Storage == nil {
		apierrors.Write(w, apierrors.Internal("storage unavailable"))
		return
	}
	id := IdentityFrom(r.Context())
	var body struct {
		DeviceID     string `json:"device_id"`
		Path         string `json:"path"`
		FromDeviceID string `json:"from_device_id"`
		SourcePath   string `json:"source_path"`
		Size         int64  `json:"size"`
		SHA256       string `json:"sha256"`
		Resume       bool   `json:"resume"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		apierrors.Write(w, apierrors.Validation("invalid json"))
		return
	}
	out, err := s.Storage.Upload(r.Context(), storage.UploadRequest{
		UserID: id.UserID, CredID: id.CredID, DeviceID: body.DeviceID, Path: body.Path,
		FromDeviceID: body.FromDeviceID, SourcePath: body.SourcePath,
		Size: body.Size, SHA256: body.SHA256, Resume: body.Resume,
	})
	if err != nil {
		s.Audit.Log(r.Context(), id.UserID, id.Actor, "storage.upload", "", err.Error(), "FAILURE")
		writeStorageErr(w, err)
		return
	}
	s.Audit.Log(r.Context(), id.UserID, id.Actor, "storage.upload", out.Transfer.ID, body.Path, "SUCCESS")
	resp := transferJSON(out.Transfer)
	if out.File != nil {
		resp["file_id"] = out.File.ID
		resp["file_status"] = out.File.Status
		resp["bytes_received"] = out.File.BytesReceived
	}
	writeJSON(w, http.StatusAccepted, resp)
}

func (s *Server) handleStorageGetFile(w http.ResponseWriter, r *http.Request) {
	if s.Storage == nil {
		apierrors.Write(w, apierrors.Internal("storage unavailable"))
		return
	}
	id := IdentityFrom(r.Context())
	f, err := s.Storage.GetFile(r.Context(), id.UserID, r.PathValue("id"))
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			apierrors.Write(w, apierrors.NotFound("file not found"))
			return
		}
		apierrors.Write(w, apierrors.Internal("failed"))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"id": f.ID, "device_id": f.DeviceID, "path": f.Path,
		"size": f.Size, "sha256": f.SHA256, "status": f.Status,
		"transfer_id": f.TransferID, "bytes_received": f.BytesReceived,
		"created_at": f.CreatedAt, "updated_at": f.UpdatedAt,
	})
}

func (s *Server) handleStorageMkdir(w http.ResponseWriter, r *http.Request) {
	if s.Storage == nil {
		apierrors.Write(w, apierrors.Internal("storage unavailable"))
		return
	}
	id := IdentityFrom(r.Context())
	var body struct {
		DeviceID string `json:"device_id"`
		Path     string `json:"path"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		apierrors.Write(w, apierrors.Validation("invalid json"))
		return
	}
	st, err := s.Storage.Mkdir(r.Context(), id.UserID, body.DeviceID, body.Path)
	if err != nil {
		s.Audit.Log(r.Context(), id.UserID, id.Actor, "storage.mkdir", "", err.Error(), "FAILURE")
		writeStorageErr(w, err)
		return
	}
	s.Audit.Log(r.Context(), id.UserID, id.Actor, "storage.mkdir", body.Path, body.DeviceID, "SUCCESS")
	writeJSON(w, http.StatusCreated, st)
}

func (s *Server) handleStorageDelete(w http.ResponseWriter, r *http.Request) {
	if s.Storage == nil {
		apierrors.Write(w, apierrors.Internal("storage unavailable"))
		return
	}
	id := IdentityFrom(r.Context())
	deviceID := r.URL.Query().Get("device_id")
	path := r.URL.Query().Get("path")
	if err := s.Storage.Delete(r.Context(), id.UserID, deviceID, path); err != nil {
		s.Audit.Log(r.Context(), id.UserID, id.Actor, "storage.delete", path, err.Error(), "FAILURE")
		writeStorageErr(w, err)
		return
	}
	s.Audit.Log(r.Context(), id.UserID, id.Actor, "storage.delete", path, deviceID, "SUCCESS")
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func (s *Server) handleStorageMove(w http.ResponseWriter, r *http.Request) {
	if s.Storage == nil {
		apierrors.Write(w, apierrors.Internal("storage unavailable"))
		return
	}
	id := IdentityFrom(r.Context())
	var body struct {
		DeviceID string `json:"device_id"`
		FromPath string `json:"from_path"`
		ToPath   string `json:"to_path"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		apierrors.Write(w, apierrors.Validation("invalid json"))
		return
	}
	st, err := s.Storage.Move(r.Context(), id.UserID, body.DeviceID, body.FromPath, body.ToPath)
	if err != nil {
		writeStorageErr(w, err)
		return
	}
	s.Audit.Log(r.Context(), id.UserID, id.Actor, "storage.move", body.ToPath, body.FromPath, "SUCCESS")
	writeJSON(w, http.StatusOK, st)
}

func (s *Server) handleStorageCopy(w http.ResponseWriter, r *http.Request) {
	if s.Storage == nil {
		apierrors.Write(w, apierrors.Internal("storage unavailable"))
		return
	}
	id := IdentityFrom(r.Context())
	var body struct {
		DeviceID string `json:"device_id"`
		FromPath string `json:"from_path"`
		ToPath   string `json:"to_path"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		apierrors.Write(w, apierrors.Validation("invalid json"))
		return
	}
	st, err := s.Storage.Copy(r.Context(), id.UserID, body.DeviceID, body.FromPath, body.ToPath)
	if err != nil {
		writeStorageErr(w, err)
		return
	}
	s.Audit.Log(r.Context(), id.UserID, id.Actor, "storage.copy", body.ToPath, body.FromPath, "SUCCESS")
	writeJSON(w, http.StatusCreated, st)
}

func (s *Server) handleStorageTransfer(w http.ResponseWriter, r *http.Request) {
	if s.Storage == nil {
		apierrors.Write(w, apierrors.Internal("storage unavailable"))
		return
	}
	id := IdentityFrom(r.Context())
	var body struct {
		FromDeviceID string `json:"from_device_id"`
		FromPath     string `json:"from_path"`
		ToDeviceID   string `json:"to_device_id"`
		ToPath       string `json:"to_path"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		apierrors.Write(w, apierrors.Validation("invalid json"))
		return
	}
	if body.FromDeviceID == body.ToDeviceID {
		st, err := s.Storage.Copy(r.Context(), id.UserID, body.FromDeviceID, body.FromPath, body.ToPath)
		if err != nil {
			writeStorageErr(w, err)
			return
		}
		s.Audit.Log(r.Context(), id.UserID, id.Actor, "storage.transfer", body.ToPath, body.FromPath, "SUCCESS")
		writeJSON(w, http.StatusOK, map[string]any{"mode": "copy", "stat": st})
		return
	}
	out, err := s.Storage.TransferBetween(r.Context(), storage.TransferBetweenRequest{
		UserID: id.UserID, CredID: id.CredID,
		FromDeviceID: body.FromDeviceID, FromPath: body.FromPath,
		ToDeviceID: body.ToDeviceID, ToPath: body.ToPath,
	})
	if err != nil {
		s.Audit.Log(r.Context(), id.UserID, id.Actor, "storage.transfer", "", err.Error(), "FAILURE")
		writeStorageErr(w, err)
		return
	}
	s.Audit.Log(r.Context(), id.UserID, id.Actor, "storage.transfer", out.Transfer.ID, body.ToPath, "SUCCESS")
	resp := transferJSON(out.Transfer)
	resp["mode"] = "transfer"
	if out.File != nil {
		resp["file_id"] = out.File.ID
		resp["file_status"] = out.File.Status
		resp["bytes_received"] = out.File.BytesReceived
	}
	writeJSON(w, http.StatusAccepted, resp)
}

func (s *Server) handleStoragePut(w http.ResponseWriter, r *http.Request) {
	if s.Storage == nil {
		apierrors.Write(w, apierrors.Internal("storage unavailable"))
		return
	}
	id := IdentityFrom(r.Context())
	q := r.URL.Query()
	deviceID := q.Get("device_id")
	path := q.Get("path")
	sha := q.Get("sha256")
	overwrite := q.Get("overwrite") == "true" || q.Get("overwrite") == "1"
	conflict := q.Get("conflict") // overwrite | rename | ""
	size := r.ContentLength
	if size <= 0 {
		apierrors.Write(w, apierrors.Validation("Content-Length required"))
		return
	}
	destPath := path
	if conflict == "rename" || (!overwrite && conflict != "overwrite") {
		if conflict == "rename" {
			p, err := s.Storage.ResolveConflictPath(r.Context(), id.UserID, deviceID, path)
			if err != nil {
				writeStorageErr(w, err)
				return
			}
			destPath = p
			overwrite = true
		}
	} else if conflict == "overwrite" {
		overwrite = true
	}
	st, err := s.Storage.Put(r.Context(), storage.PutRequest{
		UserID: id.UserID, CredID: id.CredID, DeviceID: deviceID, Path: destPath,
		Size: size, SHA256: sha, Overwrite: overwrite || conflict == "overwrite", Body: r.Body,
	})
	if err != nil {
		s.Audit.Log(r.Context(), id.UserID, id.Actor, "storage.put", path, err.Error(), "FAILURE")
		writeStorageErr(w, err)
		return
	}
	s.Audit.Log(r.Context(), id.UserID, id.Actor, "storage.put", destPath, deviceID, "SUCCESS")
	writeJSON(w, http.StatusCreated, st)
}

func (s *Server) handleStorageContent(w http.ResponseWriter, r *http.Request) {
	if s.Storage == nil {
		apierrors.Write(w, apierrors.Internal("storage unavailable"))
		return
	}
	id := IdentityFrom(r.Context())
	q := r.URL.Query()
	data, mimeType, st, err := s.Storage.Content(r.Context(), id.UserID, q.Get("device_id"), q.Get("path"), 8<<20)
	if err != nil {
		writeStorageErr(w, err)
		return
	}
	if mimeType == "" {
		mimeType = "application/octet-stream"
	}
	w.Header().Set("Content-Type", mimeType)
	if st != nil && st.Name != "" {
		w.Header().Set("Content-Disposition", fmt.Sprintf("inline; filename=%q", st.Name))
	}
	w.Header().Set("X-Knot-Size", fmt.Sprintf("%d", len(data)))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

func (s *Server) handleStoragePreview(w http.ResponseWriter, r *http.Request) {
	if s.Storage == nil {
		apierrors.Write(w, apierrors.Internal("storage unavailable"))
		return
	}
	id := IdentityFrom(r.Context())
	q := r.URL.Query()
	variant := q.Get("variant")
	if variant == "" {
		variant = "preview"
	}
	maxPixels := 0
	if raw := q.Get("max_pixels"); raw != "" {
		_, _ = fmt.Sscanf(raw, "%d", &maxPixels)
	}
	data, mimeType, st, cacheKey, err := s.Storage.Preview(r.Context(), id.UserID, q.Get("device_id"), q.Get("path"), variant, maxPixels)
	if err != nil {
		writeStorageErr(w, err)
		return
	}
	if mimeType == "" {
		mimeType = "application/octet-stream"
	}
	w.Header().Set("Content-Type", mimeType)
	w.Header().Set("X-Knot-Preview-Variant", variant)
	if cacheKey != "" {
		w.Header().Set("X-Knot-Preview-Cache-Key", cacheKey)
	}
	if st != nil && st.Name != "" {
		w.Header().Set("X-Knot-Preview-Source-Name", st.Name)
		w.Header().Set("X-Knot-Preview-Source-Mime", st.MimeType)
	}
	w.Header().Set("X-Knot-Size", fmt.Sprintf("%d", len(data)))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

func writeStorageErr(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, storage.ErrBadPath):
		apierrors.Write(w, apierrors.Validation(err.Error()))
	case errors.Is(err, storage.ErrNameConflict):
		apierrors.WriteCode(w, http.StatusConflict, "name_conflict", err.Error())
	case errors.Is(err, storage.ErrQuota):
		apierrors.WriteCode(w, http.StatusInsufficientStorage, "quota_exceeded", err.Error())
	case errors.Is(err, storage.ErrDeviceOffline):
		apierrors.WriteCode(w, http.StatusConflict, apierrors.CodeConflict, err.Error())
	case errors.Is(err, transfers.ErrTooLarge), errors.Is(err, transfers.ErrBadPath), errors.Is(err, transfers.ErrDeviceOffline):
		if errors.Is(err, transfers.ErrDeviceOffline) {
			apierrors.WriteCode(w, http.StatusConflict, apierrors.CodeConflict, err.Error())
			return
		}
		apierrors.Write(w, apierrors.Validation(err.Error()))
	case errors.Is(err, storage.ErrTimeout):
		apierrors.WriteCode(w, http.StatusGatewayTimeout, apierrors.CodeInternal, err.Error())
	default:
		apierrors.Write(w, apierrors.Validation(err.Error()))
	}
}

func deviceJSON(d store.Device) map[string]any {
	return map[string]any{
		"id":            d.ID,
		"name":          d.Name,
		"hostname":      d.Hostname,
		"os":            d.OS,
		"arch":          d.Arch,
		"cpus":          d.CPUs,
		"ram_mb":        d.RAMMB,
		"agent_version": d.AgentVersion,
		"online":        d.Online,
		"revoked_at":    d.RevokedAt,
		"last_seen_at":  d.LastSeenAt,
		"created_at":    d.CreatedAt,
	}
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

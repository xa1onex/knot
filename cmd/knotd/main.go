package main

import (
	"context"
	"crypto/tls"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/knot-infra/knot/internal/agentws"
	"github.com/knot-infra/knot/internal/aisessions"
	"github.com/knot-infra/knot/internal/api"
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
	"github.com/knot-infra/knot/internal/storage"
	"github.com/knot-infra/knot/internal/store"
	syncjob "github.com/knot-infra/knot/internal/sync"
	"github.com/knot-infra/knot/internal/selfupdate"
	"github.com/knot-infra/knot/internal/traffic"
	"github.com/knot-infra/knot/internal/transfers"
	"github.com/knot-infra/knot/internal/webui"
	"github.com/knot-infra/knot/internal/workflows"
)

func main() {
	cfg := config.Load()
	if err := cfg.ValidateBind(); err != nil {
		log.Fatalf("config: %v", err)
	}

	st, err := store.Open(cfg.DatabasePath)
	if err != nil {
		log.Fatalf("store: %v", err)
	}
	defer st.Close()

	n, err := st.CountUsers(context.Background())
	if err != nil {
		log.Fatalf("store: %v", err)
	}
	if err := cfg.ValidateBootstrap(n); err != nil {
		log.Fatalf("bootstrap: %v", err)
	}

	authSvc := &auth.Service{
		Store:            st,
		AccessTokenTTL:   cfg.AccessTokenTTL,
		RefreshTokenTTL:  cfg.RefreshTokenTTL,
		DeviceSessionTTL: cfg.DeviceSessionTTL,
	}
	if err := authSvc.EnsureBootstrapAdmin(context.Background(), cfg.BootstrapAdminEmail, cfg.BootstrapAdminPass); err != nil {
		log.Fatalf("bootstrap: %v", err)
	}

	devSvc := &devices.Service{Store: st, Auth: authSvc}
	auditLog := &audit.Logger{Store: st}
	hub := agentws.NewHub(authSvc, devSvc, st, auditLog)
	xferSvc := transfers.New(st, hub, transfers.Options{
		ForceRelay:    cfg.ForceRelay,
		STUNURLs:      cfg.STUNURLs,
		DirectTimeout: cfg.DirectTimeout,
	})
	hub.SetTransfers(xferSvc)
	storageSvc := storage.New(st, hub, xferSvc)
	storageSvc.Quotas = storage.Quotas{
		MaxTotalBytes: cfg.StorageMaxTotal,
		MaxFileBytes:  cfg.StorageMaxFile,
		MaxFiles:      cfg.StorageMaxFiles,
	}
	hub.SetStorage(storageSvc)
	syncSvc := syncjob.New(st, storageSvc)
	hub.SetOffline(syncjob.HubFlush{S: syncSvc})
	filesSvc := files.New(st, storageSvc, hub)
	hub.SetIndexer(filesSvc)
	storageSvc.OnMutate = filesSvc.OnMutate
	svcReg := services.New(st)
	edgeProxy := edge.New(st, hub)
	hub.SetEdge(edgeProxy)
	secKey, err := secrets.LoadOrCreateKey(cfg.DatabasePath, cfg.SecretsKeyFile, cfg.SecretsKey)
	if err != nil {
		log.Fatalf("secrets key: %v", err)
	}
	secretsSvc := secrets.New(st, secKey)
	envSvc := environments.New(st, secretsSvc)
	deploySvc := deploy.New(st, hub, svcReg, edgeProxy)
	deploySvc.Secrets = secretsSvc
	deploySvc.Envs = envSvc
	hub.SetDeploy(deploySvc)
	computeSvc := compute.New(st, cfg.HeartbeatTimeout)
	jobsSvc := jobs.New(st, hub, computeSvc)
	hub.SetJobs(jobsSvc)
	buildsSvc := builds.New(st, hub, secretsSvc)
	hub.SetBuilds(buildsSvc)
	relSvc := releases.New(st, deploySvc, envSvc, secretsSvc)
	relSvc.Builds = buildsSvc
	relSvc.Jobs = jobsSvc
	trafSvc := traffic.New(st)
	relSvc.Traffic = trafSvc
	logsSvc := oplogs.New(st, cfg.LogRetentionDays)
	auditLog.Logs = logsSvc
	hub.Logs = logsSvc
	edgeProxy.Logs = logsSvc
	deploySvc.Ops = logsSvc
	jobsSvc.Ops = logsSvc
	buildsSvc.Ops = logsSvc
	relSvc.Ops = logsSvc
	trafSvc.Logs = logsSvc
	opsSvc := ops.New(st, edgeProxy, trafSvc, logsSvc)
	wfSvc := workflows.New(st, opsSvc, trafSvc, relSvc, buildsSvc, jobsSvc, filesSvc, storageSvc, edgeProxy, logsSvc, auditLog)
	planSvc := plans.New(st, opsSvc, trafSvc, relSvc, buildsSvc, jobsSvc, filesSvc, storageSvc, edgeProxy, logsSvc, auditLog)
	updateSvc := selfupdate.New(st, hub, cfg)
	hub.SetUpdates(updateSvc)
	aiSvc := aisessions.New(st, authSvc)

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()
	hub.StartPresenceSweeper(ctx, cfg.HeartbeatTimeout)
	logsSvc.StartSweeper(ctx)

	gate := hardening.NewGate(20, 300)
	gate.TrustProxy = cfg.TrustProxy
	apiServer := &api.Server{
		Cfg:          cfg,
		Store:        st,
		Auth:         authSvc,
		Devices:      devSvc,
		Audit:        auditLog,
		Hub:          hub,
		Transfers:    xferSvc,
		Storage:      storageSvc,
		Sync:         syncSvc,
		Files:        filesSvc,
		Services:     svcReg,
		Edge:         edgeProxy,
		Environments: envSvc,
		Secrets:      secretsSvc,
		Deploy:       deploySvc,
		Releases:     relSvc,
		Traffic:      trafSvc,
		Builds:       buildsSvc,
		Compute:      computeSvc,
		Jobs:         jobsSvc,
		Logs:         logsSvc,
		Ops:          opsSvc,
		Updates:      updateSvc,
		Workflows:    wfSvc,
		Plans:        planSvc,
		AI:           aiSvc,
		Gate:         gate,
		Metrics:      hardening.NewMetrics(),
		StartedAt:    time.Now().UTC(),
	}

	handler := apiServer.Handler()
	if cfg.StaticDir != "" {
		handler = webui.Wrap(handler, cfg.StaticDir)
	}

	srv := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
	}

	mode := "http"
	if cfg.TLSEnabled() {
		mode = "https"
		reloader, err := hardening.NewCertReloader(cfg.TLSCertFile, cfg.TLSKeyFile)
		if err != nil {
			log.Fatalf("tls: %v", err)
		}
		srv.TLSConfig = &tls.Config{
			MinVersion:     tls.VersionTLS12,
			GetCertificate: reloader.GetCertificate,
		}
		go func() {
			ch := make(chan os.Signal, 1)
			signal.Notify(ch, syscall.SIGHUP)
			for {
				select {
				case <-ctx.Done():
					return
				case <-ch:
					if err := reloader.Reload(); err != nil {
						log.Printf("tls reload failed: %v", err)
						continue
					}
					log.Printf("tls certificates reloaded")
				}
			}
		}()
	}
	log.Printf("knotd (Node Control Plane) listening on %s (%s)", cfg.HTTPAddr, mode)
	if cfg.PublicBaseURL != "" {
		log.Printf("public base URL: %s", cfg.PublicBaseURL)
	}
	if n == 0 {
		log.Printf("bootstrapped admin: %s", cfg.BootstrapAdminEmail)
	}

	go func() {
		var err error
		if cfg.TLSEnabled() {
			err = srv.ListenAndServeTLS("", "")
		} else {
			err = srv.ListenAndServe()
		}
		if err != nil && err != http.ErrServerClosed {
			log.Fatalf("listen: %v", err)
		}
	}()

	if cfg.TLSPassthroughAddr != "" {
		addr, err := edgeProxy.StartPassthrough(ctx, cfg.TLSPassthroughAddr)
		if err != nil {
			log.Fatalf("tls passthrough: %v", err)
		}
		log.Printf("TLS passthrough (origin_tls) listening on %s", addr)
	}

	<-ctx.Done()
	shutdownCtx, c := context.WithTimeout(context.Background(), 10*time.Second)
	defer c()
	_ = srv.Shutdown(shutdownCtx)
}

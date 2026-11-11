// Copyright (C) 2026 YIQISOFT
// SPDX-License-Identifier: Apache-2.0

// Command sphere is the NereidSphere thin suite layer. It hosts the suite
// portal, the OIDC single sign-on flow against the external identity service
// and the unified API gateway that fronts the integrated products. It
// contains no product domain logic.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/NereidAI/NereidSphere/internal/admin"
	"github.com/NereidAI/NereidSphere/internal/apidocs"
	"github.com/NereidAI/NereidSphere/internal/auth"
	"github.com/NereidAI/NereidSphere/internal/config"
	"github.com/NereidAI/NereidSphere/internal/gateway"
	"github.com/NereidAI/NereidSphere/internal/httpx"
	"github.com/NereidAI/NereidSphere/internal/portal"
	"github.com/NereidAI/NereidSphere/internal/valkey"
)

// Version and GitSHA are injected at build time via -ldflags (see Makefile).
// The fallback is the rolling dev channel used by non-tag checkouts.
var (
	Version = "0.0.0-dev"
	GitSHA  = "unknown"
)

// configPath is the global -c flag selecting the YAML config file.
var configPath string

func main() {
	root := &cobra.Command{
		Use:     "sphere",
		Short:   "NereidSphere thin suite layer (portal, SSO, gateway)",
		Version: Version + " (" + GitSHA + ")",
	}
	root.PersistentFlags().StringVarP(&configPath, "config", "c", "configs/dev.yaml", "path to the YAML configuration file")

	root.AddCommand(serveCmd())

	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// serveCmd builds and runs the HTTP server.
func serveCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "serve",
		Short: "Run the portal, SSO and gateway HTTP server",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := config.Load(configPath)
			if err != nil {
				return err
			}
			if secret := os.Getenv("NS_VALKEY_PASSWORD"); secret != "" {
				cfg.Tickets.Valkey.Password = secret
			}
			configureLogging(cfg.IsDev())

			slog.Info("starting NereidSphere", "version", Version, "git_sha", GitSHA,
				"env", cfg.Server.Env, "listen", cfg.Server.Listen)

			return runServer(cmd.Context(), cfg)
		},
	}
}

// runServer is the composition root: it constructs collaborators in
// dependency order, wires routes and runs the listener with graceful
// shutdown.
func runServer(rootCtx context.Context, cfg *config.Config) error {
	ctx, stop := signal.NotifyContext(rootCtx, syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// Select the authentication backend. auth.mode=oidc makes NS a pure
	// relying party of the external identity service (authorization-code +
	// PKCE); discovery is lazy, so a temporarily unreachable issuer never
	// blocks startup. auth.mode=local (the default) uses emergency bcrypt
	// accounts rendered from the deployment Secret.
	var oidcProvider *auth.Provider
	var localAuth *auth.LocalAuthenticator
	if cfg.Auth.Mode == config.AuthModeOIDC {
		oidcProvider = auth.NewProvider(auth.ProviderConfig{
			Issuer:       cfg.OIDC.Issuer,
			ClientID:     cfg.OIDC.ClientID,
			ClientSecret: cfg.OIDC.ClientSecret,
			RedirectURL:  cfg.RedirectURL(),
			Scopes:       cfg.OIDC.Scopes,
		})
		slog.Info("OIDC authentication enabled", "mode", cfg.Auth.Mode, "issuer", cfg.OIDC.Issuer)
	} else {
		localAuth = auth.NewLocalAuthenticator(cfg.Auth.LocalUsers)
		slog.Info("local account authentication enabled", "mode", cfg.Auth.Mode, "accounts", len(cfg.Auth.LocalUsers))
	}

	codec, err := auth.NewCodec(padOrRejectSecret(cfg))
	if err != nil {
		return err
	}

	replayStore := auth.ReplayStore(auth.NewMemoryReplayStore())
	if cfg.Tickets.ReplayStore == config.ReplayStoreValkey {
		valkeyClient, err := valkey.New(cfg.Tickets.Valkey)
		if err != nil {
			return fmt.Errorf("initialize valkey replay store: %w", err)
		}
		defer func() { _ = valkeyClient.Close() }()
		replayStore = valkeyClient
		slog.Info("Valkey ticket replay store enabled", "sentinel", cfg.Tickets.Valkey.Address == "")
	}

	mux := http.NewServeMux()

	authServer := auth.NewServerWithReplay(cfg, codec, oidcProvider, localAuth, replayStore)
	// The local login endpoint gets its own, stricter per-client budget to
	// slow credential stuffing without punishing ordinary portal traffic.
	authServer.WithLoginRateLimit(httpx.RateLimitMiddleware(
		httpx.NewRateLimiter(cfg.Gateway.LoginRequestsPerMinute, cfg.Gateway.LoginBurst)))
	authServer.Routes(mux)

	portalHandler := portal.NewHandler(cfg, authServer.Resolver())
	portalHandler.Routes(mux)

	// Admin console: in oidc mode use the nereidsphere-admin service
	// account against the identity service Admin REST API; in local mode
	// there is no user database to manage.
	var adminClient *admin.Client
	if oidcProvider != nil {
		adminClient, err = admin.NewClient(cfg.OIDC.Issuer, cfg.OIDC.AdminClientID, cfg.OIDC.AdminClientSecret)
		if err != nil {
			return fmt.Errorf("initialize identity admin client: %w", err)
		}
	}
	admin.NewHandler(cfg, authServer.Resolver(), adminClient, oidcProvider).Routes(mux)

	gw := gateway.NewMiddleware(authServer.Resolver())
	gateway.Routes(mux, gw, cfg)

	// Self-contained Swagger UI + embedded OpenAPI contract at /api/docs.
	apidocs.Register(mux)

	// Embedded SPA is the catch-all; more specific patterns above win.
	mux.Handle("/", portal.SPAHandler())

	// Outer-to-inner: panic recovery, tracing, restrictive CORS, browser
	// security headers, the general per-client rate limit, then CSRF
	// double-submit enforcement on browser-driven state changes.
	generalLimiter := httpx.NewRateLimiter(cfg.Gateway.RequestsPerMinute, cfg.Gateway.Burst)
	handler := httpx.RecoverMiddleware(
		httpx.TraceMiddleware(
			httpx.CORSMiddleware(cfg.Gateway.CORSAllowedOrigins)(
				httpx.ApplySecurityHeaders(!cfg.IsDev(),
					httpx.RateLimitMiddleware(generalLimiter)(
						authServer.CSRFMiddleware(mux))))))

	server := &http.Server{
		Addr:              cfg.Server.Listen,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		slog.Info("http server listening", "addr", cfg.Server.Listen)
		if serveErr := server.ListenAndServe(); serveErr != nil && serveErr != http.ErrServerClosed {
			errCh <- serveErr
		}
		close(errCh)
	}()

	select {
	case <-ctx.Done():
		slog.Info("shutdown signal received")
	case serveErr := <-errCh:
		if serveErr != nil {
			return serveErr
		}
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("graceful shutdown: %w", err)
	}
	slog.Info("server stopped cleanly")
	return nil
}

// padOrRejectSecret returns the session secret. In development a short
// secret is zero-padded to 32 bytes so the local flow works without a strong
// configured value; production validation in config already rejects it.
func padOrRejectSecret(cfg *config.Config) string {
	secret := cfg.Session.SecretKey
	if len(secret) >= 32 {
		return secret[:32]
	}
	if cfg.IsDev() {
		padded := make([]byte, 32)
		copy(padded, secret)
		return string(padded)
	}
	return secret
}

// configureLogging selects a human-readable handler in development and JSON
// in production, matching the suite's log/slog convention.
func configureLogging(dev bool) {
	if dev {
		slog.SetDefault(slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug})))
		return
	}
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo})))
}

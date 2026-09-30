package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"truckin-be/internal/config"
	"truckin-be/internal/database"
	"truckin-be/internal/httpapi"
	"truckin-be/internal/integration"
	"truckin-be/internal/movement"
	"truckin-be/internal/operations"
	"truckin-be/internal/unitsync"
)

// @title UnitLog Pool Basirih API
// @version 1.0.0
// @description API for immutable truck IN/OUT movements and pool operations.
// @description Movement scans require `unitlog:app:default:view`, actor ID/name claims, and an Idempotency-Key UUID. Operations require `unitlog:web:default:view`.
// @BasePath /
// @schemes http https
// @securityDefinitions.apikey BearerAuth
// @in header
// @name Authorization
// @description Auth Service JWT using the `Bearer <token>` format.
func main() {
	startupContext, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	applicationConfig, err := config.Load(startupContext)
	if err != nil {
		slog.Error("load configuration", "error", err)
		os.Exit(1)
	}
	if err := applicationConfig.ValidateRuntime(); err != nil {
		slog.Error("validate configuration", "error", err)
		os.Exit(1)
	}

	db, err := database.Open(startupContext, applicationConfig.PostgresDSN)
	if err != nil {
		slog.Error("connect to database", "error", err)
		os.Exit(1)
	}
	defer db.Close()

	integrationClient := integration.NewClient(integration.Config{
		LambungBaseURL: applicationConfig.LambungAPIBaseURL, LambungToken: applicationConfig.LambungServiceToken,
		SAOSBaseURL: applicationConfig.SAOSAPIBaseURL, SAOSToken: applicationConfig.SAOSServiceToken,
		WorkshopBaseURL: applicationConfig.WorkshopAPIBaseURL, WorkshopToken: applicationConfig.WorkshopServiceToken,
		DriverBaseURL: applicationConfig.DriverAPIBaseURL, DriverToken: applicationConfig.DriverServiceToken,
		ConnectTimeout: time.Duration(applicationConfig.HTTPClientConnectTimeoutMS) * time.Millisecond,
		RequestTimeout: time.Duration(applicationConfig.HTTPClientRequestTimeoutMS) * time.Millisecond,
	})
	syncService := unitsync.NewService(db, integrationClient, applicationConfig.UnitSyncPageSize)
	movementService := movement.NewService(db, syncService, integrationClient)
	operationsService := operations.NewService(db)

	server := &http.Server{
		Addr: fmt.Sprintf(":%d", applicationConfig.ServerPort),
		Handler: httpapi.NewRouter(httpapi.Dependencies{
			Database: db, Movement: movementService, Operations: operationsService,
			JWTSecret: []byte(applicationConfig.AuthJWTHS256Secret), PermissionClaim: applicationConfig.AuthPermissionsClaim,
			ActorIDClaim: applicationConfig.AuthActorIDClaim, ActorNameClaim: applicationConfig.AuthActorNameClaim,
		}),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	syncContext, syncCancel := context.WithCancel(context.Background())
	defer syncCancel()
	interval, _ := time.ParseDuration(applicationConfig.UnitSyncInterval)
	go unitsync.RunPeriodically(syncContext, syncService, interval, func(err error) {
		slog.Error("sync Unit Lambung", "error", err)
	})

	go func() {
		slog.Info("server started", "port", applicationConfig.ServerPort)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("serve HTTP", "error", err)
			os.Exit(1)
		}
	}()

	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM)
	<-signals

	shutdownContext, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := server.Shutdown(shutdownContext); err != nil {
		slog.Error("shutdown server", "error", err)
	}
}

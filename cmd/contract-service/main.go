package main

import (
	"fmt"
	"os"

	"github.com/nurpe/snowops-contract/internal/auth"
	"github.com/nurpe/snowops-contract/internal/config"
	"github.com/nurpe/snowops-contract/internal/db"
	httphandler "github.com/nurpe/snowops-contract/internal/http"
	"github.com/nurpe/snowops-contract/internal/http/middleware"
	"github.com/nurpe/snowops-contract/internal/logger"
	"github.com/nurpe/snowops-contract/internal/repository"
	"github.com/nurpe/snowops-contract/internal/service"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to load config: %v\n", err)
		os.Exit(1)
	}

	appLogger := logger.New(cfg.Environment)

	database, err := db.New(cfg, appLogger)
	if err != nil {
		appLogger.Fatal().Err(err).Msg("failed to connect database")
	}

	contractRepo := repository.NewContractRepository(database)

	contractService := service.NewContractService(contractRepo)

	tokenParser := auth.NewParser(cfg.Auth.AccessSecret)

	handler := httphandler.NewHandler(contractService, appLogger)
	authMiddleware := middleware.Auth(tokenParser)
	router := httphandler.NewRouter(handler, authMiddleware, cfg.Environment)

	addr := fmt.Sprintf("%s:%d", cfg.HTTP.Host, cfg.HTTP.Port)
	appLogger.Info().Str("addr", addr).Msg("starting contract service")

	if err := router.Run(addr); err != nil {
		appLogger.Error().Err(err).Msg("failed to start server")
		os.Exit(1)
	}
}


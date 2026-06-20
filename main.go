package main

import (
	"context"
	"cx3/config"
	"cx3/middleware"
	redisrepo "cx3/repository/redis"
	mysqlrepo "cx3/repository/mysql"
	"cx3/router"
	"cx3/utils"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	_ "net/http/pprof"
)

var (
	configPath = flag.String("config", "config/config.yaml", "配置文件路径")
)

func main() {
	flag.Parse()

	cfg, err := config.LoadConfig(*configPath)
	if err != nil {
		log.Fatalf("load config failed: %v", err)
	}

	if err := utils.InitLogger(&cfg.Log); err != nil {
		log.Fatalf("init logger failed: %v", err)
	}
	defer utils.SyncLogger()

	utils.SugarLogger.Infow("starting cx3 shelf service...",
		"port", cfg.Server.Port,
		"mode", cfg.Server.Mode,
	)

	if err := redisrepo.InitRedis(&cfg.Redis); err != nil {
		utils.SugarLogger.Fatalw("init redis failed", "error", err)
	}
	utils.SugarLogger.Info("redis initialized successfully")

	if err := mysqlrepo.InitMySQL(&cfg.MySQL); err != nil {
		utils.SugarLogger.Warnw("init mysql failed (running without mysql)", "error", err)
	} else {
		utils.SugarLogger.Info("mysql initialized successfully")
	}

	r := router.SetupRouter(cfg)

	_ = middleware.CORSMiddleware

	srv := &http.Server{
		Addr:         fmt.Sprintf(":%d", cfg.Server.Port),
		Handler:      r,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	go func() {
		pprofAddr := fmt.Sprintf(":%d", cfg.Server.Port+1000)
		utils.SugarLogger.Infow("pprof server starting", "addr", pprofAddr)
		if err := http.ListenAndServe(pprofAddr, nil); err != nil {
			utils.SugarLogger.Warnw("pprof server failed", "error", err)
		}
	}()

	go func() {
		utils.SugarLogger.Infow("http server starting", "port", cfg.Server.Port)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			utils.SugarLogger.Fatalw("http server failed", "error", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	utils.SugarLogger.Info("shutting down server...")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		utils.SugarLogger.Fatalw("server forced to shutdown", "error", err)
	}

	mysqlrepo.CloseMySQL()
	redisrepo.CloseRedis()
	utils.SyncLogger()

	utils.SugarLogger.Info("server exited gracefully")
}

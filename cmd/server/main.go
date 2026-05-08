package main

import (
	"embed"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"ai-proxy-gateway/internal/api"
	"ai-proxy-gateway/internal/auth"
	"ai-proxy-gateway/internal/channel"
	"ai-proxy-gateway/internal/db"
	"ai-proxy-gateway/internal/failover"
	"ai-proxy-gateway/internal/models"
	"ai-proxy-gateway/internal/protocol"
	"ai-proxy-gateway/internal/proxy"
	mr "ai-proxy-gateway/internal/router"
)

//go:embed all:web/*
var webFiles embed.FS

var (
	version   = "dev"
	buildTime = "unknown"
	gitCommit = "unknown"
)

func main() {
	cfg := models.DefaultConfig()

	if port := os.Getenv("SERVER_PORT"); port != "" {
		if p, err := strconv.Atoi(port); err == nil {
			cfg.ServerPort = p
		}
	}
	if port := os.Getenv("ADMIN_PORT"); port != "" {
		if p, err := strconv.Atoi(port); err == nil {
			cfg.AdminPort = p
		}
	}
	if dbPath := os.Getenv("DATABASE_PATH"); dbPath != "" {
		cfg.DatabasePath = dbPath
	}
	if user := os.Getenv("ADMIN_USERNAME"); user != "" {
		cfg.AdminUsername = user
	}
	if pass := os.Getenv("ADMIN_PASSWORD"); pass != "" {
		cfg.AdminPassword = pass
	}
	if key := os.Getenv("PROXY_API_KEY"); key != "" {
		cfg.ProxyAPIKey = key
	}
	singlePort := os.Getenv("SINGLE_PORT") == "true"

	database, err := db.New(cfg.DatabasePath)
	if err != nil {
		log.Fatalf("Failed to initialize database: %v", err)
	}
	defer database.Close()

	chMgr := channel.New(database)
	fo := failover.NewFailoverManager(5, 60)
	modelRtr := mr.New(database)
	proxyHdl := proxy.New(chMgr, fo, modelRtr, database)
	protoAdapter := protocol.New(chMgr, fo, database)

	authMgr := auth.New(cfg.AdminUsername, cfg.AdminPassword, 24*time.Hour)

	srv := api.New(database, chMgr, fo, modelRtr, proxyHdl, authMgr, cfg.ProxyAPIKey, protoAdapter, version, buildTime, gitCommit)

	webFS, _ := fs.Sub(webFiles, "web")

	var server *http.Server

	if singlePort {
		// Single-port mode: proxy and admin share the same port
		mux := http.NewServeMux()
		srv.RegisterProxyRoutes(mux)
		srv.RegisterAdminRoutes(mux)
		mux.Handle("/", http.FileServer(http.FS(webFS)))

		server = &http.Server{
			Addr:    ":" + strconv.Itoa(cfg.ServerPort),
			Handler: mux,
		}

		go func() {
			log.Printf("Single-port server starting on :%d (proxy + admin + web)", cfg.ServerPort)
			if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				log.Fatalf("Server failed: %v", err)
			}
		}()
	} else {
		// Dual-port mode: separate proxy and admin servers
		proxyMux := http.NewServeMux()
		srv.RegisterProxyRoutes(proxyMux)

		adminMux := http.NewServeMux()
		srv.RegisterAdminRoutes(adminMux)
		adminMux.Handle("/", http.FileServer(http.FS(webFS)))

		proxyServer := &http.Server{
			Addr:    ":" + strconv.Itoa(cfg.ServerPort),
			Handler: proxyMux,
		}

		adminServer := &http.Server{
			Addr:    ":" + strconv.Itoa(cfg.AdminPort),
			Handler: adminMux,
		}

		go func() {
			log.Printf("Proxy server starting on :%d", cfg.ServerPort)
			if cfg.ProxyAPIKey != "" {
				log.Printf("Proxy API key authentication enabled")
			} else {
				log.Printf("WARNING: Proxy API key not set, all requests will be accepted")
			}
			if err := proxyServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				log.Fatalf("Proxy server failed: %v", err)
			}
		}()

		go func() {
			log.Printf("Admin panel starting on :%d (user: %s)", cfg.AdminPort, cfg.AdminUsername)
			if err := adminServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				log.Fatalf("Admin server failed: %v", err)
			}
		}()

		server = proxyServer
		_ = adminServer
	}

	// 定时清理过期会话
	go func() {
		ticker := time.NewTicker(10 * time.Minute)
		defer ticker.Stop()
		for range ticker.C {
			authMgr.CleanupExpired()
		}
	}()

	// 监听系统信号，优雅关闭
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Println("Shutting down servers...")
	server.Close()
}

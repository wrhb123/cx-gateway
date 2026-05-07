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

	database, err := db.New(cfg.DatabasePath)
	if err != nil {
		log.Fatalf("Failed to initialize database: %v", err)
	}
	defer database.Close()

	chMgr := channel.New(database)
	fo := failover.NewFailoverManager(5, 60)
	modelRtr := mr.New(database)
	proxyHdl := proxy.New(chMgr, fo, modelRtr)
	protoAdapter := protocol.New(chMgr, fo)

	authMgr := auth.New(cfg.AdminUsername, cfg.AdminPassword, 24*time.Hour)

	srv := api.New(database, chMgr, fo, modelRtr, proxyHdl, authMgr, cfg.ProxyAPIKey, protoAdapter)

	// 注册代理 API 路由
	proxyMux := http.NewServeMux()
	srv.RegisterProxyRoutes(proxyMux)

	// 注册管理 API 路由并挂载 Web 管理界面
	adminMux := http.NewServeMux()
	srv.RegisterAdminRoutes(adminMux)

	webFS, _ := fs.Sub(webFiles, "web")
	adminMux.Handle("/", http.FileServer(http.FS(webFS)))

	// 启动代理服务器
	proxyServer := &http.Server{
		Addr:    ":" + strconv.Itoa(cfg.ServerPort),
		Handler: proxyMux,
	}

	// 启动管理服务器
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
	proxyServer.Close()
	adminServer.Close()
}

// fanke 博客后端入口:加载配置 → 连接数据库(可选)→ 迁移 → 引导管理员 → 启动 HTTP。
package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"fanke/backend/internal/app"
	"fanke/backend/internal/auth"
	"fanke/backend/internal/config"
	"fanke/backend/internal/repo"
	"fanke/backend/internal/secret"
)

func main() {
	cfg := config.Load()

	var db *repo.DB
	if cfg.Configured() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		opened, err := repo.Open(ctx, cfg.DatabaseURL)
		cancel()
		if err != nil {
			log.Printf("数据库连接失败,进入引导模式: %v", err)
		} else {
			db = opened
		}
	} else {
		log.Printf("未配置 DATABASE_URL,以引导模式启动,请打开网页完成安装向导")
	}

	if db != nil {
		defer db.Close()
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		err := db.Migrate(ctx)
		cancel()
		if err != nil {
			log.Fatalf("迁移失败: %v", err)
		}
		// 字段级加密:AI/S3 等敏感字段落库前加密,历史明文就地迁移
		if box, berr := secret.NewBox(cfg.RecoveryKey); berr != nil {
			log.Printf("敏感字段加密初始化失败,按明文运行: %v", berr)
		} else if box != nil {
			db.Box = box
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			if err := db.EncryptLegacySecrets(ctx); err != nil {
				log.Printf("历史明文密钥迁移失败: %v", err)
			}
			cancel()
		}
		bootstrapAdmin(cfg, db)
	}

	application, err := app.New(cfg, db)
	if err != nil {
		log.Fatalf("初始化失败: %v", err)
	}
	srv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           application.Router(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
	}

	go func() {
		log.Printf("fanke api 监听于 %s", cfg.Addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("监听失败: %v", err)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		log.Printf("关闭 HTTP 服务出错: %v", err)
	}
	log.Println("服务已退出")
}

// bootstrapAdmin 仅在库里还没有任何用户时,用配置里的引导账号创建首个管理员。
func bootstrapAdmin(cfg *config.Config, db *repo.DB) {
	if cfg.BootstrapAdminEmail == "" || cfg.BootstrapAdminPassword == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	n, err := db.CountUsers(ctx)
	if err != nil || n > 0 {
		return
	}
	hash, err := auth.HashPassword(cfg.BootstrapAdminPassword)
	if err != nil {
		log.Printf("引导管理员密码加密失败: %v", err)
		return
	}
	id, err := db.CreateUser(ctx, cfg.BootstrapAdminEmail, cfg.BootstrapAdminUsername, hash, "admin", cfg.BootstrapAdminUsername)
	if err != nil {
		log.Printf("创建引导管理员失败: %v", err)
		return
	}
	log.Printf("已创建引导管理员 %s (id=%d)", cfg.BootstrapAdminUsername, id)
}

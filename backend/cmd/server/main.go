package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"project-hub/backend/internal/app"
)

func env(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	db, err := pgxpool.New(ctx, env("DATABASE_URL", "postgres://projecthub:projecthub-local@localhost:55432/projecthub?sslmode=disable"))
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()
	if err = db.Ping(ctx); err != nil {
		log.Fatal(err)
	}
	backupKey, err := app.ParseBackupKey(os.Getenv("BACKUP_MASTER_KEY"))
	if err != nil {
		log.Fatal(err)
	}
	a := &app.App{DB: db, UploadDir: env("UPLOAD_DIR", "./uploads"), SecureCookie: env("SECURE_COOKIE", "false") == "true", BackupKey: backupKey}
	if err = os.MkdirAll(a.UploadDir, 0700); err != nil {
		log.Fatal(err)
	}
	if err = a.Migrate(ctx); err != nil {
		log.Fatal(err)
	}
	if err = a.BootstrapAdmin(ctx, env("ADMIN_USERNAME", "admin"), os.Getenv("ADMIN_PASSWORD"), env("ADMIN_NAME", "管理员")); err != nil {
		log.Fatal(err)
	}
	a.StartAnalysisWorker(ctx)
	a.StartBackupWorker(ctx)
	server := &http.Server{Addr: ":" + env("PORT", "8080"), Handler: a.Routes(), ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 60 * time.Second}
	go func() {
		<-ctx.Done()
		shutdown, c := context.WithTimeout(context.Background(), 10*time.Second)
		defer c()
		_ = server.Shutdown(shutdown)
	}()
	log.Printf("工程台 API listening on %s", server.Addr)
	if err = server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}

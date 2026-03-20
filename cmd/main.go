package main

import (
	"context"
	"database/sql"
	"flag"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/hiimjako/syncinator/internal/env"
	"github.com/hiimjako/syncinator/internal/migration"
	syncinator "github.com/hiimjako/syncinator/pkg"
	"github.com/hiimjako/syncinator/pkg/filestorage"

	_ "github.com/mattn/go-sqlite3"
)

func main() {
	envPath := flag.String("env", ".env", ".env path")
	flag.Parse()

	ev := env.LoadEnv(*envPath)

	err := run(ev)
	if err != nil {
		log.Fatal(err)
	}
}

func run(ev *env.EnvVariables) error {
	log.Printf("running migrations")

	dbSqlite, err := sql.Open("sqlite3", ev.SqliteFilepath)
	if err != nil {
		return err
	}
	defer dbSqlite.Close()

	if _, err := dbSqlite.ExecContext(context.Background(), "PRAGMA journal_mode=WAL"); err != nil {
		return err
	}
	if _, err := dbSqlite.ExecContext(context.Background(), "PRAGMA foreign_keys = ON"); err != nil {
		return err
	}

	if err := migration.Migrate(dbSqlite); err != nil {
		return err
	}

	lc := net.ListenConfig{}
	l, err := lc.Listen(context.Background(), "tcp", net.JoinHostPort(ev.Host, ev.Port))
	if err != nil {
		return err
	}
	log.Printf("listening on http://%v", l.Addr())

	disk := filestorage.NewDisk(ev.StorageDir)

	handler := syncinator.New(dbSqlite, disk, syncinator.Options{
		JWTSecret:            ev.JWTSecret,
		OperationTTL:         ev.OperationTTL,
		CacheSize:            ev.CacheSize,
		MaxFileSizeMB:        ev.MaxFileSizeMB,
		MinChangesThreshold:  ev.MinChangesThreshold,
		FlushInterval:        ev.FlushInterval,
		SnapshotCheckpoint:   ev.SnapshotCheckpoint,
		MaxSnapshotDiffChain: ev.MaxSnapshotDiffChain,
	})
	defer handler.Close()

	s := &http.Server{
		Handler:     handler,
		ReadTimeout: time.Second * 10,
		// WriteTimeout is intentionally not set: it applies to the entire
		// connection lifetime and kills long-lived WebSocket connections.
	}
	errc := make(chan error, 1)
	go func() {
		errc <- s.Serve(l)
	}()

	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, os.Interrupt, syscall.SIGTERM)
	select {
	case err := <-errc:
		log.Printf("failed to serve: %v", err)
	case sig := <-sigs:
		log.Printf("terminating: %v", sig)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second*10)
	defer cancel()

	return s.Shutdown(ctx)
}

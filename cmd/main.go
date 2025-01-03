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
	"time"

	"github.com/hiimjako/syncinator/internal/env"
	"github.com/hiimjako/syncinator/internal/migration"
	"github.com/hiimjako/syncinator/internal/repository"
	"github.com/hiimjako/syncinator/syncinator"
	"github.com/hiimjako/syncinator/syncinator/filestorage"

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

	if err := migration.Migrate(dbSqlite); err != nil {
		return err
	}

	l, err := net.Listen("tcp", net.JoinHostPort(ev.Host, ev.Port))
	if err != nil {
		return err
	}
	log.Printf("listening on http://%v", l.Addr())

	db := repository.New(dbSqlite)
	disk := filestorage.NewDisk(ev.StorageDir)

	handler := syncinator.New(db, disk, syncinator.Options{
		JWTSecret: ev.JWTSecret,
	})
	defer handler.Close()

	s := &http.Server{
		Handler:      handler,
		ReadTimeout:  time.Second * 10,
		WriteTimeout: time.Second * 10,
	}
	errc := make(chan error, 1)
	go func() {
		errc <- s.Serve(l)
	}()

	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, os.Interrupt)
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

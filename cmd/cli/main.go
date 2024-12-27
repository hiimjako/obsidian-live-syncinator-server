package main

import (
	"context"
	"crypto/rand"
	"database/sql"
	"flag"
	"fmt"
	"os"

	"github.com/hiimjako/syncinator/internal/repository"
	_ "github.com/mattn/go-sqlite3"
	"golang.org/x/crypto/bcrypt"
)

const (
	saltSize   = 16    // Size of salt in bytes
	hashIter   = 10000 // Number of PBKDF2 iterations
	hashKeyLen = 32    // Length of generated key in bytes
)

func main() {
	workspaceName := flag.String("name", "", "workspace name")
	workspacePass := flag.String("pass", "", "workspace password")
	dbPath := flag.String("db", "", "sqlite db path")
	flag.Parse()

	if workspaceName == nil || *workspaceName == "" {
		flag.PrintDefaults()
		return
	}

	if workspacePass == nil || *workspacePass == "" {
		flag.PrintDefaults()
		return
	}

	if dbPath == nil || *dbPath == "" {
		flag.PrintDefaults()
		return
	}

	dbSqlite, err := sql.Open("sqlite3", *dbPath)
	failOnError(err)

	db := repository.New(dbSqlite)

	salt := make([]byte, saltSize)
	_, err = rand.Read(salt)
	failOnError(err)

	hash, err := bcrypt.GenerateFromPassword([]byte(*workspacePass), bcrypt.DefaultCost)
	failOnError(err)

	err = db.AddWorkspace(context.Background(), repository.AddWorkspaceParams{
		Name:     *workspaceName,
		Password: string(hash),
	})
	failOnError(err)

	fmt.Println("workspace created correctly")
}

func failOnError(err error) {
	if err != nil {
		fmt.Println("unable to create workspace")
		fmt.Println(err)
		os.Exit(1)
	}
}

package syncinator

import (
	"context"
	"database/sql"
	"io"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/hiimjako/syncinator/internal/repository"
	"github.com/hiimjako/syncinator/syncinator/filestorage"
	"golang.org/x/time/rate"
)

const (
	ApiV1Prefix = "/v1"

	PathWebSocket = ApiV1Prefix + "/sync"
	PathHttpApi   = ApiV1Prefix + "/api"
	PathHttpAuth  = ApiV1Prefix + "/auth"
)

type Options struct {
	JWTSecret       []byte
	MaxFileSize     int64
	OperationMaxAge time.Duration
}

func (o *Options) Default() {
	if o.MaxFileSize <= 0 {
		o.MaxFileSize = 10 << 20 // 10 MB
	}

	if o.OperationMaxAge <= 0 {
		o.OperationMaxAge = 12 * time.Hour
	}
}

type CachedFile struct {
	repository.File
	Content string
}

type syncinator struct {
	ctx    context.Context
	cancel context.CancelFunc
	mut    sync.Mutex

	jwtSecret       []byte
	maxFileSize     int64
	operationMaxAge time.Duration

	publishLimiter *rate.Limiter
	serverMux      *http.ServeMux
	subscribersMu  sync.Mutex
	subscribers    map[*subscriber]struct{}
	files          map[int64]CachedFile
	storageQueue   chan ChunkMessage
	storage        filestorage.Storage
	db             *repository.Queries
	conn           *sql.DB
}

func New(db *sql.DB, fs filestorage.Storage, opts Options) *syncinator {
	opts.Default()
	repo := repository.New(db)

	ctx, cancel := context.WithCancel(context.Background())
	s := &syncinator{
		ctx:    ctx,
		cancel: cancel,

		jwtSecret:       opts.JWTSecret,
		maxFileSize:     opts.MaxFileSize,
		operationMaxAge: opts.OperationMaxAge,

		serverMux:      http.NewServeMux(),
		publishLimiter: rate.NewLimiter(rate.Every(100*time.Millisecond), 8),
		subscribers:    make(map[*subscriber]struct{}),
		files:          make(map[int64]CachedFile),
		storageQueue:   make(chan ChunkMessage, 128),
		storage:        fs,
		conn:           db,
		db:             repo,
	}

	s.init()

	s.serverMux.Handle(PathHttpApi+"/", http.StripPrefix(PathHttpApi, s.apiHandler()))
	s.serverMux.Handle(PathHttpAuth+"/", http.StripPrefix(PathHttpAuth, s.authHandler()))
	s.serverMux.Handle(PathWebSocket, s.wsHandler())

	go s.internalBusProcessor()
	go s.deleteOldOperations()

	return s
}

func (s *syncinator) init() {
	files, err := s.db.FetchAllTextFiles(s.ctx)
	if err != nil {
		log.Panicf("error while fetching all files, %v\n", err)
	}

	for _, file := range files {
		fileReader, err := s.storage.ReadObject(file.DiskPath)
		if err != nil {
			log.Panicf("error while reading file, %v\n", err)
		}

		fileContent, err := io.ReadAll(fileReader)
		if err != nil {
			log.Panicf("error while reading file, %v\n", err)
		}
		fileReader.Close()

		s.files[file.ID] = CachedFile{
			File:    file,
			Content: string(fileContent),
		}
	}
}

func (s *syncinator) Close() error {
	if s.ctx.Err() != nil {
		s.cancel()
	}
	return nil
}

func (s *syncinator) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.serverMux.ServeHTTP(w, r)
}

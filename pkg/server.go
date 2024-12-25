package rtsync

import (
	"context"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/hiimjako/real-time-sync-obsidian-be/internal/repository"
	"github.com/hiimjako/real-time-sync-obsidian-be/pkg/filestorage"
	"golang.org/x/time/rate"
)

const (
	ApiV1Prefix = "/v1"

	PathWebSocket = ApiV1Prefix + "/sync"
	PathHttpApi   = ApiV1Prefix + "/api"
	PathHttpAuth  = ApiV1Prefix + "/auth"
)

type Options struct {
	JWTSecret   []byte
	MaxFileSize int64
}

func (o *Options) Default() {
	if o.MaxFileSize <= 0 {
		o.MaxFileSize = 10 << 20 // 10 MB
	}
}

type realTimeSyncServer struct {
	ctx    context.Context
	cancel context.CancelFunc
	mut    sync.Mutex

	jwtSecret   []byte
	maxFileSize int64

	publishLimiter *rate.Limiter
	serverMux      *http.ServeMux
	subscribersMu  sync.Mutex
	subscribers    map[*subscriber]struct{}
	files          map[int64]FileWithContent
	storageQueue   chan ChunkMessage
	eventQueue     chan EventMessage
	storage        filestorage.Storage
	db             *repository.Queries
}

func New(db *repository.Queries, s filestorage.Storage, opts Options) *realTimeSyncServer {
	opts.Default()

	ctx, cancel := context.WithCancel(context.Background())
	rts := &realTimeSyncServer{
		ctx:    ctx,
		cancel: cancel,

		jwtSecret:   opts.JWTSecret,
		maxFileSize: opts.MaxFileSize,

		serverMux:      http.NewServeMux(),
		publishLimiter: rate.NewLimiter(rate.Every(100*time.Millisecond), 8),
		subscribers:    make(map[*subscriber]struct{}),
		files:          make(map[int64]FileWithContent),
		storageQueue:   make(chan ChunkMessage, 128),
		eventQueue:     make(chan EventMessage, 128),
		storage:        s,
		db:             db,
	}

	rts.init()

	rts.serverMux.Handle(PathHttpApi+"/", http.StripPrefix(PathHttpApi, rts.apiHandler()))
	rts.serverMux.Handle(PathHttpAuth+"/", http.StripPrefix(PathHttpAuth, rts.authHandler()))
	rts.serverMux.HandleFunc(PathWebSocket, rts.wsHandler)

	go rts.internalBusProcessor()

	return rts
}

func (rts *realTimeSyncServer) init() {
	files, err := rts.db.FetchAllFiles(rts.ctx)
	if err != nil {
		log.Panicf("error while fetching all files, %v\n", err)
	}

	for _, file := range files {
		content, err := rts.storage.ReadObject(file.DiskPath)
		if err != nil {
			log.Panicf("error while reading file, %v\n", err)
		}

		rts.files[file.ID] = FileWithContent{
			File:    file,
			Content: string(content),
		}
	}
}

func (rts *realTimeSyncServer) Close() error {
	if rts.ctx.Err() != nil {
		rts.cancel()
	}
	return nil
}

func (rts *realTimeSyncServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	rts.serverMux.ServeHTTP(w, r)
}

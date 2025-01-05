package syncinator

import (
	"context"
	"database/sql"
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
	JWTSecret   []byte
	MaxFileSize int64
	CacheMaxAge time.Duration
}

func (o *Options) Default() {
	if o.MaxFileSize <= 0 {
		o.MaxFileSize = 10 << 20 // 10 MB
	}

	if o.CacheMaxAge <= 0 {
		o.CacheMaxAge = 0
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

	jwtSecret   []byte
	maxFileSize int64
	cacheMaxAge time.Duration

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

		jwtSecret:   opts.JWTSecret,
		maxFileSize: opts.MaxFileSize,
		cacheMaxAge: opts.CacheMaxAge,

		serverMux:      http.NewServeMux(),
		publishLimiter: rate.NewLimiter(rate.Every(100*time.Millisecond), 8),
		subscribers:    make(map[*subscriber]struct{}),
		files:          make(map[int64]CachedFile),
		storageQueue:   make(chan ChunkMessage, 128),
		storage:        fs,
		conn:           db,
		db:             repo,
	}

	s.serverMux.Handle(PathHttpApi+"/", http.StripPrefix(PathHttpApi, s.apiHandler()))
	s.serverMux.Handle(PathHttpAuth+"/", http.StripPrefix(PathHttpAuth, s.authHandler()))
	s.serverMux.Handle(PathWebSocket, s.wsHandler())

	go s.internalBusProcessor()
	go s.purgeCache()

	return s
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

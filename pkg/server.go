package syncinator

import (
	"context"
	"database/sql"
	"net/http"
	"sync"
	"time"

	"github.com/hiimjako/syncinator/internal/repository"
	"github.com/hiimjako/syncinator/pkg/filestorage"
	"golang.org/x/time/rate"
)

const (
	ApiV1Prefix = "/v1"

	PathWebSocket = ApiV1Prefix + "/sync"
	PathHttpApi   = ApiV1Prefix + "/api"
	PathHttpAuth  = ApiV1Prefix + "/auth"
)

type Options struct {
	JWTSecret           []byte
	MaxFileSizeMB       int64
	CacheMaxAge         time.Duration
	MinChangesThreshold int64
	FlushInterval       time.Duration
}

func (o *Options) Default() {
	if o.MaxFileSizeMB <= 0 {
		o.MaxFileSizeMB = 1
	}

	if o.CacheMaxAge <= 0 {
		o.CacheMaxAge = 0
	}

	if o.MinChangesThreshold < 0 {
		o.MinChangesThreshold = 5
	}

	if o.FlushInterval <= 0 {
		o.FlushInterval = 1 * time.Minute
	}
}

type CachedFile struct {
	repository.File
	Content        string
	pendingChanges int64
}

type syncinator struct {
	ctx    context.Context
	cancel context.CancelFunc
	mut    sync.Mutex

	jwtSecret           []byte
	maxFileSizeBytes    int64
	cacheMaxAge         time.Duration
	minChangesThreshold int64
	flushInterval       time.Duration

	publishLimiter *rate.Limiter
	serverMux      *http.ServeMux
	subscribersMu  sync.Mutex
	subscribers    map[*subscriber]struct{}
	files          map[int64]CachedFile
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

		jwtSecret:           opts.JWTSecret,
		maxFileSizeBytes:    opts.MaxFileSizeMB << 20,
		cacheMaxAge:         opts.CacheMaxAge,
		minChangesThreshold: opts.MinChangesThreshold,
		flushInterval:       opts.FlushInterval,

		serverMux:      http.NewServeMux(),
		publishLimiter: rate.NewLimiter(rate.Every(100*time.Millisecond), 8),
		subscribers:    make(map[*subscriber]struct{}),
		files:          make(map[int64]CachedFile),
		storage:        fs,
		conn:           db,
		db:             repo,
	}

	s.serverMux.Handle(PathHttpApi+"/", http.StripPrefix(PathHttpApi, s.apiHandler()))
	s.serverMux.Handle(PathHttpAuth+"/", http.StripPrefix(PathHttpAuth, s.authHandler()))
	s.serverMux.Handle(PathWebSocket, s.wsHandler())

	go s.processFileChanges()
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

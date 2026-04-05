package syncinator

import (
	"context"
	"database/sql"
	"log"
	"net/http"
	"sync"
	"time"

	lru "github.com/hashicorp/golang-lru/v2"
	"github.com/hiimjako/syncinator/internal/repository"
	"github.com/hiimjako/syncinator/pkg/filestorage"
	"golang.org/x/sync/singleflight"
	"golang.org/x/time/rate"
)

const (
	APIV1Prefix = "/v1"

	PathWebSocket = APIV1Prefix + "/sync"
	PathHTTPAPI   = APIV1Prefix + "/api"
	PathHTTPAuth  = APIV1Prefix + "/auth"
)

type Options struct {
	JWTSecret              []byte
	MaxFileSizeMB          int64
	OperationTTL           time.Duration
	CacheSize              int
	MinChangesThreshold    int64
	FlushInterval          time.Duration
	SnapshotCheckpoint     int64 // Create full snapshot every N versions
	MaxSnapshotDiffChain   int64 // Max consecutive diffs before forcing full snapshot
	SubscriberRateInterval time.Duration
	SubscriberRateBurst    int
	PurgeCacheInterval     time.Duration
}

func (o *Options) Default() {
	if o.MaxFileSizeMB <= 0 {
		o.MaxFileSizeMB = 1
	}

	if o.OperationTTL <= 0 {
		o.OperationTTL = 0
	}

	if o.CacheSize <= 0 {
		o.CacheSize = 128
	}

	if o.MinChangesThreshold < 0 {
		o.MinChangesThreshold = 5
	}

	if o.FlushInterval <= 0 {
		o.FlushInterval = 1 * time.Minute
	}

	if o.SnapshotCheckpoint <= 0 {
		o.SnapshotCheckpoint = 5 // Full snapshot every 5 versions
	}

	if o.MaxSnapshotDiffChain <= 0 {
		o.MaxSnapshotDiffChain = 10 // Max 10 consecutive diffs
	}

	if o.SubscriberRateInterval <= 0 {
		o.SubscriberRateInterval = 50 * time.Millisecond
	}

	if o.SubscriberRateBurst <= 0 {
		o.SubscriberRateBurst = 16
	}

	if o.PurgeCacheInterval <= 0 {
		o.PurgeCacheInterval = 10 * time.Minute
	}
}

type CachedFile struct {
	repository.File
	Content        string
	pendingChanges int64
}

type LockedCachedFile struct {
	mut sync.Mutex
	CachedFile
}

type workspaceSubscribers struct {
	mu   sync.Mutex
	subs map[*subscriber]struct{}
}

type syncinator struct {
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	jwtSecret              []byte
	maxFileSizeBytes       int64
	operationTTL           time.Duration
	minChangesThreshold    int64
	flushInterval          time.Duration
	snapshotCheckpoint     int64
	maxSnapshotDiffChain   int64
	subscriberRateInterval time.Duration
	subscriberRateBurst    int
	purgeCacheInterval     time.Duration

	publishLimiter *rate.Limiter
	serverMux      *http.ServeMux
	subscribersMu  sync.RWMutex
	// empty workspace entries are not cleaned up to avoid write-locking during broadcast
	subscribers map[int64]*workspaceSubscribers
	fileCache   *lru.Cache[int64, *LockedCachedFile]
	loader      *singleflight.Group
	storage     filestorage.Storage
	db          *repository.Queries
	conn        *sql.DB
}

func New(db *sql.DB, fs filestorage.Storage, opts Options) *syncinator {
	opts.Default()
	repo := repository.New(db)

	ctx, cancel := context.WithCancel(context.Background())
	s := &syncinator{
		ctx:    ctx,
		cancel: cancel,

		jwtSecret:              opts.JWTSecret,
		maxFileSizeBytes:       opts.MaxFileSizeMB << 20,
		operationTTL:           opts.OperationTTL,
		minChangesThreshold:    opts.MinChangesThreshold,
		flushInterval:          opts.FlushInterval,
		snapshotCheckpoint:     opts.SnapshotCheckpoint,
		maxSnapshotDiffChain:   opts.MaxSnapshotDiffChain,
		subscriberRateInterval: opts.SubscriberRateInterval,
		subscriberRateBurst:    opts.SubscriberRateBurst,
		purgeCacheInterval:     opts.PurgeCacheInterval,

		serverMux:      http.NewServeMux(),
		publishLimiter: rate.NewLimiter(rate.Every(opts.SubscriberRateInterval), opts.SubscriberRateBurst),
		subscribers:    make(map[int64]*workspaceSubscribers),
		loader:         &singleflight.Group{},
		storage:        fs,
		conn:           db,
		db:             repo,
	}

	s.initCache(opts.CacheSize)

	s.serverMux.HandleFunc("/healthz", s.healthzHandler)
	s.serverMux.HandleFunc("/readyz", s.readyzHandler)
	s.serverMux.Handle(PathHTTPAPI+"/", http.StripPrefix(PathHTTPAPI, s.apiHandler()))
	s.serverMux.Handle(PathHTTPAuth+"/", http.StripPrefix(PathHTTPAuth, s.authHandler()))
	s.serverMux.Handle(PathWebSocket, s.wsHandler())

	s.wg.Add(2)
	go func() {
		defer s.wg.Done()
		s.processFileChanges()
	}()
	go func() {
		defer s.wg.Done()
		s.purgeCache()
	}()

	return s
}

func (s *syncinator) initCache(cacheSize int) {
	onEvicted := func(_ int64, file *LockedCachedFile) {
		file.mut.Lock()
		defer file.mut.Unlock()

		err := s.flushFileToStorage(file.CachedFile)
		if err != nil {
			log.Printf("error while writing file %d before purge: %v\n", file.ID, err)
		}
	}

	fileCache, err := lru.NewWithEvict[int64, *LockedCachedFile](cacheSize, onEvicted)
	if err != nil {
		log.Fatalf("error creating lru cache: %v", err)
	}
	s.fileCache = fileCache
}

func (s *syncinator) healthzHandler(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
}

func (s *syncinator) readyzHandler(w http.ResponseWriter, _ *http.Request) {
	if err := s.conn.PingContext(s.ctx); err != nil {
		w.WriteHeader(http.StatusServiceUnavailable)
		return
	}
	w.WriteHeader(http.StatusOK)
}

func (s *syncinator) Close() error {
	if s.ctx.Err() == nil {
		s.cancel()
	}
	s.wg.Wait()
	return nil
}

func (s *syncinator) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.serverMux.ServeHTTP(w, r)
}

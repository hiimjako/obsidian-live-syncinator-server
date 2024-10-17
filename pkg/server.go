package rtsync

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/hiimjako/real-time-sync-obsidian-be/pkg/diff"
	"golang.org/x/time/rate"
)

type realTimeSyncServer struct {
	subscriberMessageBuffer int
	publishLimiter          *rate.Limiter
	serveMux                http.ServeMux
	subscribersMu           sync.Mutex
	subscribers             map[*subscriber]struct{}
	files                   map[string]string
}

func New() *realTimeSyncServer {
	rts := &realTimeSyncServer{
		subscriberMessageBuffer: 8,
		publishLimiter:          rate.NewLimiter(rate.Every(100*time.Millisecond), 8),
		subscribers:             make(map[*subscriber]struct{}),
		files:                   make(map[string]string),
	}

	rts.serveMux.HandleFunc("/subscribe", rts.subscribeHandler)
	rts.serveMux.HandleFunc("/publish/{fileId}", rts.publishHandler)

	return rts
}

type subscriber struct {
	msgs      chan []byte
	closeSlow func()
}

func (rts *realTimeSyncServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	rts.serveMux.ServeHTTP(w, r)
}

func (rts *realTimeSyncServer) subscribeHandler(w http.ResponseWriter, r *http.Request) {
	err := rts.subscribe(w, r)
	if errors.Is(err, context.Canceled) {
		return
	}
	if websocket.CloseStatus(err) == websocket.StatusNormalClosure ||
		websocket.CloseStatus(err) == websocket.StatusGoingAway {
		return
	}
	if err != nil {
		log.Printf("%v", err)
		return
	}
}

func (rts *realTimeSyncServer) publishHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}

	fileId := r.PathValue("fileId")
	if fileId == "" {
		http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, http.StatusText(http.StatusUnprocessableEntity), http.StatusUnprocessableEntity)
		return
	}
	defer r.Body.Close()

	var data []diff.DiffChunk
	err = json.Unmarshal(body, &data)
	if err != nil {
		http.Error(w, http.StatusText(http.StatusUnprocessableEntity), http.StatusUnprocessableEntity)
		return
	}

	localCopy := rts.files[fileId]
	for _, d := range data {
		localCopy = diff.ApplyDiff(localCopy, d)
	}
	diffs := diff.ComputeDiff(rts.files[fileId], localCopy)
	rts.files[fileId] = localCopy

	diffsByte, err := json.Marshal(diffs)
	if err != nil {
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	rts.publish(diffsByte)

	w.WriteHeader(http.StatusAccepted)
}

func (rts *realTimeSyncServer) subscribe(w http.ResponseWriter, r *http.Request) error {
	var mu sync.Mutex
	var c *websocket.Conn
	var closed bool
	s := &subscriber{
		msgs: make(chan []byte, rts.subscriberMessageBuffer),
		closeSlow: func() {
			mu.Lock()
			defer mu.Unlock()
			closed = true
			if c != nil {
				c.Close(websocket.StatusPolicyViolation, "connection too slow to keep up with messages")
			}
		},
	}
	rts.addSubscriber(s)
	defer rts.deleteSubscriber(s)

	c2, err := websocket.Accept(w, r, nil)
	if err != nil {
		return err
	}
	mu.Lock()
	if closed {
		mu.Unlock()
		return net.ErrClosed
	}
	c = c2
	mu.Unlock()
	//nolint:errcheck
	defer c.CloseNow()

	ctx := c.CloseRead(context.Background())

	for {
		select {
		case msg := <-s.msgs:
			err := writeTimeout(ctx, time.Second*5, c, msg)
			if err != nil {
				return err
			}
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

// publish publishes the msg to all subscribers.
// It never blocks and so messages to slow subscribers
// are dropped.
func (rts *realTimeSyncServer) publish(msg []byte) {
	rts.subscribersMu.Lock()
	defer rts.subscribersMu.Unlock()

	err := rts.publishLimiter.Wait(context.Background())
	if err != nil {
		log.Print(err)
	}

	for s := range rts.subscribers {
		select {
		case s.msgs <- msg:
		default:
			go s.closeSlow()
		}
	}
}
func (rts *realTimeSyncServer) addSubscriber(s *subscriber) {
	rts.subscribersMu.Lock()
	rts.subscribers[s] = struct{}{}
	rts.subscribersMu.Unlock()
}

// deleteSubscriber deletes the given subscriber.
func (rts *realTimeSyncServer) deleteSubscriber(s *subscriber) {
	rts.subscribersMu.Lock()
	delete(rts.subscribers, s)
	rts.subscribersMu.Unlock()
}

func writeTimeout(ctx context.Context, timeout time.Duration, c *websocket.Conn, msg []byte) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	return c.Write(ctx, websocket.MessageText, msg)
}

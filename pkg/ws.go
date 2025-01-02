package rtsync

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"

	"github.com/coder/websocket"
	"github.com/hiimjako/syncinator/internal/repository"
	"github.com/hiimjako/syncinator/pkg/diff"
	"github.com/hiimjako/syncinator/pkg/middleware"
)

type MessageType = int

const (
	ChunkEventType  MessageType = iota
	CreateEventType MessageType = iota
	DeleteEventType MessageType = iota
	RenameEventType MessageType = iota
)

type WsMessageHeader struct {
	FileId int64       `json:"fileId"`
	Type   MessageType `json:"type"`
}

type EventMessage struct {
	WsMessageHeader
	WorkspacePath string `json:"workspacePath"`
	ObjectType    string `json:"objectType"`
}

type ChunkMessage struct {
	WsMessageHeader
	Chunks  []diff.DiffChunk `json:"chunks"`
	Version int64            `json:"version"`
}

func (s *syncinator) wsHandler() http.Handler {
	router := http.NewServeMux()
	router.HandleFunc("GET /", s.createSubscriber)

	stack := middleware.CreateStack(
		middleware.IsAuthenticated(middleware.AuthOptions{SecretKey: s.jwtSecret}, middleware.ExtractWsToken),
	)

	routerWithStack := stack(router)
	return routerWithStack
}

func (s *syncinator) createSubscriber(w http.ResponseWriter, r *http.Request) {
	err := s.subscribe(w, r)
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

func (s *syncinator) subscribe(w http.ResponseWriter, r *http.Request) error {
	sub, err := NewSubscriber(s.ctx, w, r, s.onChunkMessage, s.onEventMessage)
	if err != nil {
		return err
	}

	s.addSubscriber(sub)
	defer s.deleteSubscriber(sub)

	log.Printf("client %s connected", sub.clientID)

	sub.Listen()

	return nil
}

func (s *syncinator) onEventMessage(sender *subscriber, event EventMessage) {
	s.broadcastMessage(sender, event)
}

func (s *syncinator) onChunkMessage(sender *subscriber, data ChunkMessage) {
	s.mut.Lock()
	defer s.mut.Unlock()

	file := s.files[data.FileId]
	localCopy := file.Content
	for _, d := range data.Chunks {
		localCopy = diff.ApplyDiff(localCopy, d)
	}
	diffs := diff.ComputeDiff(file.Content, localCopy)

	data.Version += 1
	file.Version += 1
	file.Content = localCopy
	s.files[data.FileId] = file

	if len(diffs) > 0 {
		s.storageQueue <- data
		s.broadcastMessage(sender, ChunkMessage{
			WsMessageHeader: data.WsMessageHeader,
			Chunks:          diffs,
			Version:         file.Version,
		})
	}
}

func (s *syncinator) broadcastMessage(sender *subscriber, msg any) {
	if sender == nil {
		log.Println("broadcasting with nil sender")
		return
	}

	s.subscribersMu.Lock()
	defer s.subscribersMu.Unlock()

	err := s.publishLimiter.Wait(context.Background())
	if err != nil {
		log.Println(err)
	}

	for sub := range s.subscribers {
		isSameWorkspace := sub.workspaceID == sender.workspaceID
		isSameClient := sub.clientID == sender.clientID
		shouldSend := isSameWorkspace && !isSameClient

		if !shouldSend {
			continue
		}

		switch m := msg.(type) {
		case ChunkMessage:
			select {
			case sub.chunkMsgQueue <- m:
			default:
				go sub.closeSlow()
			}
		case EventMessage:
			select {
			case sub.eventMsgQueue <- m:
			default:
				go sub.closeSlow()
			}
		default:
			log.Printf("Unknown message type: %T\n", msg)
		}
	}
}

func (s *syncinator) internalBusProcessor() {
	for {
		select {
		case chunkMsg := <-s.storageQueue:
			err := s.applyChunkToFile(chunkMsg)
			if err != nil {
				log.Println(err)
			}
		case <-s.ctx.Done():
			return
		}
	}
}

func (s *syncinator) applyChunkToFile(chunkMsg ChunkMessage) error {
	file, err := s.db.FetchFile(s.ctx, chunkMsg.FileId)
	if err != nil {
		return err
	}

	// FIXME: add a rollback strategy
	for _, d := range chunkMsg.Chunks {
		err := s.storage.PersistChunk(file.DiskPath, d)
		if err != nil {
			log.Println(err)
		}
	}

	operation, err := json.Marshal(chunkMsg)
	if err != nil {
		return err
	}
	_, err = s.db.CreateOperation(s.ctx, repository.CreateOperationParams{
		FileID:    file.ID,
		Version:   chunkMsg.Version,
		Operation: string(operation),
	})
	if err != nil {
		return err
	}

	err = s.db.UpdateFileVersion(s.ctx, repository.UpdateFileVersionParams{
		ID:      chunkMsg.FileId,
		Version: chunkMsg.Version,
	})
	if err != nil {
		return err
	}

	return nil
}

func (s *syncinator) addSubscriber(sub *subscriber) {
	s.subscribersMu.Lock()
	s.subscribers[sub] = struct{}{}
	s.subscribersMu.Unlock()
}

// deleteSubscriber deletes the given subscriber.
func (s *syncinator) deleteSubscriber(sub *subscriber) {
	s.subscribersMu.Lock()
	delete(s.subscribers, sub)
	s.subscribersMu.Unlock()
}

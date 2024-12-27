package rtsync

import (
	"context"
	"errors"
	"log"
	"net/http"

	"github.com/coder/websocket"
	"github.com/hiimjako/syncinator/pkg/diff"
)

type MessageType = int

const (
	ChunkEventType  MessageType = iota
	CreateEventType MessageType = iota
	DeleteEventType MessageType = iota
	RenameEventType MessageType = iota
)

type WsMessageHeader struct {
	SenderId string      `json:"-"`
	FileId   int64       `json:"fileId"`
	Type     MessageType `json:"type"`
}

type EventMessage struct {
	WsMessageHeader
	WorkspacePath string `json:"workspacePath"`
	ObjectType    string `json:"objectType"`
}

type ChunkMessage struct {
	WsMessageHeader
	Chunks []diff.DiffChunk `json:"chunks"`
}

func (s *syncinator) wsHandler(w http.ResponseWriter, r *http.Request) {
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

	sub.Listen()

	return nil
}

func (s *syncinator) onEventMessage(event EventMessage) {
	s.broadcastEventMessage(event)
}

func (s *syncinator) onChunkMessage(data ChunkMessage) {
	s.mut.Lock()
	defer s.mut.Unlock()

	file := s.files[data.FileId]
	localCopy := file.Content
	for _, d := range data.Chunks {
		localCopy = diff.ApplyDiff(localCopy, d)
	}
	diffs := diff.ComputeDiff(file.Content, localCopy)

	file.Content = localCopy
	s.files[data.FileId] = file

	if len(diffs) > 0 {
		s.storageQueue <- data
		s.broadcastChunkMessage(ChunkMessage{
			WsMessageHeader: data.WsMessageHeader,
			Chunks:          diffs,
		})
	}
}

// broadcastPublish publishes the msg to all subscribers.
// It never blocks and so messages to slow subscribers
// are dropped.
func (s *syncinator) broadcastChunkMessage(msg ChunkMessage) {
	s.subscribersMu.Lock()
	defer s.subscribersMu.Unlock()

	err := s.publishLimiter.Wait(context.Background())
	if err != nil {
		log.Print(err)
	}

	for s := range s.subscribers {
		select {
		case s.chunkMsgQueue <- msg:
		default:
			go s.closeSlow()
		}
	}
}

func (s *syncinator) broadcastEventMessage(msg EventMessage) {
	s.subscribersMu.Lock()
	defer s.subscribersMu.Unlock()

	err := s.publishLimiter.Wait(context.Background())
	if err != nil {
		log.Print(err)
	}

	for s := range s.subscribers {
		select {
		case s.eventMsgQueue <- msg:
		default:
			go s.closeSlow()
		}
	}
}

func (s *syncinator) internalBusProcessor() {
	for {
		select {
		case chunkMsg := <-s.storageQueue:
			for _, d := range chunkMsg.Chunks {
				file, err := s.db.FetchFile(context.Background(), chunkMsg.FileId)
				if err != nil {
					log.Println(err)
					return
				}

				err = s.storage.PersistChunk(file.DiskPath, d)
				if err != nil {
					log.Println(err)
				}

				err = s.db.UpdateUpdatedAt(context.Background(), chunkMsg.FileId)
				if err != nil {
					log.Println(err)
				}
			}
		case event := <-s.eventQueue:
			s.broadcastEventMessage(event)
		case <-s.ctx.Done():
			return
		}
	}
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

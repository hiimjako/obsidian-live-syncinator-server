package syncinator

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/google/uuid"
	"github.com/hiimjako/syncinator/pkg/middleware"
)

type subscriber struct {
	conn *websocket.Conn
	w    http.ResponseWriter
	r    *http.Request
	ctx  context.Context

	isConnected     atomic.Bool
	clientID        string
	workspaceID     int64
	chunkMsgQueue   chan ChunkMessage
	eventMsgQueue   chan EventMessage
	cursorMsgQueue  chan CursorMessage
	closeSlow       func()
	onChunkMessage  func(*subscriber, ChunkMessage)
	onEventMessage  func(*subscriber, EventMessage)
	onCursorMessage func(*subscriber, CursorMessage)
}

func NewSubscriber(
	ctx context.Context,
	w http.ResponseWriter,
	r *http.Request,
	onChunkMessage func(*subscriber, ChunkMessage),
	onEventMessage func(*subscriber, EventMessage),
	onCursorMessage func(*subscriber, CursorMessage),
) (*subscriber, error) {
	c, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		OriginPatterns: []string{"localhost", "127.0.0.1", "obsidian.md"},
	})
	if err != nil {
		return nil, err
	}

	const subscriberMessageBuffer = 8
	workspaceID := middleware.WorkspaceIDFromCtx(r.Context())

	s := &subscriber{
		conn:           c,
		w:              w,
		r:              r,
		ctx:            ctx,
		isConnected:    atomic.Bool{},
		chunkMsgQueue:  make(chan ChunkMessage, subscriberMessageBuffer),
		eventMsgQueue:  make(chan EventMessage, subscriberMessageBuffer),
		cursorMsgQueue: make(chan CursorMessage, subscriberMessageBuffer),
		workspaceID:    workspaceID,
		clientID:       uuid.New().String(),
		closeSlow: func() {
			if c != nil {
				c.Close(websocket.StatusPolicyViolation, "connection too slow to keep up with messages")
			}
		},
		onChunkMessage:  onChunkMessage,
		onEventMessage:  onEventMessage,
		onCursorMessage: onCursorMessage,
	}

	s.isConnected.Store(true)

	return s, nil
}

func (s *subscriber) IsConnected() bool {
	return s.isConnected.Load()
}

func (s *subscriber) Close() error {
	log.Printf("client %s (%d) disconnected\n", s.clientID, s.workspaceID)
	s.isConnected.Store(false)
	return s.conn.CloseNow()
}

func (s *subscriber) Listen() {
	// on ws message
	go func() {
		for {
			if !s.IsConnected() {
				s.Close()
				return
			}

			msg, err := s.WaitMessage()
			if err != nil {
				s.checkWsError(err)
				s.Close()
				continue
			}

			msgType, err := s.MessageType(msg)
			if err != nil {
				log.Println(err)
				continue
			}

			switch msgType {
			case ChunkEventType:
				var chunk ChunkMessage
				err := mapToStruct(msg, &chunk)
				if err != nil {
					log.Println(err)
					continue
				}

				s.onChunkMessage(s, chunk)
			case RenameEventType, CreateEventType, DeleteEventType:
				var event EventMessage
				err := mapToStruct(msg, &event)
				if err != nil {
					log.Println(err)
					continue
				}

				s.onEventMessage(s, event)
			case CursorEventType:
				var curosr CursorMessage
				err := mapToStruct(msg, &curosr)
				if err != nil {
					log.Println(err)
					continue
				}

				s.onCursorMessage(s, curosr)
			}
		}
	}()

	// on internal queue event
	go func() {
		for {
			select {
			case chunkMsg := <-s.chunkMsgQueue:
				err := s.WriteMessage(chunkMsg, time.Second*1)
				if err != nil {
					log.Printf("error sending chunk message from %s (%d): %v\n", s.clientID, s.workspaceID, err)
					s.checkWsError(err)
				}
			case eventMsg := <-s.eventMsgQueue:
				err := s.WriteMessage(eventMsg, time.Second*1)
				if err != nil {
					log.Printf("error sending event message from %s (%d): %v\n", s.clientID, s.workspaceID, err)
					s.checkWsError(err)
				}
			case cursorMsg := <-s.cursorMsgQueue:
				err := s.WriteMessage(cursorMsg, time.Second*1)
				if err != nil {
					log.Printf("error sending cursor message from %s (%d): %v\n", s.clientID, s.workspaceID, err)
					s.checkWsError(err)
				}
			case <-s.ctx.Done():
				s.Close()
				return
			case <-s.r.Context().Done():
				s.Close()
				return
			}
		}
	}()

	<-s.ctx.Done()
}

func (s *subscriber) MessageType(data map[string]any) (int, error) {
	msgType, ok := data["type"].(float64)
	if !ok {
		return 0, fmt.Errorf("type in %+v not present", data)
	}

	return int(msgType), nil
}

func (s *subscriber) WaitMessage() (map[string]any, error) {
	var msg map[string]any

	err := wsjson.Read(s.ctx, s.conn, &msg)
	if err != nil {
		return msg, err
	}

	return msg, nil
}

func (s *subscriber) WriteMessage(msg any, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(s.ctx, timeout)
	defer cancel()

	return wsjson.Write(ctx, s.conn, msg)
}

func mapToStruct(data map[string]any, result interface{}) error {
	jsonData, err := json.Marshal(data)
	if err != nil {
		return err
	}

	if err := json.Unmarshal(jsonData, &result); err != nil {
		return err
	}
	return nil
}

func (s *subscriber) checkWsError(err error) {
	if websocket.CloseStatus(err) != -1 || strings.Contains(err.Error(), "EOF") {
		log.Printf("client %s (%d) fatal error, closing connection\n", s.clientID, s.workspaceID)
		s.Close()
		return
	}

	if err != nil {
		log.Println(err)
	}
}

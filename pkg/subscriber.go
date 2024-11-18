package rtsync

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
)

type subscriber struct {
	conn *websocket.Conn
	w    http.ResponseWriter
	r    *http.Request
	ctx  context.Context

	isConnected    atomic.Bool
	clientId       string
	chunkMsgQueue  chan ChunkMessage
	eventMsgQueue  chan EventMessage
	closeSlow      func()
	onChunkMessage func(ChunkMessage)
	onEventMessage func(EventMessage)
}

func NewSubscriber(
	ctx context.Context,
	w http.ResponseWriter,
	r *http.Request,
	onChunkMessage func(ChunkMessage),
	onEventMessage func(EventMessage),
) (*subscriber, error) {
	c, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		OriginPatterns: []string{"127.0.0.1", "obsidian.md"},
	})
	if err != nil {
		return nil, err
	}

	const subscriberMessageBuffer = 8
	s := &subscriber{
		conn:          c,
		w:             w,
		r:             r,
		ctx:           ctx,
		isConnected:   atomic.Bool{},
		chunkMsgQueue: make(chan ChunkMessage, subscriberMessageBuffer),
		eventMsgQueue: make(chan EventMessage, subscriberMessageBuffer),
		clientId:      uuid.New().String(),
		closeSlow: func() {
			if c != nil {
				c.Close(websocket.StatusPolicyViolation, "connection too slow to keep up with messages")
			}
		},
		onChunkMessage: onChunkMessage,
		onEventMessage: onEventMessage,
	}

	s.isConnected.Store(true)

	return s, nil
}

func (s *subscriber) IsOpen() bool {
	return s.isConnected.Load()
}

func (s *subscriber) Close() error {
	s.isConnected.Store(false)
	return s.conn.CloseNow()
}

func (s *subscriber) Listen() {
	// on ws message
	go func() {
		for {
			if !s.IsOpen() {
				return
			}

			msg, err := s.WaitMessage()
			if err != nil {
				log.Println(err)
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

				chunk.SenderId = s.clientId

				s.onChunkMessage(chunk)
			case RenameEventType, CreateEventType, DeleteEventType:
				var event EventMessage
				err := mapToStruct(msg, &event)
				if err != nil {
					log.Println(err)
					continue
				}

				event.SenderId = s.clientId

				s.onEventMessage(event)
			}
		}
	}()

	// on internal queue event
	go func() {
		for {
			select {
			case chunkMsg := <-s.chunkMsgQueue:
				if chunkMsg.SenderId == s.clientId {
					continue
				}

				err := s.WriteMessage(chunkMsg, time.Second*1)
				if err != nil {
					log.Println("error writing message to client", err)
				}
			case eventMsg := <-s.eventMsgQueue:
				if eventMsg.SenderId == s.clientId {
					continue
				}

				err := s.WriteMessage(eventMsg, time.Second*1)
				if err != nil {
					log.Println("error writing message to client", err)
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

func (s *subscriber) ParseChunkMessage() (ChunkMessage, error) {
	var data ChunkMessage

	err := wsjson.Read(s.ctx, s.conn, &data)
	if err != nil {
		if websocket.CloseStatus(err) != -1 || strings.Contains(err.Error(), "EOF") {
			s.Close()
			return data, fmt.Errorf("client %s disconnected", s.clientId)
		}

		return data, err
	}

	return data, err
}

func (s *subscriber) ParseEventMessage() (EventMessage, error) {
	var data EventMessage

	err := wsjson.Read(s.ctx, s.conn, &data)
	if err != nil {
		if websocket.CloseStatus(err) != -1 || strings.Contains(err.Error(), "EOF") {
			s.Close()
			return data, fmt.Errorf("client %s disconnected", s.clientId)
		}

		return data, err
	}

	return data, err
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
		if websocket.CloseStatus(err) != -1 || strings.Contains(err.Error(), "EOF") {
			s.Close()
			return msg, fmt.Errorf("client %s disconnected", s.clientId)
		}

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

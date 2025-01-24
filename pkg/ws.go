package syncinator

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/coder/websocket"
	"github.com/hiimjako/syncinator/internal/repository"
	"github.com/hiimjako/syncinator/pkg/diff"
	"github.com/hiimjako/syncinator/pkg/filestorage"
	"github.com/hiimjako/syncinator/pkg/middleware"
	"github.com/hiimjako/syncinator/pkg/mimeutils"
)

type MessageType = int

const (
	ChunkEventType  MessageType = iota
	CreateEventType MessageType = iota
	DeleteEventType MessageType = iota
	RenameEventType MessageType = iota
)

type WsMessageHeader struct {
	FileID int64       `json:"fileId"`
	Type   MessageType `json:"type"`
}

type EventMessage struct {
	WsMessageHeader
	WorkspacePath string `json:"workspacePath"`
	ObjectType    string `json:"objectType"`
}

type ChunkMessage struct {
	WsMessageHeader
	Chunks  []diff.Chunk `json:"chunks"`
	Version int64        `json:"version"`
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

	log.Printf("client %s (%d) connected\n", sub.clientID, sub.workspaceID)

	sub.Listen()

	return nil
}

func (s *syncinator) onEventMessage(sender *subscriber, event EventMessage) {
	s.broadcastMessage(sender, event)
}

func (s *syncinator) onChunkMessage(sender *subscriber, data ChunkMessage) {
	if len(data.Chunks) == 0 {
		log.Printf("0 chunks, skipping message. fileId: %v, version: %v\n", data.FileID, data.Version)
		return
	}

	s.mut.Lock()
	defer s.mut.Unlock()

	file, ok := s.files[data.FileID]
	if !ok {
		err := s.loadFileInCache(data.FileID)
		if err != nil {
			log.Printf("error while caching file %v: %v\n", data.FileID, err)
			return
		}
		file = s.files[data.FileID]
	}

	chunkToApply := data.Chunks

	// the incoming data was applied on older version
	// need to transform it
	if data.Version < file.Version {
		dbOperations, err := s.db.FetchFileOperationsFromVersion(s.ctx, repository.FetchFileOperationsFromVersionParams{
			FileID:      file.ID,
			Version:     data.Version,
			WorkspaceID: file.WorkspaceID,
		})
		if err != nil {
			log.Printf(
				"error while fetching operations, skipping message. fileId: %v, version: %v, err: %v\n",
				data.FileID,
				data.Version,
				err,
			)
			return
		}

		currVersion := data.Version
		for i := 0; i < len(dbOperations); i++ {
			if currVersion+1 != dbOperations[i].Version {
				log.Printf(
					"missing operation in history to transform, skipping message. fileId: %v, version: %v, err: %v\n",
					data.FileID,
					data.Version,
					err,
				)
				return
			}

			var previousChunk []diff.Chunk
			err := json.Unmarshal([]byte(dbOperations[i].Operation), &previousChunk)
			if err != nil {
				log.Printf(
					"error while parsing operations, skipping message. fileId: %v, version: %v, err: %v\n",
					data.FileID,
					data.Version,
					err,
				)
				return
			}

			chunkToApply = diff.TransformMultiple(previousChunk, chunkToApply)
			currVersion = dbOperations[i].Version
		}
	}

	file.Content = diff.ApplyMultiple(file.Content, chunkToApply)
	file.Version += 1
	file.pendingChanges += 1
	file.UpdatedAt = time.Now()

	msgToBroadcast := ChunkMessage{
		WsMessageHeader: data.WsMessageHeader,
		Chunks:          chunkToApply,
		Version:         file.Version,
	}

	committed := false
	tx, err := s.conn.Begin()
	if err != nil {
		log.Printf("error opening transaction. fileId: %v, version: %v, err: %v\n", data.FileID, data.Version, err)
		return
	}

	txq := s.db.WithTx(tx)
	defer func() {
		if committed {
			s.files[data.FileID] = file
		} else {
			//nolint
			tx.Rollback()
		}
	}()

	operation, err := json.Marshal(msgToBroadcast.Chunks)
	if err != nil {
		log.Printf("error while marshaling operation. fileId: %v, version: %v, err: %v\n", data.FileID, data.Version, err)
		return
	}
	err = txq.CreateOperation(s.ctx, repository.CreateOperationParams{
		FileID:    file.ID,
		Version:   file.Version,
		Operation: string(operation),
	})
	if err != nil {
		log.Printf("error while storing operation. fileId: %v, version: %v, err: %v\n", data.FileID, data.Version, err)
		return
	}

	err = txq.UpdateFileVersion(s.ctx, repository.UpdateFileVersionParams{
		ID:      file.ID,
		Version: file.Version,
	})
	if err != nil {
		log.Printf("error while updating version. fileId: %v, version: %v, err: %v\n", data.FileID, data.Version, err)
		return
	}

	s.broadcastMessage(sender, msgToBroadcast)

	if err := tx.Commit(); err != nil {
		log.Printf("failed to commit transaction: %v", err)
		return
	}
	committed = true
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
		// delete dead connections
		if !sub.IsConnected() {
			delete(s.subscribers, sub)
			continue
		}

		isSameWorkspace := sub.workspaceID == sender.workspaceID
		isSameClient := sub.clientID == sender.clientID

		if !isSameWorkspace {
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
			if isSameClient {
				continue
			}

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

// processFileChanges monitors file changes and persists them to storage based on
// two conditions:
// - When number of pending changes exceeds minChangesThreshold
// - When file hasn't been modified for flushInterval duration
// The function runs until context cancellation.
func (s *syncinator) processFileChanges() {
	ticker := time.NewTicker(s.flushInterval)

	f := func() {
		s.mut.Lock()
		for fileID, file := range s.files {
			if file.pendingChanges <= 0 {
				continue
			}

			if file.pendingChanges > s.minChangesThreshold ||
				time.Since(file.UpdatedAt) >= s.flushInterval {
				err := s.writeFileToStorage(file)
				if err != nil {
					log.Println(err)
				} else {
					file.pendingChanges = 0
					s.files[fileID] = file
				}
			}
		}
		s.mut.Unlock()
	}

	for {
		select {
		case <-ticker.C:
			f()
		case <-s.ctx.Done():
			f()
			return
		}
	}
}

// purgeCache is a routine to delete old cached items:
// - operation from "operations" table
// - files loaded in memory
func (s *syncinator) purgeCache() {
	ticker := time.NewTicker(10 * time.Minute)
	for {
		select {
		case <-ticker.C:
			// removing items from operation table
			err := s.db.DeleteOperationOlderThan(s.ctx, time.Now().Add(-s.cacheMaxAge))
			if err != nil {
				log.Println("error while removing old operations", err)
			}

			// removing items from files
			s.mut.Lock()
			for key, file := range s.files {
				if time.Since(file.UpdatedAt) >= s.cacheMaxAge {
					err := s.writeFileToStorage(file)
					if err != nil {
						log.Printf("error while writing file %d before purge: %v\n", file.ID, err)
					}
					delete(s.files, key)
				}
			}
			s.mut.Unlock()

		case <-s.ctx.Done():
			return
		}
	}
}

func (s *syncinator) WriteFileToStorage(fileID int64) error {
	s.mut.Lock()
	defer s.mut.Unlock()
	file, ok := s.files[fileID]
	if !ok {
		// not in cache, it means it is already up to date
		return nil
	}

	if file.pendingChanges <= 0 {
		return nil
	}

	err := s.writeFileToStorage(file)
	if err != nil {
		return err
	}

	file.pendingChanges = 0
	s.files[fileID] = file

	return nil
}

func (s *syncinator) writeFileToStorage(file CachedFile) error {
	if file.pendingChanges <= 0 {
		return nil
	}

	err := s.storage.WriteObject(file.DiskPath, strings.NewReader(file.Content))
	if err != nil {
		return err
	}

	fileReader := strings.NewReader(file.Content)
	hash := filestorage.GenerateHash(fileReader)

	err = s.db.UpdateFileHash(s.ctx, repository.UpdateFileHashParams{
		ID:   file.ID,
		Hash: hash,
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

// loadFileInCache caches the file from db
// is not thread safe
func (s *syncinator) loadFileInCache(fileID int64) error {
	file, err := s.db.FetchFile(s.ctx, fileID)
	if err != nil {
		return err
	}

	if !mimeutils.IsText(file.MimeType) {
		return fmt.Errorf("file %v is not a textfile", file.ID)
	}

	fileReader, err := s.storage.ReadObject(file.DiskPath)
	if err != nil {
		log.Panicf("error while reading file, %v\n", err)
	}
	defer fileReader.Close()

	fileContent, err := io.ReadAll(fileReader)
	if err != nil {
		log.Panicf("error while reading file, %v\n", err)
	}

	s.files[file.ID] = CachedFile{
		File:    file,
		Content: string(fileContent),
	}

	return nil
}

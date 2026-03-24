package syncinator

import (
	"context"
	"database/sql"
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
	ChunkEventType MessageType = iota
	CreateEventType
	DeleteEventType
	RenameEventType
	CursorEventType
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

type CursorMessage struct {
	WsMessageHeader
	ID    string  `json:"id,omitempty,omitzero"`
	Path  string  `json:"path"`
	Label string  `json:"label"`
	Color string  `json:"color"`
	Line  float64 `json:"line"`
	Ch    float64 `json:"ch"`
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
	sub, err := NewSubscriber(
		s.ctx, w, r,
		s.subscriberRateInterval, s.subscriberRateBurst,
		s.onChunkMessage, s.onEventMessage, s.onCursorMessage,
	)
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

func (s *syncinator) onCursorMessage(sender *subscriber, cursor CursorMessage) {
	s.broadcastMessage(sender, cursor)
}

func (s *syncinator) onChunkMessage(sender *subscriber, data ChunkMessage) {
	if len(data.Chunks) == 0 {
		log.Printf("0 chunks, skipping message. fileId: %v, version: %v\n", data.FileID, data.Version)
		return
	}

	if err := diff.ValidateChunks(data.Chunks); err != nil {
		log.Printf("invalid chunks, skipping message. fileId: %v, version: %v, err: %v\n", data.FileID, data.Version, err)
		return
	}

	file, ok := s.fileCache.Get(data.FileID)
	if !ok {
		var err error
		file, err = s.fetchAndCacheFile(data.FileID)
		if err != nil {
			log.Printf("error while caching file %v: %v\n", data.FileID, err)
			return
		}
	}

	file.mut.Lock()
	defer file.mut.Unlock()

	chunkToApply := data.Chunks

	// the incoming data was applied on older version
	// need to transform it
	if data.Version < file.Version {
		dbOperations, err := s.db.FetchFileOperationsFromVersion(s.ctx, repository.FetchFileOperationsFromVersionParams{

			FileID: file.ID,

			Version: data.Version,

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
		for i := range dbOperations {
			if currVersion+1 != dbOperations[i].Version {
				log.Printf(
					"missing operation in history to transform, skipping message. fileId: %v, version: %v\n",
					data.FileID,
					data.Version,
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

	newContent := diff.ApplyMultiple(file.Content, chunkToApply)
	newVersion := file.Version + 1

	msgToBroadcast := ChunkMessage{
		WsMessageHeader: data.WsMessageHeader,
		Chunks:          chunkToApply,
		Version:         newVersion,
	}

	committed := false
	tx, err := s.conn.BeginTx(s.ctx, nil)
	if err != nil {
		log.Printf("error opening transaction. fileId: %v, version: %v, err: %v\n", data.FileID, data.Version, err)
		return
	}

	txq := s.db.WithTx(tx)
	defer func() {
		if !committed {
			err := tx.Rollback()
			if err != nil {
				log.Printf("error rollbacking transaction. fileId: %v, version: %v, err: %v\n", data.FileID, data.Version, err)
			}
		}
	}()

	operation, err := json.Marshal(msgToBroadcast.Chunks)
	if err != nil {
		log.Printf("error while marshaling operation. fileId: %v, version: %v, err: %v\n", data.FileID, data.Version, err)
		return
	}
	err = txq.CreateOperation(s.ctx, repository.CreateOperationParams{
		FileID:    file.ID,
		Version:   newVersion,
		Operation: string(operation),
	})
	if err != nil {
		log.Printf("error while storing operation. fileId: %v, version: %v, err: %v\n", data.FileID, data.Version, err)
		return
	}

	err = txq.UpdateFileVersion(s.ctx, repository.UpdateFileVersionParams{
		ID:      file.ID,
		Version: newVersion,
	})
	if err != nil {
		log.Printf("error while updating version. fileId: %v, version: %v, err: %v\n", data.FileID, data.Version, err)
		return
	}

	if err := tx.Commit(); err != nil {
		log.Printf("failed to commit transaction: %v", err)
		return
	}
	committed = true

	// Apply in-memory changes only after successful commit
	file.Content = newContent
	file.Version = newVersion
	file.pendingChanges += 1
	file.UpdatedAt = time.Now()

	s.broadcastMessage(sender, msgToBroadcast)
}

func (s *syncinator) broadcastMessage(sender *subscriber, msg any) {
	if sender == nil {
		log.Println("broadcasting with nil sender")
		return
	}

	err := s.publishLimiter.Wait(s.ctx)
	if err != nil {
		log.Println(err)
	}

	s.subscribersMu.RLock()
	ws, ok := s.subscribers[sender.workspaceID]
	s.subscribersMu.RUnlock()

	if !ok {
		return
	}

	ws.mu.Lock()
	defer ws.mu.Unlock()

	for sub := range ws.subs {
		if !sub.IsConnected() {
			delete(ws.subs, sub)
			continue
		}

		isSameClient := sub.clientID == sender.clientID

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
		case CursorMessage:
			if isSameClient {
				continue
			}

			m.ID = sender.clientID

			select {
			case sub.cursorMsgQueue <- m:
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

	processFile := func(fileId int64) {
		file, ok := s.fileCache.Get(fileId)
		if !ok {
			return
		}

		file.mut.Lock()
		defer file.mut.Unlock()
		if file.pendingChanges <= 0 {
			return
		}

		if file.pendingChanges > s.minChangesThreshold ||
			time.Since(file.UpdatedAt) >= s.flushInterval {
			err := s.CreateFileSnapshot(file.CachedFile)
			if err != nil {
				log.Printf("error while creating snapshot: %v", err)
			}

			err = s.writeFileToStorage(file.CachedFile)
			if err != nil {
				log.Println(err)
			} else {
				file.pendingChanges = 0
			}
		}
	}

	processFiles := func() {
		for _, fileId := range s.fileCache.Keys() {
			processFile(fileId)
		}
	}

	for {
		select {
		case <-ticker.C:
			processFiles()
		case <-s.ctx.Done():
			processFiles()
			return
		}
	}
}

// purgeCache is a routine to delete old cached items:
// - operation from "operations" table
func (s *syncinator) purgeCache() {
	ticker := time.NewTicker(10 * time.Minute)
	for {
		select {
		case <-ticker.C:
			// removing items from operation table
			err := s.db.DeleteOperationOlderThan(s.ctx, time.Now().Add(-s.operationTTL))
			if err != nil {
				log.Println("error while removing old operations", err)
			}

		case <-s.ctx.Done():
			return
		}
	}
}

func (s *syncinator) WriteFileToStorage(fileID int64) error {
	file, ok := s.fileCache.Get(fileID)
	if !ok {
		// not in cache, it means it is already up to date
		return nil
	}

	file.mut.Lock()
	defer file.mut.Unlock()

	if file.pendingChanges <= 0 {
		return nil
	}

	err := s.writeFileToStorage(file.CachedFile)
	if err != nil {
		return err
	}

	file.pendingChanges = 0

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
	hash, err := filestorage.GenerateHash(fileReader)
	if err != nil {
		return err
	}

	err = s.db.UpdateFileHash(s.ctx, repository.UpdateFileHashParams{

		ID: file.ID,

		Hash: hash,
	})
	if err != nil {
		return err
	}

	return nil
}

func (s *syncinator) CreateFileSnapshot(file CachedFile) error {
	shouldCreateFullSnapshot := false
	var baseContent string

	latestSnapshot, err := s.db.FetchLatestSnapshotForFile(s.ctx, file.ID)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}

	if errors.Is(err, sql.ErrNoRows) {
		shouldCreateFullSnapshot = true
	} else {
		if file.Version%s.snapshotCheckpoint == 0 {
			shouldCreateFullSnapshot = true
		}

		if !shouldCreateFullSnapshot {
			snapshots, err := s.db.FetchSnapshots(s.ctx, repository.FetchSnapshotsParams{
				FileID:      file.ID,
				WorkspaceID: file.WorkspaceID,
			})
			if err != nil {
				return err
			}

			consecutiveDiffs := int64(0)
			for _, snap := range snapshots {
				if snap.Type == "diff" {
					consecutiveDiffs++
				} else {
					break
				}
			}

			if consecutiveDiffs >= s.maxSnapshotDiffChain {
				shouldCreateFullSnapshot = true
			}
		}
	}

	// Create full snapshot
	if shouldCreateFullSnapshot {
		reader := strings.NewReader(file.Content)
		diskPath, err := s.storage.CreateObject(reader)
		if err != nil {
			return err
		}

		_, err = reader.Seek(0, 0)
		if err != nil {
			return err
		}
		hash, err := filestorage.GenerateHash(reader)
		if err != nil {
			return err
		}

		err = s.db.CreateSnapshot(s.ctx, repository.CreateSnapshotParams{
			FileID:      file.ID,
			Version:     file.Version,
			DiskPath:    diskPath,
			Type:        "file",
			Hash:        hash,
			WorkspaceID: file.WorkspaceID,
		})
		return err
	}

	// Create diff snapshot
	// Need to get the content from the latest snapshot for comparison
	snapshotReader, err := s.storage.ReadObject(latestSnapshot.DiskPath)
	if err != nil {
		return err
	}
	defer snapshotReader.Close()

	snapshotContent, err := io.ReadAll(snapshotReader)
	if err != nil {
		return err
	}

	// If latest snapshot is a diff, we need to reconstruct the previous version
	if latestSnapshot.Type == "diff" {
		baseContent, err = s.ReconstructSnapshot(file.ID, latestSnapshot.Version, file.WorkspaceID)
		if err != nil {
			return err
		}
	} else {
		baseContent = string(snapshotContent)
	}

	// Compute diff between previous version and current version
	d := diff.Compute([]rune(baseContent), []rune(file.Content))

	diffJSON, err := json.Marshal(d)
	if err != nil {
		return err
	}

	reader := strings.NewReader(string(diffJSON))
	diskPath, err := s.storage.CreateObject(reader)
	if err != nil {
		return err
	}

	_, err = reader.Seek(0, 0)
	if err != nil {
		return err
	}
	hash, err := filestorage.GenerateHash(reader)
	if err != nil {
		return err
	}

	err = s.db.CreateSnapshot(s.ctx, repository.CreateSnapshotParams{
		FileID:      file.ID,
		Version:     file.Version,
		DiskPath:    diskPath,
		Type:        "diff",
		Hash:        hash,
		WorkspaceID: file.WorkspaceID,
	})

	return err
}

func (s *syncinator) ReconstructSnapshot(fileID, version, workspaceID int64) (string, error) {
	snapshots, err := s.db.FetchSnapshots(s.ctx, repository.FetchSnapshotsParams{
		FileID:      fileID,
		WorkspaceID: workspaceID,
	})
	if err != nil {
		return "", err
	}

	// Find the most recent full snapshot at or before the requested version
	// and collect all diffs that need to be applied
	var fullSnapshot repository.Snapshot
	diffSnapshots := make([]repository.Snapshot, 0)

	for _, snapshot := range snapshots {
		if snapshot.Version > version {
			continue
		}

		switch snapshot.Type {
		case "diff":
			// Collect diffs newer than the first full snapshot we encounter.
			// FetchSnapshots returns DESC order, so diffs for this version appear
			// before the base snapshot in the list.
			if fullSnapshot.DiskPath == "" {
				diffSnapshots = append(diffSnapshots, snapshot)
			}
		case "file":
			// First full snapshot at or before target version is the correct base.
			if fullSnapshot.DiskPath == "" {
				fullSnapshot = snapshot
			}
		}

		if fullSnapshot.DiskPath != "" {
			break
		}
	}

	// Verify we found a full snapshot
	if fullSnapshot.DiskPath == "" {
		return "", errors.New("no full snapshot found")
	}

	// Read the full snapshot content
	fileReader, err := s.storage.ReadObject(fullSnapshot.DiskPath)
	if err != nil {
		return "", err
	}
	defer fileReader.Close()

	fileContent, err := io.ReadAll(fileReader)
	if err != nil {
		return "", err
	}

	content := string(fileContent)

	// Apply diffs in chronological order (oldest to newest)
	// NOTE: diffSnapshots are in descending order, so we need to reverse
	for i := len(diffSnapshots) - 1; i >= 0; i-- {
		snapshot := diffSnapshots[i]

		diffReader, err := s.storage.ReadObject(snapshot.DiskPath)
		if err != nil {
			return "", err
		}

		diffContent, err := io.ReadAll(diffReader)
		diffReader.Close()
		if err != nil {
			return "", err
		}

		var d []diff.Chunk
		err = json.Unmarshal(diffContent, &d)
		if err != nil {
			return "", err
		}

		content = diff.ApplyMultiple(content, d)
	}

	return content, nil
}

func (s *syncinator) addSubscriber(sub *subscriber) {
	s.subscribersMu.Lock()
	ws, ok := s.subscribers[sub.workspaceID]
	if !ok {
		ws = &workspaceSubscribers{subs: make(map[*subscriber]struct{})}
		s.subscribers[sub.workspaceID] = ws
	}
	s.subscribersMu.Unlock()

	ws.mu.Lock()
	ws.subs[sub] = struct{}{}
	ws.mu.Unlock()
}

// deleteSubscriber deletes the given subscriber.
func (s *syncinator) deleteSubscriber(sub *subscriber) {
	s.subscribersMu.RLock()
	ws, ok := s.subscribers[sub.workspaceID]
	s.subscribersMu.RUnlock()

	if !ok {
		return
	}

	ws.mu.Lock()
	delete(ws.subs, sub)
	ws.mu.Unlock()
}

// fetchAndCacheFile caches the file from db
func (s *syncinator) fetchAndCacheFile(fileID int64) (*LockedCachedFile, error) {
	key := fmt.Sprintf("file-%d", fileID)
	res, err, _ := s.loader.Do(key, func() (interface{}, error) {
		file, err := s.db.FetchFile(s.ctx, fileID)
		if err != nil {
			return nil, err
		}

		if !mimeutils.IsText(file.MimeType) {
			return nil, fmt.Errorf("file %v is not a textfile", file.ID)
		}

		fileReader, err := s.storage.ReadObject(file.DiskPath)
		if err != nil {
			return nil, fmt.Errorf("error while reading file: %w", err)
		}
		defer fileReader.Close()

		fileContent, err := io.ReadAll(fileReader)
		if err != nil {
			return nil, fmt.Errorf("error while reading file content: %w", err)
		}

		cachedFile := &LockedCachedFile{
			CachedFile: CachedFile{

				File: file,

				Content: string(fileContent),
			},
		}
		s.fileCache.Add(file.ID, cachedFile)
		return cachedFile, nil
	})

	if err != nil {
		return nil, err
	}

	return res.(*LockedCachedFile), nil
}

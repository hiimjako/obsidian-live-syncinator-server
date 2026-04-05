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

	chunkToApply, err := transformStaleChunks(s.ctx, s.db, file, data.Version, data.Chunks)
	if err != nil {
		log.Printf("error transforming chunks. fileId: %v, version: %v, err: %v\n", data.FileID, data.Version, err)
		return
	}

	newContent := diff.ApplyMultiple(file.Content, chunkToApply)
	newVersion := file.Version + 1

	err = persistChunkOperation(s.ctx, s.conn, s.db, file.ID, newVersion, chunkToApply)
	if err != nil {
		log.Printf("error persisting operation. fileId: %v, version: %v, err: %v\n", data.FileID, data.Version, err)
		return
	}

	file.Content = newContent
	file.Version = newVersion
	file.pendingChanges += 1
	file.UpdatedAt = time.Now()

	s.broadcastMessage(sender, ChunkMessage{
		WsMessageHeader: data.WsMessageHeader,
		Chunks:          chunkToApply,
		Version:         newVersion,
	})
}

func transformStaleChunks(
	ctx context.Context,
	db *repository.Queries,
	file *LockedCachedFile,
	incomingVersion int64,
	incomingChunks []diff.Chunk,
) ([]diff.Chunk, error) {
	if incomingVersion >= file.Version {
		return incomingChunks, nil
	}

	dbOperations, err := db.FetchFileOperationsFromVersion(ctx, repository.FetchFileOperationsFromVersionParams{
		FileID:      file.ID,
		Version:     incomingVersion,
		WorkspaceID: file.WorkspaceID,
	})
	if err != nil {
		return nil, fmt.Errorf("fetching operations: %w", err)
	}

	transformed := incomingChunks
	currVersion := incomingVersion
	for i := range dbOperations {
		if currVersion+1 != dbOperations[i].Version {
			return nil, fmt.Errorf("missing operation in history at version %d", currVersion+1)
		}

		var previousChunk []diff.Chunk
		if err := json.Unmarshal([]byte(dbOperations[i].Operation), &previousChunk); err != nil {
			return nil, fmt.Errorf("parsing operation at version %d: %w", dbOperations[i].Version, err)
		}

		transformed = diff.TransformMultiple(previousChunk, transformed)
		currVersion = dbOperations[i].Version
	}

	return transformed, nil
}

func persistChunkOperation(
	ctx context.Context,
	conn *sql.DB,
	db *repository.Queries,
	fileID int64,
	newVersion int64,
	chunks []diff.Chunk,
) error {
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("opening transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	txq := db.WithTx(tx)

	operation, err := json.Marshal(chunks)
	if err != nil {
		return fmt.Errorf("marshaling operation: %w", err)
	}

	if err := txq.CreateOperation(ctx, repository.CreateOperationParams{
		FileID:    fileID,
		Version:   newVersion,
		Operation: string(operation),
	}); err != nil {
		return fmt.Errorf("storing operation: %w", err)
	}

	if err := txq.UpdateFileVersion(ctx, repository.UpdateFileVersionParams{
		ID:      fileID,
		Version: newVersion,
	}); err != nil {
		return fmt.Errorf("updating version: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("committing transaction: %w", err)
	}

	return nil
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

			err = s.flushFileToStorage(file.CachedFile)
			if err != nil {
				log.Println(err)
			} else {
				file.pendingChanges = 0
			}
		}
	}

	processFiles := func() {
		for _, fileID := range s.fileCache.Keys() {
			processFile(fileID)
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
	ticker := time.NewTicker(s.purgeCacheInterval)
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
		return nil
	}

	file.mut.Lock()
	defer file.mut.Unlock()

	if err := s.flushFileToStorage(file.CachedFile); err != nil {
		return err
	}

	file.pendingChanges = 0
	return nil
}

func (s *syncinator) flushFileToStorage(file CachedFile) error {
	if file.pendingChanges <= 0 {
		return nil
	}

	if err := s.storage.WriteObject(file.DiskPath, strings.NewReader(file.Content)); err != nil {
		return err
	}

	hash, err := filestorage.GenerateHash(strings.NewReader(file.Content))
	if err != nil {
		return err
	}

	return s.db.UpdateFileHash(s.ctx, repository.UpdateFileHashParams{
		ID:   file.ID,
		Hash: hash,
	})
}

func (s *syncinator) CreateFileSnapshot(file CachedFile) error {
	needsFull, latestSnapshot, err := s.shouldCreateFullSnapshot(file)
	if err != nil {
		return err
	}

	if needsFull {
		return s.createFullSnapshot(file)
	}
	return s.createDiffSnapshot(file, latestSnapshot)
}

func (s *syncinator) shouldCreateFullSnapshot(file CachedFile) (bool, repository.Snapshot, error) {
	latestSnapshot, err := s.db.FetchLatestSnapshotForFile(s.ctx, file.ID)
	if errors.Is(err, sql.ErrNoRows) {
		return true, repository.Snapshot{}, nil
	}
	if err != nil {
		return false, repository.Snapshot{}, err
	}

	if file.Version%s.snapshotCheckpoint == 0 {
		return true, latestSnapshot, nil
	}

	snapshots, err := s.db.FetchSnapshots(s.ctx, repository.FetchSnapshotsParams{
		FileID:      file.ID,
		WorkspaceID: file.WorkspaceID,
	})
	if err != nil {
		return false, repository.Snapshot{}, err
	}

	consecutiveDiffs := int64(0)
	for _, snap := range snapshots {
		if snap.Type == "diff" {
			consecutiveDiffs++
		} else {
			break
		}
	}

	return consecutiveDiffs >= s.maxSnapshotDiffChain, latestSnapshot, nil
}

func (s *syncinator) createFullSnapshot(file CachedFile) error {
	reader := strings.NewReader(file.Content)
	diskPath, err := s.storage.CreateObject(reader)
	if err != nil {
		return err
	}

	if _, err = reader.Seek(0, 0); err != nil {
		return err
	}
	hash, err := filestorage.GenerateHash(reader)
	if err != nil {
		return err
	}

	return s.db.CreateSnapshot(s.ctx, repository.CreateSnapshotParams{
		FileID:      file.ID,
		Version:     file.Version,
		DiskPath:    diskPath,
		Type:        "file",
		Hash:        hash,
		WorkspaceID: file.WorkspaceID,
	})
}

func (s *syncinator) createDiffSnapshot(file CachedFile, latestSnapshot repository.Snapshot) error {
	baseContent, err := s.resolveSnapshotBaseContent(latestSnapshot, file)
	if err != nil {
		return err
	}

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

	if _, err = reader.Seek(0, 0); err != nil {
		return err
	}
	hash, err := filestorage.GenerateHash(reader)
	if err != nil {
		return err
	}

	return s.db.CreateSnapshot(s.ctx, repository.CreateSnapshotParams{
		FileID:      file.ID,
		Version:     file.Version,
		DiskPath:    diskPath,
		Type:        "diff",
		Hash:        hash,
		WorkspaceID: file.WorkspaceID,
	})
}

func (s *syncinator) resolveSnapshotBaseContent(latestSnapshot repository.Snapshot, file CachedFile) (string, error) {
	if latestSnapshot.Type == "diff" {
		return s.ReconstructSnapshot(file.ID, latestSnapshot.Version, file.WorkspaceID)
	}

	snapshotReader, err := s.storage.ReadObject(latestSnapshot.DiskPath)
	if err != nil {
		return "", err
	}
	defer snapshotReader.Close()

	content, err := io.ReadAll(snapshotReader)
	if err != nil {
		return "", err
	}

	return string(content), nil
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

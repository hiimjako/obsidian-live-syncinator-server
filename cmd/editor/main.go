package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/gdamore/tcell/v2"
	"github.com/hiimjako/syncinator/internal/screen"
	rtsync "github.com/hiimjako/syncinator/pkg"
	"github.com/hiimjako/syncinator/pkg/diff"
)

var (
	fileId    = flag.Int("file", 1, "file to write")
	serverURL = flag.String("url", "127.0.0.1:8080", "server URL")
)

func main() {
	log.SetOutput(os.Stderr)
	flag.Parse()

	s, err := tcell.NewScreen()
	if err != nil {
		panic(err)
	}

	scr, err := screen.NewScreen(s)
	if err != nil {
		log.Fatalf("error while creating screen %v", err)
	}

	errc := make(chan error, 1)
	go func() {
		errc <- scr.Init()
	}()

	go pollText(&scr)

	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, os.Interrupt)
	select {
	case err := <-errc:
		log.Printf("failed to serve: %v", err)
	case sig := <-sigs:
		log.Printf("terminating: %v", sig)
	}
}

func pollText(s *screen.Screen) {
	var mu sync.Mutex

	ctx := context.Background()
	url := "ws://" + *serverURL + rtsync.PathWebSocket
	ws, _, err := websocket.Dial(ctx, url, nil)
	logOnError(err)

	lastContent := ""
	go func() {
		// listen for changes in ws
		for {
			var msg rtsync.ChunkMessage
			err = wsjson.Read(ctx, ws, &msg)
			logOnError(err)

			if msg.FileId != int64(*fileId) {
				continue
			}

			mu.Lock()
			lastContent = s.ApplyDiff(msg.Chunks)
			s.Render()
			mu.Unlock()
		}
	}()

	go func() {
		// send local changes to server
		for {
			<-time.After(1 * time.Millisecond)

			mu.Lock()
			content := s.Content()
			d := diff.ComputeDiff(lastContent, content)

			if len(d) == 0 {
				mu.Unlock()
				continue
			}

			err = wsjson.Write(ctx, ws, rtsync.ChunkMessage{
				WsMessageHeader: rtsync.WsMessageHeader{
					FileId: int64(*fileId),
				},
				Chunks: d,
			})
			logOnError(err)

			lastContent = content
			mu.Unlock()
		}
	}()
}

func logOnError(err error) {
	if err != nil {
		log.Println(err)
	}
}

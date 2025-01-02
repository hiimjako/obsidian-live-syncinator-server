package screen

import (
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/hiimjako/syncinator/syncinator/diff"
	"github.com/stretchr/testify/assert"
)

func TestScreen(t *testing.T) {
	s, err := NewScreen(tcell.NewSimulationScreen("UTF-8"))
	assert.NoError(t, err)

	time.Sleep(100 * time.Millisecond)

	//nolint:errcheck
	go s.Init()

	time.Sleep(10 * time.Millisecond)

	sendKey(t, s.screen, tcell.KeyRune, 'h')
	sendKey(t, s.screen, tcell.KeyRune, 'x')
	sendKey(t, s.screen, tcell.KeyRune, 'l')
	sendKey(t, s.screen, tcell.KeyRune, 'o')

	sendKey(t, s.screen, tcell.KeyLeft, ' ')
	sendKey(t, s.screen, tcell.KeyLeft, ' ')
	sendKey(t, s.screen, tcell.KeyBackspace, ' ')
	sendKey(t, s.screen, tcell.KeyRune, 'e')
	sendKey(t, s.screen, tcell.KeyRune, 'l')

	sendKey(t, s.screen, tcell.KeyRight, ' ')
	sendKey(t, s.screen, tcell.KeyRight, ' ')
	sendKey(t, s.screen, tcell.KeyRune, '!')

	assert.Contains(t, s.contentWithCursor(), "hello!|")
	assert.Equal(t, "hello!", s.Content())

	s.DeleteChunk(2, 2)
	assert.Contains(t, s.contentWithCursor(), "heo!|")
	assert.Equal(t, "heo!", s.Content())

	sendKey(t, s.screen, tcell.KeyLeft, ' ')
	sendKey(t, s.screen, tcell.KeyLeft, ' ')
	s.InsertChunk(2, "ll")
	assert.Contains(t, s.contentWithCursor(), "hell|o!")
	assert.Equal(t, "hello!", s.Content())

	updatedContent := s.ApplyDiff([]diff.Chunk{
		{
			Position: 5,
			Type:     diff.Add,
			Text:     " world",
			Len:      6,
		},
	})
	assert.Contains(t, s.contentWithCursor(), "hell|o world!")
	assert.Equal(t, "hello world!", s.Content())
	assert.Equal(t, "hello world!", updatedContent)
}

func sendKey(t testing.TB, s tcell.Screen, key tcell.Key, r rune) {
	assert.NoError(t, s.PostEvent(tcell.NewEventKey(key, r, tcell.ModNone)))
	time.Sleep(5 * time.Millisecond)
}

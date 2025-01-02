package diff

import (
	"github.com/sergi/go-diff/diffmatchpatch"
)

type Operation int8

const (
	Remove Operation = -1
	Add    Operation = 1
)

type Chunk struct {
	Type Operation `json:"type"`
	// Position indicates the position immediately after the last valid character, inclusive.
	Position int64  `json:"position"`
	Text     string `json:"text"`
	Len      int64  `json:"len"`
}

func Compute(oldText, newText string) []Chunk {
	var diffChunks []Chunk

	dmp := diffmatchpatch.New()

	var idx int64
	diffs := dmp.DiffMain(oldText, newText, true)
	for _, diff := range diffs {
		l := int64(len(diff.Text))
		switch diff.Type {
		case diffmatchpatch.DiffInsert:
			diffChunks = append(diffChunks, Chunk{
				Type:     Add,
				Position: idx,
				Text:     diff.Text,
				Len:      l,
			})
			idx += l
		case diffmatchpatch.DiffDelete:
			diffChunks = append(diffChunks, Chunk{
				Type:     Remove,
				Position: idx,
				Text:     diff.Text,
				Len:      l,
			})
		case diffmatchpatch.DiffEqual:
			idx += l
		}
	}

	return diffChunks
}

func Apply(text string, diff Chunk) string {
	textLen := int64(len(text))

	switch diff.Type {
	case Add:
		if diff.Position > textLen {
			return text + diff.Text
		}

		if diff.Position == 0 {
			return diff.Text + text
		}

		return text[:diff.Position] + diff.Text + text[diff.Position:]

	case Remove:
		if text == "" || diff.Position >= textLen {
			return text
		}

		endPosition := diff.Position + diff.Len
		if endPosition > textLen {
			endPosition = textLen
		}

		return text[:diff.Position] + text[endPosition:]
	}

	panic("not reachable")
}

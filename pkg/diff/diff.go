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

func Compute(oldText, newText []rune) []Chunk {
	var diffChunks []Chunk

	dmp := diffmatchpatch.New()

	var idx int64
	diffs := dmp.DiffMainRunes(oldText, newText, true)
	for _, diff := range diffs {
		l := int64(len([]rune(diff.Text)))
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

func ApplyMultiple(text string, chunks []Chunk) string {
	output := []rune(text)
	for i := range chunks {
		output = Apply(output, chunks[i])
	}

	return string(output)
}

func Apply(text []rune, chunk Chunk) []rune {
	textLen := int64(len(text))

	if chunk.Position < 0 {
		chunk.Position = 0
	}

	switch chunk.Type {
	case Add:
		if chunk.Position > textLen {
			return append(text, []rune(chunk.Text)...)
		}
		if chunk.Position == 0 {
			return append([]rune(chunk.Text), text...)
		}
		newText := make([]rune, 0, len(text)+len(chunk.Text))
		newText = append(newText, text[:chunk.Position]...)
		newText = append(newText, []rune(chunk.Text)...)
		newText = append(newText, text[chunk.Position:]...)
		return newText
	case Remove:
		if len(text) == 0 || chunk.Position >= textLen {
			return text
		}
		endPosition := min(chunk.Position+chunk.Len, textLen)
		newText := make([]rune, 0, len(text)-(int(endPosition)-int(chunk.Position)))
		newText = append(newText, text[:chunk.Position]...)
		newText = append(newText, text[endPosition:]...)
		return newText
	}

	panic("not reachable")
}

// Transform updates op2 based on op1
// op1 is the operation that has already been applied.
// op2 is the operation that needs to be transformed.
func Transform(op1, op2 Chunk) Chunk {
	if op1.Type == Add {
		if op2.Type == Add {
			// If op1 is inserted before op2, shift op2's position
			if op1.Position < op2.Position || (op1.Position == op2.Position) {
				op2.Position += op1.Len
			}
		} else {
			// If op1 is inserted before op2's start, shift op2's position
			if op1.Position <= op2.Position {
				op2.Position += op1.Len
			} else if op1.Position < op2.Position+op2.Len {
				// If op1 is inserted within op2's range, expand op2's length
				op2.Len += op1.Len
			}
		}
	} else {
		if op2.Type == Add {
			if op1.Position < op2.Position {
				shift := min(op1.Len, op2.Position-op1.Position)
				op2.Position -= shift
			}
		} else {
			// Calculate the new position of op2
			newOp2Position := op2.Position
			if op1.Position < op2.Position {
				newOp2Position -= min(op1.Len, op2.Position-op1.Position)
			}

			// Calculate the new length of op2
			newOp2Len := op2.Len
			overlapStart := max(op1.Position, op2.Position)
			overlapEnd := min(op1.Position+op1.Len, op2.Position+op2.Len)
			overlapLen := max(0, overlapEnd-overlapStart)

			newOp2Len -= overlapLen

			op2.Position = newOp2Position
			op2.Len = newOp2Len
		}
	}

	return op2
}

func TransformMultiple(lastOpList, opToTransformList []Chunk) []Chunk {
	transformedOps := make([]Chunk, len(opToTransformList))

	for i, op2 := range opToTransformList {
		transformedOp := op2

		for _, op1 := range lastOpList {
			transformedOp = Transform(op1, transformedOp)
		}

		transformedOps[i] = transformedOp
	}

	return transformedOps
}

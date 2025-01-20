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

func ApplyMultiple(text string, chunks []Chunk) string {
	output := text
	for i := 0; i < len(chunks); i++ {
		output = Apply(output, chunks[i])
	}

	return output
}

func Apply(text string, chunk Chunk) string {
	textLen := int64(len(text))

	switch chunk.Type {
	case Add:
		if chunk.Position > textLen {
			return text + chunk.Text
		}

		if chunk.Position == 0 {
			return chunk.Text + text
		}

		return text[:chunk.Position] + chunk.Text + text[chunk.Position:]

	case Remove:
		if text == "" || chunk.Position >= textLen {
			return text
		}

		endPosition := chunk.Position + chunk.Len
		if endPosition > textLen {
			endPosition = textLen
		}

		return text[:chunk.Position] + text[endPosition:]
	}

	panic("not reachable")
}

func Transform(lastOp, opToTransform Chunk) Chunk {
	transformed := opToTransform

	switch lastOp.Type {
	case Add:
		switch opToTransform.Type {
		case Add:
			if lastOp.Position <= opToTransform.Position {
				transformed.Position += lastOp.Len
			}
		case Remove:
			if lastOp.Position <= opToTransform.Position {
				transformed.Position += lastOp.Len
			}
		}
	case Remove:
		switch opToTransform.Type {
		case Add:
			if lastOp.Position < opToTransform.Position {
				transformed.Position -= min(lastOp.Len, opToTransform.Position-lastOp.Position)
			}
		case Remove:
			if lastOp.Position < opToTransform.Position+opToTransform.Len &&
				lastOp.Position+lastOp.Len > opToTransform.Position {
				startOverlap := max(lastOp.Position, opToTransform.Position)
				endOverlap := min(lastOp.Position+lastOp.Len, opToTransform.Position+opToTransform.Len)

				overlapStartInopToTransform := startOverlap - opToTransform.Position
				overlapEndInopToTransform := endOverlap - opToTransform.Position
				opToTransformText := opToTransform.Text[:overlapStartInopToTransform] +
					opToTransform.Text[overlapEndInopToTransform:]

				transformed.Position = min(opToTransform.Position, lastOp.Position)
				transformed.Len -= endOverlap - startOverlap
				transformed.Text = opToTransformText
			} else if lastOp.Position <= opToTransform.Position {
				transformed.Position -= lastOp.Len
			}
		}
	}

	return transformed
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

package diff

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestComputeDiff(t *testing.T) {
	tests := []struct {
		name     string
		text     string
		update   string
		expected []Chunk
	}{
		{
			name:   "compute remove chunk",
			text:   "hello world!",
			update: "hello!",
			expected: []Chunk{
				{
					Position: 5,
					Type:     Remove,
					Text:     " world",
					Len:      6,
				},
			},
		},
		{
			name:   "compute remove chunk 2",
			text:   " ",
			update: "",
			expected: []Chunk{
				{
					Position: 0,
					Type:     Remove,
					Text:     " ",
					Len:      1,
				},
			},
		},
		{
			name:   "compute add chunk",
			text:   "hello!",
			update: "hello world!",
			expected: []Chunk{
				{
					Position: 5,
					Type:     Add,
					Text:     " world",
					Len:      6,
				},
			},
		},
		{
			name:   "compute add chunk 2",
			text:   "h",
			update: "he",
			expected: []Chunk{
				{
					Position: 1,
					Type:     Add,
					Text:     "e",
					Len:      1,
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, Compute(tt.text, tt.update))
		})
	}
}

func TestApplyDiff(t *testing.T) {
	tests := []struct {
		name     string
		text     string
		diff     Chunk
		expected string
	}{
		{
			name:     "Add text at the beginning",
			text:     "Hello",
			diff:     Chunk{Type: Add, Position: 0, Text: "Hi "},
			expected: "Hi Hello",
		},
		{
			name:     "Add text in the middle",
			text:     "Hello",
			diff:     Chunk{Type: Add, Position: 3, Text: " there"},
			expected: "Hel therelo",
		},
		{
			name:     "Add text at the end",
			text:     "Hello",
			diff:     Chunk{Type: Add, Position: 5, Text: " World"},
			expected: "Hello World",
		},
		{
			name:     "Add text beyond the end",
			text:     "Hello",
			diff:     Chunk{Type: Add, Position: 10, Text: "!!!"},
			expected: "Hello!!!",
		},
		{
			name:     "Remove text from the beginning",
			text:     "Hello",
			diff:     Chunk{Type: Remove, Position: 0, Len: 3},
			expected: "lo",
		},
		{
			name:     "Remove text from the middle",
			text:     "Hello",
			diff:     Chunk{Type: Remove, Position: 1, Len: 3},
			expected: "Ho",
		},
		{
			name:     "Remove text from the end",
			text:     "Hello",
			diff:     Chunk{Type: Remove, Position: 2, Len: 3},
			expected: "He",
		},
		{
			name:     "Remove text beyond the end",
			text:     "Hello",
			diff:     Chunk{Type: Remove, Position: 3, Len: 10},
			expected: "Hel",
		},
		{
			name:     "Remove text when position is beyond text length",
			text:     "Hello",
			diff:     Chunk{Type: Remove, Position: 10, Len: 3},
			expected: "Hello",
		},
		{
			name:     "Remove no text (empty diff)",
			text:     "Hello",
			diff:     Chunk{Type: Remove, Position: 0, Len: 0},
			expected: "Hello",
		},
		{
			name:     "Add to empty string",
			text:     "",
			diff:     Chunk{Type: Add, Position: 0, Text: "Hello"},
			expected: "Hello",
		},
		{
			name:     "Remove from empty string",
			text:     "",
			diff:     Chunk{Type: Remove, Position: 0, Len: 3},
			expected: "",
		},
		{
			name:     "Add to empty string beyond length",
			text:     "",
			diff:     Chunk{Type: Add, Position: 10, Text: "Hello"},
			expected: "Hello",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := Apply(tt.text, tt.diff)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestTransform(t *testing.T) {
	tests := []struct {
		name     string
		op1      Chunk
		op2      Chunk
		expected Chunk
	}{
		{
			name:     "Insert before Insert",
			op1:      Chunk{Type: Add, Position: 3, Text: "abc", Len: 3},
			op2:      Chunk{Type: Add, Position: 5, Text: "xyz", Len: 3},
			expected: Chunk{Type: Add, Position: 8, Text: "xyz", Len: 3},
		},
		{
			name:     "Insert at same position",
			op1:      Chunk{Type: Add, Position: 5, Text: "abc", Len: 3},
			op2:      Chunk{Type: Add, Position: 5, Text: "xyz", Len: 3},
			expected: Chunk{Type: Add, Position: 8, Text: "xyz", Len: 3},
		},
		{
			name:     "Insert before Remove",
			op1:      Chunk{Type: Add, Position: 3, Text: "abc", Len: 3},
			op2:      Chunk{Type: Remove, Position: 5, Text: "xyz", Len: 3},
			expected: Chunk{Type: Remove, Position: 8, Text: "xyz", Len: 3},
		},
		{
			name:     "Remove before Insert",
			op1:      Chunk{Type: Remove, Position: 3, Text: "abc", Len: 3},
			op2:      Chunk{Type: Add, Position: 6, Text: "xyz", Len: 3},
			expected: Chunk{Type: Add, Position: 3, Text: "xyz", Len: 3},
		},
		{
			name:     "Remove overlapping Remove",
			op1:      Chunk{Type: Remove, Position: 3, Text: "bcd", Len: 3},
			op2:      Chunk{Type: Remove, Position: 2, Text: "abcd", Len: 4},
			expected: Chunk{Type: Remove, Position: 2, Text: "a", Len: 1},
		},
		{
			name:     "Remove non-overlapping Remove",
			op1:      Chunk{Type: Remove, Position: 3, Text: "abc", Len: 3},
			op2:      Chunk{Type: Remove, Position: 6, Text: "xyz", Len: 3},
			expected: Chunk{Type: Remove, Position: 3, Text: "xyz", Len: 3},
		},
		{
			name:     "Insert after Remove",
			op1:      Chunk{Type: Remove, Position: 3, Text: "abc", Len: 3},
			op2:      Chunk{Type: Add, Position: 6, Text: "xyz", Len: 3},
			expected: Chunk{Type: Add, Position: 3, Text: "xyz", Len: 3},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := Transform(tt.op1, tt.op2)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestTransformMultiple(t *testing.T) {
	tests := []struct {
		name     string
		opList1  []Chunk
		opList2  []Chunk
		expected []Chunk
		text     string
		result   string
	}{
		{
			name:   "Add and Remove interleaved",
			text:   "foo",
			result: "foobarbaz",
			opList1: []Chunk{
				{Type: Add, Position: 0, Text: "foo", Len: 3},
				{Type: Add, Position: 3, Text: "bar", Len: 3},
			},
			opList2: []Chunk{
				{Type: Remove, Position: 0, Text: "foo", Len: 3},
				{Type: Add, Position: 0, Text: "baz", Len: 3},
			},
			expected: []Chunk{
				{Type: Remove, Position: 6, Text: "foo", Len: 3},
				{Type: Add, Position: 6, Text: "baz", Len: 3},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := TransformMultiple(tt.opList1, tt.opList2)
			assert.Equal(t, tt.expected, result)

			rOp1 := ApplyMultiple(tt.text, tt.opList1)
			rOp2Transformed := ApplyMultiple(rOp1, result)
			assert.Equal(t, tt.result, rOp2Transformed)
		})
	}
}

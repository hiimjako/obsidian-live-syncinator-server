package diff

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestComputeDiff(t *testing.T) {
	tests := []struct {
		name     string
		text     []rune
		update   []rune
		expected []Chunk
	}{
		{
			name:   "compute remove chunk",
			text:   []rune("hello world!"),
			update: []rune("hello!"),
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
			text:   []rune(" "),
			update: []rune(""),
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
			text:   []rune("hello!"),
			update: []rune("hello world!"),
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
			text:   []rune("h"),
			update: []rune("he"),
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
		text     []rune
		diff     Chunk
		expected string
	}{
		{
			name:     "Add text at the beginning",
			text:     []rune("Hello"),
			diff:     Chunk{Type: Add, Position: 0, Text: "Hi "},
			expected: "Hi Hello",
		},
		{
			name:     "Add text in the middle",
			text:     []rune("Hello"),
			diff:     Chunk{Type: Add, Position: 3, Text: " there"},
			expected: "Hel therelo",
		},
		{
			name:     "Add text at the end",
			text:     []rune("Hello"),
			diff:     Chunk{Type: Add, Position: 5, Text: " World"},
			expected: "Hello World",
		},
		{
			name:     "Add text beyond the end",
			text:     []rune("Hello"),
			diff:     Chunk{Type: Add, Position: 10, Text: "!!!"},
			expected: "Hello!!!",
		},
		{
			name:     "Remove text from the beginning",
			text:     []rune("Hello"),
			diff:     Chunk{Type: Remove, Position: 0, Len: 3},
			expected: "lo",
		},
		{
			name:     "Remove text from the middle",
			text:     []rune("Hello"),
			diff:     Chunk{Type: Remove, Position: 1, Len: 3},
			expected: "Ho",
		},
		{
			name:     "Remove text from the end",
			text:     []rune("Hello"),
			diff:     Chunk{Type: Remove, Position: 2, Len: 3},
			expected: "He",
		},
		{
			name:     "Remove text beyond the end",
			text:     []rune("Hello"),
			diff:     Chunk{Type: Remove, Position: 3, Len: 10},
			expected: "Hel",
		},
		{
			name:     "Remove text when position is beyond text length",
			text:     []rune("Hello"),
			diff:     Chunk{Type: Remove, Position: 10, Len: 3},
			expected: "Hello",
		},
		{
			name:     "Remove no text (empty diff)",
			text:     []rune("Hello"),
			diff:     Chunk{Type: Remove, Position: 0, Len: 0},
			expected: "Hello",
		},
		{
			name:     "Add to empty string",
			text:     []rune(""),
			diff:     Chunk{Type: Add, Position: 0, Text: "Hello"},
			expected: "Hello",
		},
		{
			name:     "Remove from empty string",
			text:     []rune(""),
			diff:     Chunk{Type: Remove, Position: 0, Len: 3},
			expected: "",
		},
		{
			name:     "Add to empty string beyond length",
			text:     []rune(""),
			diff:     Chunk{Type: Add, Position: 10, Text: "Hello"},
			expected: "Hello",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := Apply(tt.text, tt.diff)
			assert.Equal(t, tt.expected, string(result))
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

func TestUTF16Handling(t *testing.T) {
	tests := []struct {
		name     string
		text     []rune
		diff     Chunk
		expected string
	}{
		{
			name:     "Add emoji in middle",
			text:     []rune("Hello world"),
			diff:     Chunk{Type: Add, Position: 5, Text: "👋", Len: 2},
			expected: "Hello👋 world",
		},
		{
			name:     "Remove emoji",
			text:     []rune("Hello 👋 world"),
			diff:     Chunk{Type: Remove, Position: 6, Len: 2},
			expected: "Hello world",
		},
		{
			name:     "Add multiple emojis",
			text:     []rune("Meeting:"),
			diff:     Chunk{Type: Add, Position: 8, Text: "👨‍👩‍👧‍👦 👨‍💻 🏃‍♂️", Len: 11},
			expected: "Meeting:👨‍👩‍👧‍👦 👨‍💻 🏃‍♂️",
		},
		{
			name:     "Add text with combining characters",
			text:     []rune("Resum"),
			diff:     Chunk{Type: Add, Position: 5, Text: "é", Len: 1},
			expected: "Resumé",
		},
		{
			name:     "Remove combining characters",
			text:     []rune("Resumé"),
			diff:     Chunk{Type: Remove, Position: 5, Len: 1},
			expected: "Resum",
		},
		{
			name:     "Add RTL text",
			text:     []rune("Hello"),
			diff:     Chunk{Type: Add, Position: 5, Text: " مرحبا", Len: 6},
			expected: "Hello مرحبا",
		},
		{
			name:     "Remove RTL text",
			text:     []rune("Hello مرحبا"),
			diff:     Chunk{Type: Remove, Position: 5, Len: 6},
			expected: "Hello",
		},
		{
			name:     "Add zero-width joiner sequence",
			text:     []rune("Family:"),
			diff:     Chunk{Type: Add, Position: 7, Text: "👨‍👩‍👧", Len: 5},
			expected: "Family:👨‍👩‍👧",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := Apply(tt.text, tt.diff)
			assert.Equal(t, tt.expected, string(result))
		})
	}
}

func TestUTF16ComputeDiff(t *testing.T) {
	tests := []struct {
		name     string
		text     []rune
		update   []rune
		expected []Chunk
	}{
		{
			name:   "compute diff with emoji",
			text:   []rune("Hello world"),
			update: []rune("Hello 👋 world"),
			expected: []Chunk{
				{
					Position: 6,
					Type:     Add,
					Text:     "👋 ",
					Len:      2,
				},
			},
		},
		{
			name:   "compute diff with combining characters",
			text:   []rune("Resume"),
			update: []rune("Resumé"),
			expected: []Chunk{
				{
					Position: 5,
					Type:     Remove,
					Text:     "e",
					Len:      1,
				},
				{
					Position: 5,
					Type:     Add,
					Text:     "é",
					Len:      1,
				},
			},
		},
		{
			name:   "compute diff with zero-width joiner sequence",
			text:   []rune("Emoji: 👩 👨 👧"),
			update: []rune("Emoji: 👨‍👩‍👶"),
			expected: []Chunk{
				{Type: -1, Position: 7, Text: "👩 ", Len: 2},
				{Type: -1, Position: 8, Text: " 👧", Len: 2},
				{Type: 1, Position: 8, Text: "\u200d👩\u200d👶", Len: 4},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := Compute(tt.text, tt.update)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestUTF16Transform(t *testing.T) {
	tests := []struct {
		name     string
		op1      Chunk
		op2      Chunk
		expected Chunk
	}{
		{
			name:     "Transform with emoji insertion",
			op1:      Chunk{Type: Add, Position: 3, Text: "👋", Len: 1},
			op2:      Chunk{Type: Add, Position: 5, Text: "world", Len: 5},
			expected: Chunk{Type: Add, Position: 6, Text: "world", Len: 5},
		},
		{
			name:     "Transform with combining characters",
			op1:      Chunk{Type: Add, Position: 3, Text: "é", Len: 1},
			op2:      Chunk{Type: Remove, Position: 4, Text: "test", Len: 4},
			expected: Chunk{Type: Remove, Position: 5, Text: "test", Len: 4},
		},
		{
			name:     "Transform with zero-width joiner sequence",
			op1:      Chunk{Type: Add, Position: 3, Text: "👨‍👩‍👧", Len: 5},
			op2:      Chunk{Type: Add, Position: 4, Text: "test", Len: 4},
			expected: Chunk{Type: Add, Position: 9, Text: "test", Len: 4},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := Transform(tt.op1, tt.op2)
			assert.Equal(t, tt.expected, result)
		})
	}
}

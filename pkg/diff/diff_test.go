package diff

import (
	"reflect"
	"testing"
)

func TestTransform(t *testing.T) {
	t.Run("insert vs insert", func(t *testing.T) {
		op1 := Chunk{Type: Add, Position: 5, Text: "hello", Len: int64(len([]rune("hello")))}
		op2 := Chunk{Type: Add, Position: 10, Text: "world", Len: int64(len([]rune("world")))}
		exp := Chunk{Type: Add, Position: 15, Text: "world", Len: int64(len([]rune("world")))}
		if got := Transform(op1, op2); !reflect.DeepEqual(got, exp) {
			t.Errorf("Transform() = %v, want %v", got, exp)
		}

		op1 = Chunk{Type: Add, Position: 10, Text: "world", Len: int64(len([]rune("world")))}
		op2 = Chunk{Type: Add, Position: 5, Text: "hello", Len: int64(len([]rune("hello")))}
		exp = Chunk{Type: Add, Position: 5, Text: "hello", Len: int64(len([]rune("hello")))}
		if got := Transform(op1, op2); !reflect.DeepEqual(got, exp) {
			t.Errorf("Transform() = %v, want %v", got, exp)
		}

		op1 = Chunk{Type: Add, Position: 5, Text: "hello", Len: int64(len([]rune("hello")))}
		op2 = Chunk{Type: Add, Position: 5, Text: "world", Len: int64(len([]rune("world")))}
		exp = Chunk{Type: Add, Position: 10, Text: "world", Len: int64(len([]rune("world")))}
		if got := Transform(op1, op2); !reflect.DeepEqual(got, exp) {
			t.Errorf("Transform() = %v, want %v", got, exp)
		}
	})

	t.Run("delete vs insert", func(t *testing.T) {
		op1 := Chunk{Type: Remove, Position: 5, Len: 5}
		op2 := Chunk{Type: Add, Position: 10, Text: "hello", Len: int64(len([]rune("hello")))}
		exp := Chunk{Type: Add, Position: 5, Text: "hello", Len: int64(len([]rune("hello")))}
		if got := Transform(op1, op2); !reflect.DeepEqual(got, exp) {
			t.Errorf("Transform() = %v, want %v", got, exp)
		}

		op1 = Chunk{Type: Remove, Position: 10, Len: 5}
		op2 = Chunk{Type: Add, Position: 5, Text: "hello", Len: int64(len([]rune("hello")))}
		exp = Chunk{Type: Add, Position: 5, Text: "hello", Len: int64(len([]rune("hello")))}
		if got := Transform(op1, op2); !reflect.DeepEqual(got, exp) {
			t.Errorf("Transform() = %v, want %v", got, exp)
		}

		op1 = Chunk{Type: Remove, Position: 5, Len: 10}
		op2 = Chunk{Type: Add, Position: 10, Text: "hello", Len: int64(len([]rune("hello")))}
		exp = Chunk{Type: Add, Position: 5, Text: "hello", Len: int64(len([]rune("hello")))}
		if got := Transform(op1, op2); !reflect.DeepEqual(got, exp) {
			t.Errorf("Transform() = %v, want %v", got, exp)
		}
	})

	t.Run("insert vs delete", func(t *testing.T) {
		op1 := Chunk{Type: Add, Position: 5, Text: "hello", Len: int64(len([]rune("hello")))}
		op2 := Chunk{Type: Remove, Position: 10, Len: 5}
		exp := Chunk{Type: Remove, Position: 15, Len: 5}
		if got := Transform(op1, op2); !reflect.DeepEqual(got, exp) {
			t.Errorf("Transform() = %v, want %v", got, exp)
		}

		op1 = Chunk{Type: Add, Position: 10, Text: "hello", Len: int64(len([]rune("hello")))}
		op2 = Chunk{Type: Remove, Position: 5, Len: 5}
		exp = Chunk{Type: Remove, Position: 5, Len: 5}
		if got := Transform(op1, op2); !reflect.DeepEqual(got, exp) {
			t.Errorf("Transform() = %v, want %v", got, exp)
		}

		op1 = Chunk{Type: Add, Position: 5, Text: "helloworld", Len: int64(len([]rune("helloworld")))}
		op2 = Chunk{Type: Remove, Position: 7, Len: 3}
		exp = Chunk{Type: Remove, Position: 17, Len: 3}
		if got := Transform(op1, op2); !reflect.DeepEqual(got, exp) {
			t.Errorf("Transform() = %v, want %v", got, exp)
		}
	})

	t.Run("delete vs delete", func(t *testing.T) {
		op1 := Chunk{Type: Remove, Position: 5, Len: 5}
		op2 := Chunk{Type: Remove, Position: 10, Len: 5}
		exp := Chunk{Type: Remove, Position: 5, Len: 5}
		if got := Transform(op1, op2); !reflect.DeepEqual(got, exp) {
			t.Errorf("Transform() = %v, want %v", got, exp)
		}

		op1 = Chunk{Type: Remove, Position: 10, Len: 5}
		op2 = Chunk{Type: Remove, Position: 5, Len: 5}
		exp = Chunk{Type: Remove, Position: 5, Len: 5}
		if got := Transform(op1, op2); !reflect.DeepEqual(got, exp) {
			t.Errorf("Transform() = %v, want %v", got, exp)
		}

		op1 = Chunk{Type: Remove, Position: 5, Len: 10}
		op2 = Chunk{Type: Remove, Position: 7, Len: 3}
		exp = Chunk{Type: Remove, Position: 5, Len: 0}
		if got := Transform(op1, op2); !reflect.DeepEqual(got, exp) {
			t.Errorf("Transform() = %v, want %v", got, exp)
		}

		op1 = Chunk{Type: Remove, Position: 7, Len: 3}
		op2 = Chunk{Type: Remove, Position: 5, Len: 10}
		exp = Chunk{Type: Remove, Position: 5, Len: 7}
		if got := Transform(op1, op2); !reflect.DeepEqual(got, exp) {
			t.Errorf("Transform() = %v, want %v", got, exp)
		}
	})
}

func TestTransform_UTF16(t *testing.T) {
	t.Run("insert vs insert with emojis", func(t *testing.T) {
		op1 := Chunk{Type: Add, Position: 2, Text: "👋", Len: int64(len([]rune("👋")))}
		op2 := Chunk{Type: Add, Position: 5, Text: "🎉", Len: int64(len([]rune("🎉")))}
		exp := Chunk{Type: Add, Position: 6, Text: "🎉", Len: int64(len([]rune("🎉")))}
		if got := Transform(op1, op2); !reflect.DeepEqual(got, exp) {
			t.Errorf("Transform() = %v, want %v", got, exp)
		}
	})

	t.Run("delete vs insert with emojis", func(t *testing.T) {
		op1 := Chunk{Type: Remove, Position: 1, Len: 2} // remove 2 characters
		op2 := Chunk{Type: Add, Position: 4, Text: "🚀", Len: int64(len([]rune("🚀")))}
		exp := Chunk{Type: Add, Position: 2, Text: "🚀", Len: int64(len([]rune("🚀")))}
		if got := Transform(op1, op2); !reflect.DeepEqual(got, exp) {
			t.Errorf("Transform() = %v, want %v", got, exp)
		}
	})

	t.Run("insert vs delete with emojis", func(t *testing.T) {
		op1 := Chunk{Type: Add, Position: 1, Text: "🎈", Len: int64(len([]rune("🎈")))}
		op2 := Chunk{Type: Remove, Position: 3, Len: 2}
		exp := Chunk{Type: Remove, Position: 4, Len: 2}
		if got := Transform(op1, op2); !reflect.DeepEqual(got, exp) {
			t.Errorf("Transform() = %v, want %v", got, exp)
		}
	})

	t.Run("delete vs delete with emojis", func(t *testing.T) {
		op1 := Chunk{Type: Remove, Position: 1, Len: 2}
		op2 := Chunk{Type: Remove, Position: 4, Len: 3}
		exp := Chunk{Type: Remove, Position: 2, Len: 3}
		if got := Transform(op1, op2); !reflect.DeepEqual(got, exp) {
			t.Errorf("Transform() = %v, want %v", got, exp)
		}
	})
}

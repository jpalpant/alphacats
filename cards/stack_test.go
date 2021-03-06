package cards

import (
	"testing"
)

func TestNewStackFromCards(t *testing.T) {
	testCards := []Card{Unknown, Unknown, Skip, Shuffle, SeeTheFuture, SeeTheFuture}
	stack := NewStackFromCards(testCards)
	for i, card := range testCards {
		if stack.NthCard(i) != card {
			t.Errorf("card pile position %d has %v, expected %v", i, stack.NthCard(i), card)
		}
	}
}

func TestIsEmpty(t *testing.T) {
	testCards := []Card{Unknown, Unknown, Skip, Shuffle, SeeTheFuture, SeeTheFuture}
	stack := NewStackFromCards(testCards)

	if stack.IsEmpty() {
		t.Error("stack with known cards should not be empty")
	}

	stack = NewStack()
	if !stack.IsEmpty() {
		t.Error("stack with no cards should be empty")
	}
}

func TestStackLen(t *testing.T) {
	testCases := []struct {
		cards    []Card
		expected int
	}{
		{[]Card{Skip, Shuffle, SeeTheFuture}, 3},
		// Unknowns on the end of the stack are not distinguishable.
		{[]Card{Skip, Shuffle, SeeTheFuture, Unknown, Unknown}, 3},
		// But at the beginning and in the middle they are.
		{[]Card{Unknown, Skip, Shuffle, SeeTheFuture, Unknown, Unknown}, 4},
		{[]Card{Skip, Shuffle, Unknown, SeeTheFuture, Unknown, Unknown}, 4},
	}

	for _, tc := range testCases {
		stack := NewStackFromCards(tc.cards)
		if stack.Len() != tc.expected {
			t.Errorf("stack: %v, expected: %v, got: %v", stack, tc.expected, stack.Len())
		}
	}
}

func TestSetNthCard(t *testing.T) {
	testCards := []Card{Unknown, Unknown, Skip, Shuffle, SeeTheFuture, SeeTheFuture}
	stack := NewStackFromCards(testCards)
	stack.SetNthCard(1, Slap1x)
	testCards[1] = Slap1x
	for i, card := range testCards {
		if stack.NthCard(i) != card {
			t.Errorf("card pile position %d has %v, expected %v", i, stack.NthCard(i), card)
		}
	}

	if stack.NthCard(10) != Unknown {
		t.Errorf("card pile position %d has %v, expected %v", 10, stack.NthCard(10), Unknown)
	}
	stack.SetNthCard(10, Slap2x)
	if stack.NthCard(10) != Slap2x {
		t.Errorf("card pile position %d has %v, expected %v", 10, stack.NthCard(10), Slap2x)
	}

	// All other positions should remain the same.
	for i, card := range testCards {
		if stack.NthCard(i) != card {
			t.Errorf("card pile position %d has %v, expected %v", i, stack.NthCard(i), card)
		}
	}
}

func BenchmarkSetNthCard(b *testing.B) {
	testCards := []Card{Unknown, Unknown, Skip, Shuffle, SeeTheFuture, SeeTheFuture}
	stack := NewStackFromCards(testCards)
	for i := 0; i < b.N; i++ {
		stack.SetNthCard(4, Skip)
	}
}

func TestSetNthCard_NoOp(t *testing.T) {
	testCards := []Card{Slap2x, Slap1x, ExplodingKitten}
	stack := NewStackFromCards(testCards)
	for i, card := range testCards {
		if stack.NthCard(i) != card {
			t.Errorf("got %v, expected %v", stack.NthCard(i), card)
		}
	}

	// Should be a no-op since these are already set.
	for i, card := range testCards {
		stack.SetNthCard(i, card)
	}

	for i, card := range testCards {
		if stack.NthCard(i) != card {
			t.Errorf("got %v, expected %v", stack.NthCard(i), card)
		}
	}
}

func TestRemoveCard(t *testing.T) {
	testCards := []Card{Unknown, Unknown, Skip, Shuffle, SeeTheFuture, SeeTheFuture}
	stack := NewStackFromCards(testCards)
	stack.RemoveCard(0)
	testCards = testCards[1:]
	for i, card := range testCards {
		if stack.NthCard(i) != card {
			t.Errorf("card pile position %d has %v, expected %v", i, stack.NthCard(i), card)
			t.Logf("%v, expected %v", stack, testCards)
		}
	}

	stack.RemoveCard(2)
	testCards = append(testCards[:2], testCards[3:]...)
	for i, card := range testCards {
		if stack.NthCard(i) != card {
			t.Errorf("card pile position %d has %v, expected %v", i, stack.NthCard(i), card)
		}
	}
}

func BenchmarkInsertRemoveCard(b *testing.B) {
	testCards := []Card{Unknown, Unknown, Skip, Shuffle, SeeTheFuture, SeeTheFuture}
	stack := NewStackFromCards(testCards)
	for i := 0; i < b.N; i++ {
		stack.InsertCard(Skip, 2)
		stack.RemoveCard(2)
	}
}

func TestInsertCard(t *testing.T) {
	testCards := []Card{Unknown, Unknown, Skip, Shuffle, SeeTheFuture, SeeTheFuture}
	stack := NewStackFromCards(testCards)
	stack.InsertCard(Slap1x, 0)
	testCards = append([]Card{Slap1x}, testCards...)
	for i, card := range testCards {
		if stack.NthCard(i) != card {
			t.Errorf("card pile position %d has %v, expected %v", i, stack.NthCard(i), card)
			t.Logf("%v, expected %v", stack, testCards)
		}
	}

	stack.InsertCard(Slap2x, 2)
	remainder := append([]Card{Slap2x}, testCards[2:]...)
	testCards = append(testCards[:2], remainder...)
	for i, card := range testCards {
		if stack.NthCard(i) != card {
			t.Errorf("card pile position %d has %v, expected %v", i, stack.NthCard(i), card)
			t.Logf("%v, expected %v", stack, testCards)
		}
	}
}

func TestStackToSet(t *testing.T) {
	testCases := [][]Card{
		{Skip, Shuffle, SeeTheFuture, Cat},
		{Unknown, Skip, Shuffle, SeeTheFuture, Cat},
		{Skip, Unknown, Unknown, Shuffle, SeeTheFuture, Cat},
	}

	for _, tc := range testCases {
		stack := NewStackFromCards(tc)
		set := stack.ToSet()
		result := set.AsSlice()
		if !setEqual(tc, result) {
			t.Errorf("input = %v, output = %v", tc, result)
		}
	}
}

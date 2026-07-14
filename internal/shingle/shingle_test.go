// Tests for shingle hashing: determinism, window mechanics and the
// separator that keeps token boundaries out of the collision surface.
package shingle

import "testing"

func TestHashBasicProperties(t *testing.T) {
	a := Hash([]string{"the", "quick", "fox"})
	b := Hash([]string{"the", "quick", "fox"})
	if a != b {
		t.Fatalf("same input hashed differently: %d vs %d", a, b)
	}
	if Hash([]string{"a", "b"}) == Hash([]string{"a", "c"}) {
		t.Fatal("distinct token sequences collided")
	}
	if Hash([]string{"a", "b"}) == Hash([]string{"b", "a"}) {
		t.Fatal("order-swapped sequences collided")
	}
}

func TestSeparatorPreventsConcatenationCollision(t *testing.T) {
	// Without a separator, ["ab","c"] and ["a","bc"] would hash the same
	// byte stream — exactly the false positive a checker cannot afford.
	if Hash([]string{"ab", "c"}) == Hash([]string{"a", "bc"}) {
		t.Fatal("boundary-shifted sequences collided")
	}
}

func TestHashesWindowCount(t *testing.T) {
	toks := []string{"a", "b", "c", "d", "e"}
	if got := len(Hashes(toks, 3)); got != 3 {
		t.Fatalf("want 3 windows, got %d", got)
	}
}

func TestHashesWindowsMatchDirectHash(t *testing.T) {
	toks := []string{"a", "b", "c", "d"}
	hs := Hashes(toks, 2)
	for i := range hs {
		if want := Hash(toks[i : i+2]); hs[i] != want {
			t.Fatalf("window %d: got %d, want %d", i, hs[i], want)
		}
	}
}

func TestHashesExactlyOneWindow(t *testing.T) {
	toks := []string{"a", "b", "c"}
	hs := Hashes(toks, 3)
	if len(hs) != 1 || hs[0] != Hash(toks) {
		t.Fatalf("got %v", hs)
	}
}

func TestHashesDocumentShorterThanWindow(t *testing.T) {
	if got := Hashes([]string{"a", "b"}, 3); got != nil {
		t.Fatalf("want nil for short doc, got %v", got)
	}
}

func TestHashesRejectsNonPositiveN(t *testing.T) {
	if Hashes([]string{"a"}, 0) != nil || Hashes([]string{"a"}, -1) != nil {
		t.Fatal("non-positive n must produce no windows")
	}
}

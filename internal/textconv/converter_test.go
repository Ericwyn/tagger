package textconv

import "testing"

func TestSimplifyPreservesNonChineseText(t *testing.T) {
	input := "Easy On Me [Live] 2024"
	if got := Simplify(input); got != input {
		t.Fatalf("Simplify(%q) = %q", input, got)
	}
}

func TestSimplifyConvertsTraditionalChinese(t *testing.T) {
	input := "許嵩／想見你"
	want := "许嵩／想见你"
	if got := Simplify(input); got != want {
		t.Fatalf("Simplify(%q) = %q, want %q", input, got, want)
	}
}

func TestSimplifyAllDoesNotMutateInput(t *testing.T) {
	input := []string{"許嵩", "Easy On Me"}
	got := SimplifyAll(input)
	if got[0] != "许嵩" || got[1] != input[1] {
		t.Fatalf("SimplifyAll = %#v", got)
	}
	if input[0] != "許嵩" {
		t.Fatalf("input was mutated: %#v", input)
	}
}

package providers

import "testing"

func TestSimplifyCandidateConvertsHumanTextWithoutMutatingSlices(t *testing.T) {
	input := Candidate{
		Title: "想見你", Artists: []string{"許嵩", "Adele"}, Album: "專輯",
		AlbumArtists: []string{"許嵩"}, Genres: []string{"流行音樂"},
		Lyrics: "[00:01.00] 想見你", SyncedLyrics: "[00:01.00] 想見你",
		AlternateTitles: []string{"想見你 (現場)"}, Comment: "現場版",
	}
	got := SimplifyCandidate(input)
	if got.Title != "想见你" || got.Artists[0] != "许嵩" || got.Artists[1] != "Adele" || got.Album != "专辑" || got.AlbumArtists[0] != "许嵩" || got.Genres[0] != "流行音乐" || got.Lyrics != "[00:01.00] 想见你" || got.AlternateTitles[0] != "想见你 (现场)" || got.Comment != "现场版" {
		t.Fatalf("simplified candidate = %#v", got)
	}
	if input.Title != "想見你" || input.Artists[0] != "許嵩" || input.AlternateTitles[0] != "想見你 (現場)" {
		t.Fatalf("input candidate was mutated = %#v", input)
	}
}

func TestParseSimplifyChinese(t *testing.T) {
	if value, err := ParseSimplifyChinese("true"); err != nil || !value {
		t.Fatalf("parse true = %v, %v", value, err)
	}
	if _, err := ParseSimplifyChinese("maybe"); err == nil {
		t.Fatal("invalid simplifyChinese value was accepted")
	}
}

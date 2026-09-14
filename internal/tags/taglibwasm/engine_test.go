package taglibwasm

import (
	"bytes"
	"context"
	"encoding/base64"
	"os"
	"path/filepath"
	"testing"
)

const tinyOggVorbisBase64 = "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"

const tinyOggOpusBase64 = "T2dnUwACAAAAAAAAAAB4nsf8AAAAAD4MprgBE09wdXNIZWFkAQE4AUAfAAAAAABPZ2dTAAAAAAAAAAAAAHiex/wBAAAA48YeOgE/T3B1c1RhZ3MNAAAATGF2ZjU4Ljc2LjEwMAEAAAAeAAAAZW5jb2Rlcj1MYXZjNTguMTM0LjEwMCBsaWJvcHVzT2dnUwAE+BMAAAAAAAB4nsf8AgAAADjqh3oGBwYGBgYGCAvkuaC8hAgHxrMOxggHxrMOxggHxrMOxggHxrMOxggHxrMOxg=="

const onePixelPNGBase64 = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII="

func TestEngineRoundTripsOggVorbisAndOpus(t *testing.T) {
	tests := []struct {
		name      string
		extension string
		encoded   string
		codec     string
	}{
		{name: "vorbis", extension: ".ogg", encoded: tinyOggVorbisBase64, codec: "VORBIS"},
		{name: "opus", extension: ".opus", encoded: tinyOggOpusBase64, codec: "OPUS"},
	}
	cover, err := base64.StdEncoding.DecodeString(onePixelPNGBase64)
	if err != nil {
		t.Fatal(err)
	}
	engine := New()
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			audio, err := base64.StdEncoding.DecodeString(test.encoded)
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(t.TempDir(), "fixture"+test.extension)
			if err := os.WriteFile(path, audio, 0o600); err != nil {
				t.Fatal(err)
			}
			before, err := engine.Read(context.Background(), path)
			if err != nil {
				t.Fatal(err)
			}
			if before.Properties.Container != "OGG" || before.Properties.Codec != test.codec {
				t.Fatalf("properties = %#v", before.Properties)
			}
			if err := engine.Write(context.Background(), path, map[string][]string{
				"TITLE":  {"OGG 测试"},
				"ARTIST": {"甲", "乙"},
			}); err != nil {
				t.Fatal(err)
			}
			afterTags, err := engine.Read(context.Background(), path)
			if err != nil || len(afterTags.Raw["TITLE"]) != 1 || afterTags.Raw["TITLE"][0] != "OGG 测试" || len(afterTags.Raw["ARTIST"]) != 2 {
				t.Fatalf("tag round trip = %#v, %v", afterTags.Raw, err)
			}
			if err := engine.Write(context.Background(), path, map[string][]string{"TITLE": nil}); err != nil {
				t.Fatal(err)
			}
			afterDelete, err := engine.Read(context.Background(), path)
			if err != nil || len(afterDelete.Raw["TITLE"]) != 0 || len(afterDelete.Raw["ARTIST"]) != 2 {
				t.Fatalf("tag delete = %#v, %v", afterDelete.Raw, err)
			}
			if err := engine.WriteArtwork(context.Background(), path, 0, cover, "image/png"); err != nil {
				t.Fatal(err)
			}
			gotCover, err := engine.ReadArtwork(context.Background(), path, 0)
			if err != nil || !bytes.Equal(gotCover, cover) {
				t.Fatalf("artwork round trip: bytes=%d err=%v", len(gotCover), err)
			}
			afterArtwork, err := engine.Read(context.Background(), path)
			if err != nil || afterArtwork.ArtworkCount != 1 {
				t.Fatalf("artwork properties = %#v, %v", afterArtwork, err)
			}
			if err := engine.WriteArtwork(context.Background(), path, 0, nil, ""); err != nil {
				t.Fatal(err)
			}
			afterArtworkDelete, err := engine.Read(context.Background(), path)
			if err != nil || afterArtworkDelete.ArtworkCount != 0 {
				t.Fatalf("artwork delete = %#v, %v", afterArtworkDelete, err)
			}
		})
	}
}

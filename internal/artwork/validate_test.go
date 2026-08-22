package artwork

import (
	"bytes"
	"errors"
	"image"
	"image/jpeg"
	"image/png"
	"testing"
)

func TestValidateJPEGAndPNG(t *testing.T) {
	for _, fixture := range []struct {
		name, mime string
		encode     func(*bytes.Buffer) error
	}{
		{name: "jpeg", mime: "image/jpeg", encode: func(output *bytes.Buffer) error {
			return jpeg.Encode(output, image.NewRGBA(image.Rect(0, 0, 3, 2)), nil)
		}},
		{name: "png", mime: "image/png", encode: func(output *bytes.Buffer) error {
			return png.Encode(output, image.NewRGBA(image.Rect(0, 0, 2, 3)))
		}},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			var data bytes.Buffer
			if err := fixture.encode(&data); err != nil {
				t.Fatal(err)
			}
			asset, err := Validate(data.Bytes(), fixture.mime+"; charset=binary")
			if err != nil {
				t.Fatal(err)
			}
			if asset.MIME != fixture.mime || asset.Width <= 0 || asset.Height <= 0 || len(asset.Hash) != 64 {
				t.Fatalf("asset = %#v", asset)
			}
		})
	}
}

func TestValidateAcceptsCommonCDNMIMEAliases(t *testing.T) {
	var data bytes.Buffer
	if err := jpeg.Encode(&data, image.NewRGBA(image.Rect(0, 0, 2, 2)), nil); err != nil {
		t.Fatal(err)
	}
	for _, declared := range []string{"image/jpg", "image/pjpeg", "image/jpeg; charset=binary"} {
		asset, err := Validate(data.Bytes(), declared)
		if err != nil || asset.MIME != "image/jpeg" {
			t.Fatalf("declared %q: asset=%#v err=%v", declared, asset, err)
		}
	}
}

func TestValidateRejectsMIMEConfusionAndInvalidData(t *testing.T) {
	var data bytes.Buffer
	if err := png.Encode(&data, image.NewRGBA(image.Rect(0, 0, 1, 1))); err != nil {
		t.Fatal(err)
	}
	if _, err := Validate(data.Bytes(), "image/jpeg"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("MIME mismatch error = %v", err)
	}
	if _, err := Validate([]byte("not an image"), "application/octet-stream"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("invalid image error = %v", err)
	}
	if _, err := Validate(make([]byte, MaxBytes+1), "image/jpeg"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("oversized image error = %v", err)
	}
}

func TestResizeSquareCenterCropsAndScales(t *testing.T) {
	var data bytes.Buffer
	if err := png.Encode(&data, image.NewRGBA(image.Rect(0, 0, 8, 4))); err != nil {
		t.Fatal(err)
	}
	original, err := Validate(data.Bytes(), "image/png")
	if err != nil {
		t.Fatal(err)
	}
	resized, err := ResizeSquare(original, 3)
	if !errors.Is(err, ErrInvalidResize) {
		t.Fatalf("invalid size error = %v", err)
	}
	if resized.Width != 0 || resized.Height != 0 {
		t.Fatalf("invalid resize returned asset = %#v", resized)
	}
	resized, err = ResizeSquare(original, 1000)
	if err != nil {
		t.Fatal(err)
	}
	if resized.Width != 4 || resized.Height != 4 || resized.MIME != "image/png" {
		t.Fatalf("square crop = %#v", resized)
	}
	resized, err = ResizeSquare(original, 500)
	if err != nil {
		t.Fatal(err)
	}
	if resized.Width != 4 || resized.Height != 4 {
		t.Fatalf("small image should not upscale = %#v", resized)
	}
	unchanged, err := ResizeSquare(original, 0)
	if err != nil || unchanged.Hash != original.Hash || unchanged.Width != original.Width || unchanged.Height != original.Height {
		t.Fatalf("original = %#v err=%v", unchanged, err)
	}
}

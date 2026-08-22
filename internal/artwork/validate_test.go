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

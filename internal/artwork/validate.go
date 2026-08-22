package artwork

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"net/http"
	"strings"

	xdraw "golang.org/x/image/draw"
	_ "golang.org/x/image/webp"
)

const (
	MaxBytes  = 10 << 20
	MaxPixels = 40_000_000
)

var ErrInvalid = errors.New("invalid artwork")
var ErrInvalidResize = errors.New("invalid artwork resize")

type Asset struct {
	Data   []byte `json:"-"`
	MIME   string `json:"mime"`
	Format string `json:"format"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
	Size   int    `json:"size"`
	Hash   string `json:"hash"`
}

func Validate(data []byte, declaredMIME string) (Asset, error) {
	if len(data) == 0 {
		return Asset{}, fmt.Errorf("%w: image is empty", ErrInvalid)
	}
	if len(data) > MaxBytes {
		return Asset{}, fmt.Errorf("%w: image exceeds %d MiB", ErrInvalid, MaxBytes>>20)
	}
	detectedMIME := normalizeMIME(http.DetectContentType(data))
	if detectedMIME != "image/jpeg" && detectedMIME != "image/png" && detectedMIME != "image/webp" {
		return Asset{}, fmt.Errorf("%w: unsupported image type %q", ErrInvalid, detectedMIME)
	}
	declaredMIME = normalizeMIME(declaredMIME)
	if declaredMIME != "" && declaredMIME != "application/octet-stream" && declaredMIME != detectedMIME {
		return Asset{}, fmt.Errorf("%w: declared MIME %q does not match %q", ErrInvalid, declaredMIME, detectedMIME)
	}
	config, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return Asset{}, fmt.Errorf("%w: decode image: %v", ErrInvalid, err)
	}
	if config.Width <= 0 || config.Height <= 0 || int64(config.Width)*int64(config.Height) > MaxPixels {
		return Asset{}, fmt.Errorf("%w: image dimensions %dx%d exceed the %d MP limit", ErrInvalid, config.Width, config.Height, MaxPixels/1_000_000)
	}
	digest := sha256.Sum256(data)
	return Asset{
		Data: append([]byte(nil), data...), MIME: detectedMIME, Format: strings.ToUpper(format),
		Width: config.Width, Height: config.Height, Size: len(data), Hash: hex.EncodeToString(digest[:]),
	}, nil
}

// normalizeMIME handles the aliases commonly returned by image CDNs. In
// particular, NetEase currently responds with image/jpg even though the
// payload is a regular JPEG and Go detects it as image/jpeg.
func normalizeMIME(value string) string {
	value = strings.ToLower(strings.TrimSpace(strings.Split(value, ";")[0]))
	switch value {
	case "image/jpg", "image/pjpeg":
		return "image/jpeg"
	case "image/x-png":
		return "image/png"
	default:
		return value
	}
}

func Describe(data []byte) (Asset, error) {
	return Validate(data, "")
}

// ResizeSquare center-crops an artwork to a square and scales it down to the
// requested maximum edge. A zero maxDimension keeps the original bytes. The
// output is always revalidated so the existing byte, pixel and MIME limits
// remain in force after transformation.
func ResizeSquare(asset Asset, maxDimension int) (Asset, error) {
	if maxDimension == 0 {
		return Asset{Data: append([]byte(nil), asset.Data...), MIME: asset.MIME, Format: asset.Format, Width: asset.Width, Height: asset.Height, Size: asset.Size, Hash: asset.Hash}, nil
	}
	if maxDimension != 500 && maxDimension != 1000 {
		return Asset{}, fmt.Errorf("%w: supported sizes are 500 or 1000", ErrInvalidResize)
	}
	source, _, err := image.Decode(bytes.NewReader(asset.Data))
	if err != nil {
		return Asset{}, fmt.Errorf("%w: decode image: %v", ErrInvalidResize, err)
	}
	bounds := source.Bounds()
	side := bounds.Dx()
	if bounds.Dy() < side {
		side = bounds.Dy()
	}
	if side <= 0 {
		return Asset{}, fmt.Errorf("%w: image has no pixels", ErrInvalidResize)
	}
	crop := image.Rect(
		bounds.Min.X+(bounds.Dx()-side)/2,
		bounds.Min.Y+(bounds.Dy()-side)/2,
		bounds.Min.X+(bounds.Dx()-side)/2+side,
		bounds.Min.Y+(bounds.Dy()-side)/2+side,
	)
	targetSide := side
	if targetSide > maxDimension {
		targetSide = maxDimension
	}
	destination := image.NewRGBA(image.Rect(0, 0, targetSide, targetSide))
	xdraw.CatmullRom.Scale(destination, destination.Bounds(), source, crop, xdraw.Over, nil)

	var output bytes.Buffer
	mimeType := asset.MIME
	switch mimeType {
	case "image/png":
		if err := png.Encode(&output, destination); err != nil {
			return Asset{}, fmt.Errorf("%w: encode PNG: %v", ErrInvalidResize, err)
		}
	case "image/jpeg":
		if err := jpeg.Encode(&output, destination, &jpeg.Options{Quality: 92}); err != nil {
			return Asset{}, fmt.Errorf("%w: encode JPEG: %v", ErrInvalidResize, err)
		}
	default:
		// Go has no WebP encoder in the standard/x-image packages; a validated
		// WebP input is safely converted to JPEG for the resized output.
		mimeType = "image/jpeg"
		if err := jpeg.Encode(&output, destination, &jpeg.Options{Quality: 92}); err != nil {
			return Asset{}, fmt.Errorf("%w: encode JPEG: %v", ErrInvalidResize, err)
		}
	}
	resized, err := Validate(output.Bytes(), mimeType)
	if err != nil {
		return Asset{}, fmt.Errorf("%w: validate resized image: %v", ErrInvalidResize, err)
	}
	return resized, nil
}

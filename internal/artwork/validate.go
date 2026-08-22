package artwork

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"net/http"
	"strings"

	_ "golang.org/x/image/webp"
)

const (
	MaxBytes  = 10 << 20
	MaxPixels = 40_000_000
)

var ErrInvalid = errors.New("invalid artwork")

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

package main

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"sort"
	"strings"
	"sync"

	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"

	"github.com/unxed/f4/vfs"
	"github.com/unxed/vtui"
)

// ImageDecoder describes one way of turning file bytes into pixels. Several
// decoders may claim the same extension; the one with the highest priority is
// tried first and the rest act as fallbacks. This is the seam through which
// platform decoders and subplugins will be added later.
type ImageDecoder struct {
	Name       string
	Priority   int
	Extensions []string
	Decode     func(data []byte) (*vtui.ImageSurface, error)
}

// The registry is read from the decoding workers, so it is guarded: a plugin
// registering a decoder while a picture is being decoded is otherwise a race.
var (
	imageDecodersMu sync.RWMutex
	imageDecoders   []ImageDecoder
)

// allImageDecoders returns a snapshot of the registry.
func allImageDecoders() []ImageDecoder {
	imageDecodersMu.RLock()
	defer imageDecodersMu.RUnlock()
	return append([]ImageDecoder(nil), imageDecoders...)
}

// RegisterImageDecoder adds a decoder, replacing an earlier one of the same
// name so that a plugin can override a built-in.
func RegisterImageDecoder(d ImageDecoder) {
	if d.Name == "" || d.Decode == nil {
		return
	}
	imageDecodersMu.Lock()
	defer imageDecodersMu.Unlock()
	for i := range imageDecoders {
		if imageDecoders[i].Name == d.Name {
			imageDecoders[i] = d
			return
		}
	}
	imageDecoders = append(imageDecoders, d)
}

func imageExtension(path string) string {
	base := path
	if i := strings.LastIndexAny(base, "/\\"); i >= 0 {
		base = base[i+1:]
	}
	dot := strings.LastIndex(base, ".")
	if dot < 0 || dot == len(base)-1 {
		return ""
	}
	return strings.ToLower(base[dot+1:])
}

// ImageDecodersFor returns the decoders claiming this file, best first.
func ImageDecodersFor(path string) []ImageDecoder {
	ext := imageExtension(path)
	if ext == "" {
		return nil
	}
	var out []ImageDecoder
	for _, d := range allImageDecoders() {
		for _, e := range d.Extensions {
			if strings.ToLower(e) == ext {
				out = append(out, d)
				break
			}
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].Priority > out[j].Priority
	})
	return out
}

// IsImageFile reports whether anything at all can decode this file.
func IsImageFile(path string) bool {
	return len(ImageDecodersFor(path)) > 0
}

// DecodeImage walks the decoders in priority order and returns the first
// result, together with the name of the decoder that produced it. The ones
// claiming the extension go first; when they all fail the remaining ones are
// offered the file too, because a photograph saved under the wrong name is
// still a photograph.
func DecodeImage(path string, data []byte) (*vtui.ImageSurface, string, error) {
	decoders := ImageDecodersFor(path)
	claimed := make(map[string]bool, len(decoders))
	for _, d := range decoders {
		claimed[d.Name] = true
	}
	rest := make([]ImageDecoder, 0, len(claimed))
	for _, d := range allImageDecoders() {
		if !claimed[d.Name] {
			rest = append(rest, d)
		}
	}
	sort.SliceStable(rest, func(i, j int) bool {
		return rest[i].Priority > rest[j].Priority
	})
	decoders = append(decoders, rest...)
	if len(decoders) == 0 {
		return nil, "", fmt.Errorf("no image decoder for %q", path)
	}

	var lastErr error
	for _, d := range decoders {
		surf, err := d.Decode(data)
		if err == nil && surf.Valid() {
			return surf, d.Name, nil
		}
		if err == nil {
			err = fmt.Errorf("decoder %s produced an empty image", d.Name)
		}
		lastErr = err
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("no image decoder for %q", path)
	}
	return nil, "", lastErr
}

// maxImageFileSize guards against loading a multi gigabyte file into memory.
// Tiled decoding of huge images is a separate job.
const maxImageFileSize = 128 << 20

// imageMaxPixels bounds the geometry a decoder will honour, whatever the
// file claims about itself.
const imageMaxPixels = 64 << 20

// LoadImage reads a file through the VFS and decodes it.
func LoadImage(ctx context.Context, v vfs.VFS, path string) (*vtui.ImageSurface, string, error) {
	f, err := v.Open(ctx, path)
	if err != nil {
		return nil, "", err
	}
	defer f.Close()

	size := f.Size()
	if size <= 0 {
		return nil, "", fmt.Errorf("file is empty")
	}
	if size > maxImageFileSize {
		return nil, "", fmt.Errorf("image is too large: %d bytes", size)
	}

	data := make([]byte, size)
	n, err := f.ReadAt(ctx, data, 0)
	if n <= 0 {
		if err == nil {
			err = fmt.Errorf("nothing could be read")
		}
		return nil, "", err
	}
	return DecodeImage(path, data[:n])
}

func decodeImageWithStdlib(data []byte) (*vtui.ImageSurface, error) {
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	surf := vtui.NewImageSurfaceFromImage(img)
	if surf == nil {
		return nil, fmt.Errorf("unsupported image geometry")
	}
	return surf, nil
}

func init() {
	RegisterImageDecoder(ImageDecoder{
		Name:       "go-std",
		Priority:   0,
		Extensions: []string{"png", "jpg", "jpeg", "jfif", "gif"},
		Decode:     decodeImageWithStdlib,
	})
}

// Package composer stitches a list of tile images into a single mosaic JPEG.
//
// Tiles are center-cropped to square, downsampled with a box filter, and drawn
// into an RGBA canvas at their (row, col) position. Missing tiles leave black
// cells. JPEG-encoded for compact archive storage.
package composer

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	_ "image/png" // register PNG decoder for tiles uploaded as PNG
)

// TileImage is one tile's encoded bytes plus its grid position.
type TileImage struct {
	Row, Col int
	Data     []byte
}

// Options configures the composition. All fields are required except Quality.
type Options struct {
	Cols     int
	Rows     int
	CellSize int // pixels per cell (square)
	Quality  int // JPEG quality 1-100; defaults to 85 if zero
}

// Compose returns JPEG bytes of the mosaic.
func Compose(opts Options, tiles []TileImage) ([]byte, error) {
	if opts.Cols <= 0 || opts.Rows <= 0 || opts.CellSize <= 0 {
		return nil, fmt.Errorf("composer: invalid grid options %+v", opts)
	}

	canvasW := opts.Cols * opts.CellSize
	canvasH := opts.Rows * opts.CellSize
	canvas := image.NewRGBA(image.Rect(0, 0, canvasW, canvasH))
	draw.Draw(canvas, canvas.Bounds(), &image.Uniform{C: color.Black}, image.Point{}, draw.Src)

	for _, t := range tiles {
		if t.Row < 0 || t.Row >= opts.Rows || t.Col < 0 || t.Col >= opts.Cols {
			continue
		}
		img, _, err := image.Decode(bytes.NewReader(t.Data))
		if err != nil {
			return nil, fmt.Errorf("composer: decode tile [%d,%d]: %w", t.Row, t.Col, err)
		}
		// Phone JPEGs typically encode pixels in sensor orientation and rely on
		// an EXIF tag for display rotation. Apply it before cropping/scaling so
		// the mosaic shows tiles upright.
		img = applyExifOrientation(img, readExifOrientation(t.Data))
		cropped := centerCropSquare(img)
		scaled := scaleBox(cropped, opts.CellSize, opts.CellSize)
		dstX := t.Col * opts.CellSize
		dstY := t.Row * opts.CellSize
		draw.Draw(
			canvas,
			image.Rect(dstX, dstY, dstX+opts.CellSize, dstY+opts.CellSize),
			scaled, image.Point{}, draw.Src,
		)
	}

	quality := opts.Quality
	if quality == 0 {
		quality = 85
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, canvas, &jpeg.Options{Quality: quality}); err != nil {
		return nil, fmt.Errorf("composer: encode jpeg: %w", err)
	}
	return buf.Bytes(), nil
}

// Thumbnail decodes JPEG bytes, downsamples to a square of `size` px using the
// box filter, and re-encodes. Cheaper than re-running Compose since we resize
// once instead of per-tile.
func Thumbnail(jpegBytes []byte, size int) ([]byte, error) {
	if size <= 0 {
		return nil, fmt.Errorf("composer: thumbnail size must be > 0")
	}
	img, _, err := image.Decode(bytes.NewReader(jpegBytes))
	if err != nil {
		return nil, fmt.Errorf("composer: thumbnail decode: %w", err)
	}
	scaled := scaleBox(centerCropSquare(img), size, size)
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, scaled, &jpeg.Options{Quality: 80}); err != nil {
		return nil, fmt.Errorf("composer: thumbnail encode: %w", err)
	}
	return buf.Bytes(), nil
}

func centerCropSquare(img image.Image) image.Image {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	if w == h {
		return img
	}
	var crop image.Rectangle
	if w > h {
		offset := (w - h) / 2
		crop = image.Rect(b.Min.X+offset, b.Min.Y, b.Min.X+offset+h, b.Max.Y)
	} else {
		offset := (h - w) / 2
		crop = image.Rect(b.Min.X, b.Min.Y+offset, b.Max.X, b.Min.Y+offset+w)
	}
	if sub, ok := img.(interface {
		SubImage(r image.Rectangle) image.Image
	}); ok {
		return sub.SubImage(crop)
	}
	rgba := image.NewRGBA(image.Rect(0, 0, crop.Dx(), crop.Dy()))
	draw.Draw(rgba, rgba.Bounds(), img, crop.Min, draw.Src)
	return rgba
}

// scaleBox averages all source pixels that fall in each destination pixel.
// Designed for downsampling; for upsampling it degrades to nearest-neighbor.
func scaleBox(src image.Image, dstW, dstH int) *image.RGBA {
	sb := src.Bounds()
	sw, sh := sb.Dx(), sb.Dy()
	dst := image.NewRGBA(image.Rect(0, 0, dstW, dstH))
	if sw == 0 || sh == 0 {
		return dst
	}

	xRatio := float64(sw) / float64(dstW)
	yRatio := float64(sh) / float64(dstH)

	for y := range dstH {
		sy0 := int(float64(y) * yRatio)
		sy1 := int(float64(y+1) * yRatio)
		if sy1 <= sy0 {
			sy1 = sy0 + 1
		}
		if sy1 > sh {
			sy1 = sh
		}
		for x := range dstW {
			sx0 := int(float64(x) * xRatio)
			sx1 := int(float64(x+1) * xRatio)
			if sx1 <= sx0 {
				sx1 = sx0 + 1
			}
			if sx1 > sw {
				sx1 = sw
			}

			var sumR, sumG, sumB, sumA uint64
			var count uint64
			for sy := sy0; sy < sy1; sy++ {
				for sx := sx0; sx < sx1; sx++ {
					r, g, b, a := src.At(sb.Min.X+sx, sb.Min.Y+sy).RGBA()
					sumR += uint64(r >> 8)
					sumG += uint64(g >> 8)
					sumB += uint64(b >> 8)
					sumA += uint64(a >> 8)
					count++
				}
			}
			dst.SetRGBA(x, y, color.RGBA{
				R: uint8(sumR / count),
				G: uint8(sumG / count),
				B: uint8(sumB / count),
				A: uint8(sumA / count),
			})
		}
	}
	return dst
}

// readExifOrientation pulls the EXIF Orientation tag (TIFF tag 0x0112) from a
// JPEG byte stream. Returns 1 (no transform) when EXIF is missing/unreadable.
//
// JPEG layout: SOI (FFD8) followed by markers; EXIF lives in the APP1 marker
// (FFE1) prefixed with "Exif\0\0", then a TIFF header + IFD0.
func readExifOrientation(jpegData []byte) int {
	if len(jpegData) < 4 || jpegData[0] != 0xFF || jpegData[1] != 0xD8 {
		return 1
	}
	i := 2
	for i+4 <= len(jpegData) {
		if jpegData[i] != 0xFF {
			return 1
		}
		marker := jpegData[i+1]
		// SOS (0xDA) — image data starts; stop scanning.
		// EOI (0xD9), or an RSTn (D0..D7) — no length field.
		if marker == 0xDA || marker == 0xD9 {
			return 1
		}
		if marker >= 0xD0 && marker <= 0xD7 {
			i += 2
			continue
		}
		segLen := int(jpegData[i+2])<<8 | int(jpegData[i+3])
		if segLen < 2 || i+2+segLen > len(jpegData) {
			return 1
		}
		if marker == 0xE1 && segLen >= 8 {
			seg := jpegData[i+4 : i+2+segLen]
			if len(seg) >= 6 && string(seg[0:6]) == "Exif\x00\x00" {
				if o := parseTIFFOrientation(seg[6:]); o > 0 {
					return o
				}
				return 1
			}
		}
		i += 2 + segLen
	}
	return 1
}

func parseTIFFOrientation(tiff []byte) int {
	if len(tiff) < 8 {
		return 0
	}
	var bo binary.ByteOrder
	switch string(tiff[0:2]) {
	case "II":
		bo = binary.LittleEndian
	case "MM":
		bo = binary.BigEndian
	default:
		return 0
	}
	if bo.Uint16(tiff[2:4]) != 0x002A {
		return 0
	}
	ifdOffset := int(bo.Uint32(tiff[4:8]))
	if ifdOffset+2 > len(tiff) {
		return 0
	}
	n := int(bo.Uint16(tiff[ifdOffset:]))
	base := ifdOffset + 2
	for k := 0; k < n; k++ {
		entry := base + k*12
		if entry+12 > len(tiff) {
			return 0
		}
		tag := bo.Uint16(tiff[entry:])
		if tag == 0x0112 {
			// SHORT (type 3), count 1 — the value sits in the first 2 bytes
			// of the 4-byte value/offset field.
			return int(bo.Uint16(tiff[entry+8:]))
		}
	}
	return 0
}

// applyExifOrientation rotates/mirrors `img` per the EXIF orientation value
// (1..8). Returns img unchanged for orientation 1 (default) or unrecognized
// values. Operates in pure RGBA copies — no external scaler dependency.
func applyExifOrientation(img image.Image, orientation int) image.Image {
	switch orientation {
	case 2:
		return flipHorizontal(img)
	case 3:
		return rotate180(img)
	case 4:
		return flipVertical(img)
	case 5:
		return rotate90CW(flipHorizontal(img))
	case 6:
		return rotate90CW(img)
	case 7:
		return rotate90CCW(flipHorizontal(img))
	case 8:
		return rotate90CCW(img)
	default:
		return img
	}
}

func rotate90CW(src image.Image) image.Image {
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	dst := image.NewRGBA(image.Rect(0, 0, h, w))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			dst.Set(h-1-y, x, src.At(b.Min.X+x, b.Min.Y+y))
		}
	}
	return dst
}

func rotate90CCW(src image.Image) image.Image {
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	dst := image.NewRGBA(image.Rect(0, 0, h, w))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			dst.Set(y, w-1-x, src.At(b.Min.X+x, b.Min.Y+y))
		}
	}
	return dst
}

func rotate180(src image.Image) image.Image {
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			dst.Set(w-1-x, h-1-y, src.At(b.Min.X+x, b.Min.Y+y))
		}
	}
	return dst
}

func flipHorizontal(src image.Image) image.Image {
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			dst.Set(w-1-x, y, src.At(b.Min.X+x, b.Min.Y+y))
		}
	}
	return dst
}

func flipVertical(src image.Image) image.Image {
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			dst.Set(x, h-1-y, src.At(b.Min.X+x, b.Min.Y+y))
		}
	}
	return dst
}

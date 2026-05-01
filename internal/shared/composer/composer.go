// Package composer stitches a list of tile images into a single mosaic JPEG.
//
// Tiles are center-cropped to square, downsampled with a box filter, and drawn
// into an RGBA canvas at their (row, col) position. Missing tiles leave black
// cells. JPEG-encoded for compact archive storage.
package composer

import (
	"bytes"
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

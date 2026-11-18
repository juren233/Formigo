package converter

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/gif"
	"image/jpeg"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/HugoSmits86/nativewebp"
	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/srwiley/oksvg"
	"github.com/srwiley/rasterx"
	"golang.org/x/image/bmp"
	xdraw "golang.org/x/image/draw"
	"golang.org/x/image/tiff"
	xwebp "golang.org/x/image/webp"
)

const maxImagePixels = 120_000_000 // 120 MP safety ceiling

func (p *Pipeline) convertImage(
	ctx context.Context,
	engine EngineID,
	inPath, srcExt, outPath, dstExt, workDir string,
	opts Options,
	report func(float64, string),
) ([]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, ErrCanceled
	}

	// Image -> Video (MP4 / WebM) via FFmpeg
	if dstExt == "mp4" || dstExt == "webm" {
		return p.convertImageToVideo(ctx, inPath, srcExt, outPath, dstExt, workDir, opts)
	}

	report(0.15, "Decoding source image")
	img, warnings, err := p.decodeAnyImage(ctx, engine, inPath, srcExt, workDir, opts.ImageMaxWidth)
	if err != nil {
		return nil, err
	}

	bounds := img.Bounds()
	w, h := bounds.Dx(), bounds.Dy()
	if w <= 0 || h <= 0 {
		return nil, errors.New("decoded image has invalid zero dimensions")
	}
	if int64(w)*int64(h) > maxImagePixels {
		return nil, fmt.Errorf("image dimensions (%dx%d) exceed 120MP safety budget", w, h)
	}

	// Apply proportional max-width downscaling when requested (never upscale).
	if opts.ImageMaxWidth > 0 && w > opts.ImageMaxWidth {
		newW := opts.ImageMaxWidth
		newH := int(float64(h) * float64(newW) / float64(w))
		if newH < 1 {
			newH = 1
		}
		report(0.45, fmt.Sprintf("Resizing %dx%d -> %dx%d", w, h, newW, newH))
		img = resizeCatmullRom(img, newW, newH)
	}

	report(0.65, "Encoding "+strings.ToUpper(dstExt))
	if dstExt == "pdf" {
		stagePNG := filepath.Join(workDir, "page-1.png")
		if err := encodeImageToFile(img, stagePNG, "png", opts.ImageQuality); err != nil {
			return warnings, err
		}
		if err := api.ImportImagesFile(ctx, []string{stagePNG}, outPath, nil, nil); err != nil {
			return warnings, fmt.Errorf("PDF image import failed: %w", err)
		}
		return warnings, nil
	}

	if err := encodeImageToFile(img, outPath, dstExt, opts.ImageQuality); err != nil {
		return warnings, err
	}
	return warnings, nil
}

func (p *Pipeline) decodeAnyImage(ctx context.Context, engine EngineID, inPath, srcExt, workDir string, targetMaxW int) (image.Image, []string, error) {
	switch {
	case engine == EngineSips:
		tmpPNG := filepath.Join(workDir, "sips-decoded.png")
		sipsBin := p.rt.Path(EngineSips)
		if sipsBin == "" {
			sipsBin = "sips"
		}
		_, _, err := RunCommand(ctx, sipsBin, []string{"-s", "format", "png", inPath, "--out", tmpPNG}, nil)
		if err != nil {
			// Fallback to FFmpeg if available
			if p.rt.Available(EngineFFmpeg) {
				return p.decodeImageViaFFmpeg(ctx, inPath, workDir)
			}
			return nil, nil, err
		}
		img, err := decodeStandardFile(tmpPNG, "png")
		return img, nil, err

	case engine == EngineFFmpeg || srcExt == "avif" || srcExt == "tga" || srcExt == "heic" || srcExt == "heif":
		return p.decodeImageViaFFmpeg(ctx, inPath, workDir)

	case srcExt == "svg":
		targetSide := 1024
		if targetMaxW > 0 {
			targetSide = targetMaxW
		}
		img, err := rasterizeSVG(inPath, targetSide)
		return img, nil, err

	case srcExt == "ico":
		img, err := decodeICOFile(inPath)
		return img, nil, err

	default:
		img, err := decodeStandardFile(inPath, srcExt)
		if err != nil && p.rt != nil && p.rt.Available(EngineFFmpeg) {
			return p.decodeImageViaFFmpeg(ctx, inPath, workDir)
		}
		return img, nil, err
	}
}

func (p *Pipeline) decodeImageViaFFmpeg(ctx context.Context, inPath, workDir string) (image.Image, []string, error) {
	ff := p.rt.Path(EngineFFmpeg)
	if ff == "" {
		return nil, nil, errors.New("FFmpeg is required to decode this image format")
	}
	tmpPNG := filepath.Join(workDir, "ffmpeg-decoded.png")
	_, _, err := RunCommand(ctx, ff, []string{"-hide_banner", "-loglevel", "error", "-y", "-i", inPath, "-frames:v", "1", tmpPNG}, nil)
	if err != nil {
		return nil, nil, err
	}
	img, err := decodeStandardFile(tmpPNG, "png")
	return img, nil, err
}

func decodeStandardFile(path, ext string) (image.Image, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	switch ext {
	case "png":
		return png.Decode(f)
	case "jpg":
		return jpeg.Decode(f)
	case "gif":
		return gif.Decode(f)
	case "bmp":
		return bmp.Decode(f)
	case "tiff":
		return tiff.Decode(f)
	case "webp":
		data, err := io.ReadAll(f)
		if err != nil {
			return nil, err
		}
		if img, err := xwebp.Decode(bytes.NewReader(data)); err == nil {
			return img, nil
		}
		return nativewebp.Decode(bytes.NewReader(data))
	default:
		img, _, err := image.Decode(f)
		return img, err
	}
}

func encodeImageToFile(img image.Image, outPath, dstExt string, quality int) error {
	if quality <= 0 || quality > 100 {
		quality = 90
	}
	f, err := os.Create(outPath)
	if err != nil {
		return err
	}
	defer f.Close()

	switch dstExt {
	case "png":
		return png.Encode(f, img)
	case "jpg":
		flat := flattenOnWhite(img)
		return jpeg.Encode(f, flat, &jpeg.Options{Quality: quality})
	case "webp":
		return nativewebp.Encode(f, img, nil)
	case "bmp":
		flat := flattenOnWhite(img)
		return bmp.Encode(f, flat)
	case "tiff":
		return tiff.Encode(f, img, &tiff.Options{Compression: tiff.Deflate, Predictor: true})
	case "gif":
		return gif.Encode(f, img, &gif.Options{NumColors: 256})
	case "ico":
		return encodeMultiSizeICO(f, img)
	default:
		return fmt.Errorf("unsupported image output format: %s", dstExt)
	}
}

func flattenOnWhite(src image.Image) image.Image {
	b := src.Bounds()
	dst := image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	draw.Draw(dst, dst.Bounds(), &image.Uniform{C: color.White}, image.Point{}, draw.Src)
	draw.Draw(dst, dst.Bounds(), src, b.Min, draw.Over)
	return dst
}

func resizeCatmullRom(src image.Image, w, h int) *image.NRGBA {
	dst := image.NewNRGBA(image.Rect(0, 0, w, h))
	xdraw.CatmullRom.Scale(dst, dst.Bounds(), src, src.Bounds(), xdraw.Over, nil)
	return dst
}

// rasterizeSVG renders an SVG file into an RGBA image preserving its viewBox aspect ratio.
func rasterizeSVG(path string, targetMaxSide int) (image.Image, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	icon, err := oksvg.ReadIconStream(f)
	if err != nil {
		return nil, fmt.Errorf("invalid SVG: %w", err)
	}

	vw, vh := icon.ViewBox.W, icon.ViewBox.H
	if vw <= 0 {
		vw = 512
	}
	if vh <= 0 {
		vh = 512
	}
	if targetMaxSide <= 0 {
		targetMaxSide = 1024
	}

	var w, h int
	if vw >= vh {
		w = targetMaxSide
		h = int(vh * float64(targetMaxSide) / vw)
	} else {
		h = targetMaxSide
		w = int(vw * float64(targetMaxSide) / vh)
	}
	if w < 1 {
		w = 1
	}
	if h < 1 {
		h = 1
	}

	icon.SetTarget(0, 0, float64(w), float64(h))
	rgba := image.NewRGBA(image.Rect(0, 0, w, h))
	scanner := rasterx.NewScannerGV(w, h, rgba, rgba.Bounds())
	dasher := rasterx.NewDasher(w, h, scanner)
	icon.Draw(dasher, 1.0)
	return rgba, nil
}

// encodeMultiSizeICO generates a multi-resolution Windows ICO (16..256px) with transparent aspect-ratio padding,
// never upscaling images smaller than standard tiers.
func encodeMultiSizeICO(w io.Writer, src image.Image) error {
	squared := padToSquareTransparent(src)
	maxSide := squared.Bounds().Dx()
	allTiers := []int{16, 32, 48, 64, 128, 256}
	var sizes []int
	for _, s := range allTiers {
		if s <= maxSide {
			sizes = append(sizes, s)
		}
	}
	if len(sizes) == 0 {
		side := maxSide
		if side < 16 {
			side = maxSide
		}
		if side > 256 {
			side = 256
		}
		sizes = []int{side}
	}

	frames := make([][]byte, len(sizes))
	for i, sz := range sizes {
		var frameImg image.Image = squared
		if sz != maxSide {
			frameImg = resizeCatmullRom(squared, sz, sz)
		}
		var buf bytes.Buffer
		if err := png.Encode(&buf, frameImg); err != nil {
			return err
		}
		frames[i] = buf.Bytes()
	}

	// ICONDIR header (6 bytes): Reserved=0, Type=1 (Icon), Count=len(frames)
	header := make([]byte, 6)
	binary.LittleEndian.PutUint16(header[0:2], 0)
	binary.LittleEndian.PutUint16(header[2:4], 1)
	binary.LittleEndian.PutUint16(header[4:6], uint16(len(frames)))
	if _, err := w.Write(header); err != nil {
		return err
	}

	offset := uint32(6 + 16*len(frames))
	for i, sz := range sizes {
		entry := make([]byte, 16)
		if sz < 256 {
			entry[0] = byte(sz)
			entry[1] = byte(sz)
		} // 0 represents 256px per ICO specification
		entry[2] = 0 // palette count
		entry[3] = 0 // reserved
		binary.LittleEndian.PutUint16(entry[4:6], 1)   // color planes
		binary.LittleEndian.PutUint16(entry[6:8], 32)  // bits per pixel
		binary.LittleEndian.PutUint32(entry[8:12], uint32(len(frames[i])))
		binary.LittleEndian.PutUint32(entry[12:16], offset)
		if _, err := w.Write(entry); err != nil {
			return err
		}
		offset += uint32(len(frames[i]))
	}

	for _, payload := range frames {
		if _, err := w.Write(payload); err != nil {
			return err
		}
	}
	return nil
}

func padToSquareTransparent(src image.Image) image.Image {
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	if w == h {
		return src
	}
	side := w
	if h > side {
		side = h
	}
	dst := image.NewNRGBA(image.Rect(0, 0, side, side))
	offX := (side - w) / 2
	offY := (side - h) / 2
	draw.Draw(dst, image.Rect(offX, offY, offX+w, offY+h), src, b.Min, draw.Src)
	return dst
}

// decodeICOFile parses an .ico container and decodes its highest-resolution PNG or uncompressed DIB frame.
func decodeICOFile(path string) (image.Image, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if len(data) < 6 {
		return nil, errors.New("corrupt ICO header")
	}
	reserved := binary.LittleEndian.Uint16(data[0:2])
	icoType := binary.LittleEndian.Uint16(data[2:4])
	count := int(binary.LittleEndian.Uint16(data[4:6]))
	if reserved != 0 || (icoType != 1 && icoType != 2) || count <= 0 || len(data) < 6+count*16 {
		return nil, errors.New("invalid ICO directory header")
	}

	bestIdx := -1
	bestArea := -1
	bestBPP := 0
	var bestOffset, bestSize uint32

	for i := 0; i < count; i++ {
		ent := data[6+i*16 : 6+(i+1)*16]
		w := int(ent[0])
		if w == 0 {
			w = 256
		}
		h := int(ent[1])
		if h == 0 {
			h = 256
		}
		bpp := int(binary.LittleEndian.Uint16(ent[6:8]))
		sz := binary.LittleEndian.Uint32(ent[8:12])
		off := binary.LittleEndian.Uint32(ent[12:16])
		if sz == 0 || uint64(off)+uint64(sz) > uint64(len(data)) {
			continue
		}
		area := w * h
		if area > bestArea || (area == bestArea && bpp > bestBPP) {
			bestIdx = i
			bestArea = area
			bestBPP = bpp
			bestOffset = off
			bestSize = sz
		}
	}
	if bestIdx < 0 {
		return nil, errors.New("ICO contains no valid image frames")
	}

	frame := data[bestOffset : bestOffset+bestSize]
	if bytes.HasPrefix(frame, []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1A, '\n'}) {
		return png.Decode(bytes.NewReader(frame))
	}
	return decodeICODIB(frame)
}

func decodeICODIB(dib []byte) (image.Image, error) {
	if len(dib) < 40 {
		return nil, errors.New("ICO DIB header too short")
	}
	hdrSize := binary.LittleEndian.Uint32(dib[0:4])
	width := int(int32(binary.LittleEndian.Uint32(dib[4:8])))
	heightX2 := int(int32(binary.LittleEndian.Uint32(dib[8:12])))
	bpp := binary.LittleEndian.Uint16(dib[14:16])
	compression := binary.LittleEndian.Uint32(dib[16:20])

	if hdrSize < 40 || width <= 0 || heightX2 == 0 || compression != 0 {
		return nil, errors.New("unsupported compressed or non-standard ICO DIB frame")
	}
	bottomUp := heightX2 > 0
	if heightX2 < 0 {
		heightX2 = -heightX2
	}
	height := heightX2 / 2
	if height <= 0 || width > 4096 || height > 4096 {
		return nil, errors.New("invalid ICO DIB dimensions")
	}
	if bpp != 32 && bpp != 24 {
		return nil, fmt.Errorf("unsupported ICO DIB bit depth: %d-bit", bpp)
	}

	stride := ((width*int(bpp) + 31) / 32) * 4
	pixOffset := int(hdrSize)
	xorBytes := stride * height
	if len(dib) < pixOffset+xorBytes {
		return nil, errors.New("truncated ICO DIB pixel data")
	}

	andStride := ((width + 31) / 32) * 4
	andOffset := pixOffset + xorBytes
	hasAndMask := len(dib) >= andOffset+andStride*height

	img := image.NewNRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		srcY := y
		if bottomUp {
			srcY = height - 1 - y
		}
		row := dib[pixOffset+srcY*stride : pixOffset+(srcY+1)*stride]
		for x := 0; x < width; x++ {
			var r, g, b, a uint8
			if bpp == 32 {
				b = row[x*4]
				g = row[x*4+1]
				r = row[x*4+2]
				a = row[x*4+3]
			} else {
				b = row[x*3]
				g = row[x*3+1]
				r = row[x*3+2]
				a = 255
				if hasAndMask {
					maskByte := dib[andOffset+srcY*andStride+(x/8)]
					if (maskByte>>(7-(x%8)))&1 == 1 {
						a = 0
					}
				}
			}
			img.SetNRGBA(x, y, color.NRGBA{R: r, G: g, B: b, A: a})
		}
	}
	return img, nil
}

func (p *Pipeline) convertImageToVideo(ctx context.Context, inPath, srcExt, outPath, dstExt, workDir string, opts Options) ([]string, error) {
	ff := p.rt.Path(EngineFFmpeg)
	if ff == "" {
		return nil, errors.New("FFmpeg is required for image-to-video conversion")
	}
	sourceForFFmpeg := inPath
	if srcExt == "ico" || srcExt == "svg" || srcExt == "webp" {
		img, _, err := p.decodeAnyImage(ctx, EngineBuiltin, inPath, srcExt, workDir, opts.ImageMaxWidth)
		if err != nil {
			return nil, err
		}
		tmpPNG := filepath.Join(workDir, "video-frame.png")
		if err := encodeImageToFile(img, tmpPNG, "png", 95); err != nil {
			return nil, err
		}
		sourceForFFmpeg = tmpPNG
	}

	var args []string
	if srcExt == "gif" {
		args = []string{"-hide_banner", "-loglevel", "error", "-y", "-i", sourceForFFmpeg}
	} else {
		args = []string{"-hide_banner", "-loglevel", "error", "-y", "-loop", "1", "-t", "3", "-i", sourceForFFmpeg}
	}
	if dstExt == "webm" {
		args = append(args, "-c:v", "libvpx-vp9", "-pix_fmt", "yuv420p", "-vf", "scale=trunc(iw/2)*2:trunc(ih/2)*2", outPath)
	} else {
		args = append(args, "-c:v", "libx264", "-pix_fmt", "yuv420p", "-movflags", "+faststart", "-vf", "scale=trunc(iw/2)*2:trunc(ih/2)*2", outPath)
	}
	_, _, err := RunCommand(ctx, ff, args, nil)
	return nil, err
}

func (p *Pipeline) convertImageOCR(ctx context.Context, inPath, outPath, dstExt, workDir string, opts Options) ([]string, error) {
	tess := p.rt.Path(EngineTesseract)
	if tess == "" {
		return nil, errors.New("Tesseract OCR is not installed")
	}
	lang := p.rt.BestOCRLang(opts.OCRLanguage)
	outBase := filepath.Join(workDir, "ocr-out")
	_, _, err := RunCommand(ctx, tess, []string{inPath, outBase, "-l", lang}, nil)
	if err != nil {
		return nil, err
	}
	raw, err := os.ReadFile(outBase + ".txt")
	if err != nil {
		return nil, err
	}
	text := strings.TrimSpace(normalizeNewlines(string(raw)))
	if text == "" {
		text = "(No text recognized)"
	}
	if dstExt == "md" {
		title := safeStem(inPath)
		content := fmt.Sprintf("# %s\n\n%s\n", title, text)
		return nil, os.WriteFile(outPath, []byte(content), 0o644)
	}
	return nil, os.WriteFile(outPath, []byte(text+"\n"), 0o644)
}

package files

import (
	"bytes"
	"context"
	"encoding/base64"
	"image"
	"image/color"
	"image/draw"
	_ "image/gif"
	_ "image/jpeg"
	"image/png"
	"os"

	"github.com/ervisio/ervisio/server/internal/rpc"
)

const (
	maxThumbInput  = 25 << 20
	maxThumbPixels = 50_000_000
)

func hThumbnail(ctx context.Context, c *rpc.Call) (any, error) {
	var p struct {
		Path string `json:"path"`
		Size int    `json:"size"`
	}
	if err := c.Bind(&p); err != nil {
		return nil, err
	}
	path, err := cleanPath(p.Path)
	if err != nil {
		return nil, err
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	if err := checkThumbSource(f); err != nil {
		return nil, err
	}
	img, cfg, format, err := decodeThumbImage(f)
	if err != nil {
		return nil, err
	}
	out := scaleDown(img, clampThumbSize(p.Size))
	var buf bytes.Buffer
	if err := (&png.Encoder{CompressionLevel: png.BestSpeed}).Encode(&buf, out); err != nil {
		return nil, err
	}
	return map[string]any{
		"mime": "image/png", "data": base64.StdEncoding.EncodeToString(buf.Bytes()),
		"width": out.Bounds().Dx(), "height": out.Bounds().Dy(),
		"origWidth": cfg.Width, "origHeight": cfg.Height, "format": format,
	}, nil
}

func clampThumbSize(size int) int {
	if size <= 0 {
		return 256
	}
	return min(max(size, 32), 640)
}

// checkThumbSource rejects anything that is not a regular file of sane size.
func checkThumbSource(f *os.File) error {
	fi, err := f.Stat()
	if err != nil {
		return err
	}
	if !fi.Mode().IsRegular() {
		return rpc.Errorf(rpc.Invalid, "This is not an image file.")
	}
	if fi.Size() > maxThumbInput {
		return rpc.Errorf(rpc.Invalid, "This image is too large for a preview (over 25 MiB).")
	}
	return nil
}

// decodeThumbImage checks the header first so huge images are never decoded.
func decodeThumbImage(f *os.File) (image.Image, image.Config, string, error) {
	cfg, format, err := image.DecodeConfig(f)
	if err != nil {
		return nil, cfg, "", rpc.Errorf(rpc.Invalid, "This image format cannot be previewed (PNG, JPEG and GIF are supported).")
	}
	if cfg.Width <= 0 || cfg.Height <= 0 || cfg.Width*cfg.Height > maxThumbPixels {
		return nil, cfg, "", rpc.Errorf(rpc.Invalid, "This image has too many pixels for a preview.")
	}
	if _, err := f.Seek(0, 0); err != nil {
		return nil, cfg, "", err
	}
	img, _, err := image.Decode(f)
	if err != nil {
		return nil, cfg, "", rpc.Errorf(rpc.Invalid, "This image is damaged and cannot be previewed.")
	}
	return img, cfg, format, nil
}

// fitSize returns the dimensions of a w x h image fitted inside size x size.
func fitSize(w, h, size int) (int, int) {
	if w <= size && h <= size {
		return w, h
	}
	nw, nh := size, h*size/w
	if w < h {
		nw, nh = w*size/h, size
	}
	return max(nw, 1), max(nh, 1)
}

// scaleDown fits img inside size x size with an area-average filter. Images
// that are already small are returned as RGBA unchanged.
func scaleDown(img image.Image, size int) *image.RGBA {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	nw, nh := fitSize(w, h, size)
	src := image.NewNRGBA(image.Rect(0, 0, w, h))
	draw.Draw(src, src.Bounds(), img, b.Min, draw.Src)
	dst := image.NewRGBA(image.Rect(0, 0, nw, nh))
	if nw == w && nh == h {
		draw.Draw(dst, dst.Bounds(), src, image.Point{}, draw.Src)
		return dst
	}
	for y := 0; y < nh; y++ {
		for x := 0; x < nw; x++ {
			dst.SetRGBA(x, y, averageBlock(src, x*w/nw, (x+1)*w/nw, y*h/nh, (y+1)*h/nh))
		}
	}
	return dst
}

// averageBlock returns the alpha-weighted mean of the source pixels in
// [x0,x1) x [y0,y1); an empty range still covers one pixel.
func averageBlock(src *image.NRGBA, x0, x1, y0, y1 int) color.RGBA {
	if y1 <= y0 {
		y1 = y0 + 1
	}
	if x1 <= x0 {
		x1 = x0 + 1
	}
	var r, g, bl, a, n uint64
	for sy := y0; sy < y1; sy++ {
		row := src.Pix[sy*src.Stride:]
		for sx := x0; sx < x1; sx++ {
			px := row[sx*4 : sx*4+4]
			al := uint64(px[3])
			r += uint64(px[0]) * al
			g += uint64(px[1]) * al
			bl += uint64(px[2]) * al
			a += al
			n++
		}
	}
	if a == 0 {
		return color.RGBA{}
	}
	return color.RGBA{uint8(r / a), uint8(g / a), uint8(bl / a), uint8(a / n)}
}

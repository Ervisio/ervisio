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

	"github.com/Fonlogen/LinuxAdmin/server/internal/rpc"
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
	size := p.Size
	if size <= 0 {
		size = 256
	}
	if size < 32 {
		size = 32
	}
	if size > 640 {
		size = 640
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !fi.Mode().IsRegular() {
		return nil, rpc.Errorf(rpc.Invalid, "This is not an image file.")
	}
	if fi.Size() > maxThumbInput {
		return nil, rpc.Errorf(rpc.Invalid, "This image is too large for a preview (over 25 MiB).")
	}
	cfg, format, err := image.DecodeConfig(f)
	if err != nil {
		return nil, rpc.Errorf(rpc.Invalid, "This image format cannot be previewed (PNG, JPEG and GIF are supported).")
	}
	if cfg.Width <= 0 || cfg.Height <= 0 || cfg.Width*cfg.Height > maxThumbPixels {
		return nil, rpc.Errorf(rpc.Invalid, "This image has too many pixels for a preview.")
	}
	if _, err := f.Seek(0, 0); err != nil {
		return nil, err
	}
	img, _, err := image.Decode(f)
	if err != nil {
		return nil, rpc.Errorf(rpc.Invalid, "This image is damaged and cannot be previewed.")
	}
	out := scaleDown(img, size)
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

// scaleDown fits img inside size x size with an area-average filter. Images
// that are already small are returned as RGBA unchanged.
func scaleDown(img image.Image, size int) *image.RGBA {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	nw, nh := w, h
	if w > size || h > size {
		if w >= h {
			nw, nh = size, h*size/w
		} else {
			nw, nh = w*size/h, size
		}
		if nh < 1 {
			nh = 1
		}
		if nw < 1 {
			nw = 1
		}
	}
	src := image.NewNRGBA(image.Rect(0, 0, w, h))
	draw.Draw(src, src.Bounds(), img, b.Min, draw.Src)
	dst := image.NewRGBA(image.Rect(0, 0, nw, nh))
	if nw == w && nh == h {
		draw.Draw(dst, dst.Bounds(), src, image.Point{}, draw.Src)
		return dst
	}
	for y := 0; y < nh; y++ {
		y0, y1 := y*h/nh, (y+1)*h/nh
		if y1 <= y0 {
			y1 = y0 + 1
		}
		for x := 0; x < nw; x++ {
			x0, x1 := x*w/nw, (x+1)*w/nw
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
				dst.SetRGBA(x, y, color.RGBA{})
				continue
			}
			dst.SetRGBA(x, y, color.RGBA{uint8(r / a), uint8(g / a), uint8(bl / a), uint8(a / n)})
		}
	}
	return dst
}

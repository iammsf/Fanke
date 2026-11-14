package media

import (
	"bytes"
	"encoding/binary"
	"fmt"
	_ "golang.org/x/image/webp"
	"image"
	"path"
	"strconv"
	"strings"
	"unicode"
)

const VariantPrefix = "_media_variants/"

func VariantWidths(width int) []int {
	if width <= 0 {
		return nil
	}
	out := []int{}
	for _, w := range []int{480, 740, 1080} {
		if width < w {
			if len(out) == 0 || out[len(out)-1] != width {
				out = append(out, width)
			}
			break
		}
		out = append(out, w)
	}
	return out
}

func VariantKey(filename, sha string, id int64, width int) string {
	filename = path.Base(strings.ReplaceAll(filename, "\\", "/"))
	stem := strings.TrimSuffix(filename, path.Ext(filename))
	var b strings.Builder
	for _, r := range stem {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '-' || r == '_' {
			b.WriteRune(r)
		} else {
			b.WriteByte('_')
		}
		if b.Len() >= 90 {
			break
		}
	}
	if b.Len() == 0 {
		b.WriteString("image")
	}
	return VariantPrefix + sha + "/" + strconv.FormatInt(id, 10) + "/" + b.String() + "-" + strconv.Itoa(width) + "w.webp"
}

// StaticImageConfig rejects animated and unsupported sources before invoking cwebp.
func StaticImageConfig(data []byte) (image.Config, error) {
	if bytes.HasPrefix(data, []byte("\x89PNG\r\n\x1a\n")) {
		for off := 8; off+12 <= len(data); {
			n := int(binary.BigEndian.Uint32(data[off:]))
			if n > len(data)-off-12 {
				return image.Config{}, fmt.Errorf("invalid PNG chunk")
			}
			if string(data[off+4:off+8]) == "acTL" {
				return image.Config{}, fmt.Errorf("animated PNG skipped")
			}
			off += n + 12
		}
	}
	if len(data) >= 12 && string(data[:4]) == "RIFF" && string(data[8:12]) == "WEBP" {
		for off := 12; off+8 <= len(data); {
			n := int(binary.LittleEndian.Uint32(data[off+4:]))
			if n > len(data)-off-8 {
				return image.Config{}, fmt.Errorf("invalid WebP chunk")
			}
			kind := string(data[off : off+4])
			if kind == "ANIM" || kind == "ANMF" || (kind == "VP8X" && n > 0 && data[off+8]&2 != 0) {
				return image.Config{}, fmt.Errorf("animated WebP skipped")
			}
			off += 8 + n + (n & 1)
		}
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return cfg, err
	}
	if format != "jpeg" && format != "png" && format != "webp" {
		return cfg, fmt.Errorf("unsupported image format: %s", format)
	}
	if cfg.Width <= 0 || cfg.Height <= 0 {
		return cfg, fmt.Errorf("invalid dimensions")
	}
	return cfg, nil
}

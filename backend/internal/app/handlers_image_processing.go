package app

import (
	"encoding/json"
	"net/http"
	"sort"
)

type ImageProcessingSettings struct {
	Enabled bool  `json:"enabled"`
	Widths  []int `json:"widths"`
	Quality int   `json:"quality"`
}

func normalizeImageProcessing(in ImageProcessingSettings) ImageProcessingSettings {
	seen := map[int]bool{}
	widths := []int{}
	for _, w := range in.Widths {
		if w >= 16 && w <= 8192 && !seen[w] {
			seen[w] = true
			widths = append(widths, w)
		}
	}
	sort.Ints(widths)
	if len(widths) > 8 {
		widths = widths[:8]
	}
	if len(widths) == 0 {
		widths = defaultImageProcessing().Widths
	}
	in.Widths = widths
	if in.Quality < 1 || in.Quality > 100 {
		in.Quality = defaultImageProcessing().Quality
	}
	return in
}

func defaultImageProcessing() ImageProcessingSettings {
	return ImageProcessingSettings{Widths: []int{480, 740, 1080}, Quality: 82}
}
func (a *App) GetImageProcessing(w http.ResponseWriter, r *http.Request) {
	out := defaultImageProcessing()
	raw, err := a.DB.GetSetting(r.Context(), "media_image_upload")
	if err == nil {
		_ = unmarshalJSON(raw, &out)
	}
	writeJSON(w, http.StatusOK, normalizeImageProcessing(out))
}
func (a *App) PutImageProcessing(w http.ResponseWriter, r *http.Request) {
	var in ImageProcessingSettings
	if err := decodeJSON(r, &in); err != nil {
		a.fail(w, 400, "invalid json")
		return
	}
	if len(in.Widths) < 1 || len(in.Widths) > 8 || in.Quality < 1 || in.Quality > 100 {
		a.fail(w, 400, "widths数量必须1-8且quality为1-100")
		return
	}
	seen := map[int]bool{}
	for _, v := range in.Widths {
		if v < 16 || v > 8192 || seen[v] {
			a.fail(w, 400, "widths必须唯一且范围16..8192")
			return
		}
		seen[v] = true
	}
	sort.Ints(in.Widths)
	if err := a.DB.PutSetting(r.Context(), "media_image_upload", in); err != nil {
		a.failInternal(w, r, "save image processing", err)
		return
	}
	writeJSON(w, 200, in)
}
func unmarshalJSON(b []byte, v any) error { return json.Unmarshal(b, v) }

package main

import (
	"encoding/json"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"os"
	"os/exec"
	"strconv"
)

type Req struct {
	Version int             `json:"version"`
	ID      string          `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}
type Resp struct {
	Version int    `json:"version"`
	ID      string `json:"id"`
	Result  any    `json:"result,omitempty"`
	Error   string `json:"error,omitempty"`
}
type P struct {
	Input   string `json:"input"`
	Output  string `json:"output"`
	Width   int    `json:"width"`
	Quality int    `json:"quality"`
}

func main() {
	var q Req
	if json.NewDecoder(os.Stdin).Decode(&q) != nil {
		return
	}
	if q.Version != 1 {
		return
	}
	r := Resp{Version: 1, ID: q.ID}
	var err error
	var v any
	if q.Method != "image.process" {
		err = fmt.Errorf("unsupported method")
	} else {
		var p P
		err = json.Unmarshal(q.Params, &p)
		if err == nil {
			v, err = process(p)
		}
	}
	if err != nil {
		r.Error = err.Error()
	} else {
		r.Result = v
	}
	_ = json.NewEncoder(os.Stdout).Encode(r)
}
func process(p P) (any, error) {
	in, err := os.Open(p.Input)
	if err != nil {
		return nil, err
	}
	defer in.Close()
	cfg, _, err := image.DecodeConfig(in)
	if err != nil {
		return nil, err
	}
	if p.Width <= 0 || p.Width > cfg.Width {
		p.Width = cfg.Width
	}
	if p.Quality <= 0 {
		p.Quality = 82
	}
	args := []string{"-quiet", "-q", strconv.Itoa(p.Quality)}
	if p.Width < cfg.Width {
		args = append(args, "-resize", strconv.Itoa(p.Width), "0")
	}
	args = append(args, "-o", p.Output, "--", p.Input)
	_, err = exec.Command("cwebp", args...).CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("cwebp failed: %w", err)
	}
	st, err := os.Stat(p.Output)
	if err != nil {
		return nil, err
	}
	return map[string]any{"path": p.Output, "width": p.Width, "height": cfg.Height * p.Width / cfg.Width, "size": st.Size()}, nil
}

var _ = io.EOF

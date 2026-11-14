package pluginrpc

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

type Manifest struct {
	Protocol     int      `json:"protocol_version"`
	Trusted      bool     `json:"trusted_process"`
	Entrypoint   string   `json:"entrypoint"`
	Capabilities []string `json:"capabilities"`
	Dependencies []string `json:"dependencies"`
}

func Entry(template string) string {
	s := strings.ReplaceAll(strings.ReplaceAll(template, "${goos}", runtime.GOOS), "${goarch}", runtime.GOARCH)
	if runtime.GOOS == "windows" && !strings.HasSuffix(s, ".exe") {
		s += ".exe"
	}
	return s
}
func LoadManifest(root string) (Manifest, error) {
	var m Manifest
	if fi, err := os.Lstat(root); err != nil || !fi.IsDir() || fi.Mode()&os.ModeSymlink != 0 {
		return m, errors.New("plugin root must be a real directory")
	}
	b, err := os.ReadFile(filepath.Join(root, "info.json"))
	if err != nil {
		return m, err
	}
	err = json.Unmarshal(b, &m)
	return m, err
}
func ValidateManifest(root, slug string, checkDependencies bool) error {
	m, err := LoadManifest(root)
	if err != nil {
		return errors.New("invalid server manifest")
	}
	expected := map[string][]string{
		"s3-storage":       {"storage.save", "storage.open", "storage.delete", "storage.list", "storage.head", "storage.setCacheControl", "storage.listBuckets", "storage.test"},
		"image-processing": {"image.process"},
	}
	caps, server := expected[slug]
	if !server {
		if m.Protocol != 0 || m.Entrypoint != "" || m.Trusted || len(m.Capabilities) > 0 || len(m.Dependencies) > 0 {
			return errors.New("unsupported server plugin")
		}
		return nil
	}
	if m.Protocol != ProtocolVersion || !m.Trusted || m.Entrypoint != "server/bin/${goos}-${goarch}/"+slug+"-server" {
		return errors.New("invalid server protocol or entrypoint")
	}
	if len(m.Capabilities) != len(caps) {
		return errors.New("invalid server capabilities")
	}
	seen := map[string]bool{}
	for _, c := range m.Capabilities {
		if seen[c] {
			return errors.New("duplicate capability")
		}
		seen[c] = true
	}
	for _, c := range caps {
		if !seen[c] {
			return errors.New("missing server capability")
		}
	}
	if slug == "image-processing" {
		if len(m.Dependencies) != 1 || m.Dependencies[0] != "cwebp" {
			return errors.New("invalid image dependencies")
		}
	} else if len(m.Dependencies) != 0 {
		return errors.New("invalid storage dependencies")
	}
	if err := ValidateEntrypoint(root, Entry(m.Entrypoint)); err != nil {
		return err
	}
	if checkDependencies {
		for _, d := range m.Dependencies {
			if _, err := exec.LookPath(d); err != nil {
				return errors.New("missing plugin dependency: " + d)
			}
		}
	}
	return nil
}

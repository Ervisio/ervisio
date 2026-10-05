package sys

import (
	"bufio"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// OSRelease holds the fields of /etc/os-release we use.
type OSRelease struct {
	ID         string   `json:"id"`
	IDLike     []string `json:"idLike,omitempty"`
	Name       string   `json:"name"`
	PrettyName string   `json:"prettyName"`
	VersionID  string   `json:"versionId,omitempty"`
	Logo       string   `json:"logo,omitempty"`
	ANSIColor  string   `json:"ansiColor,omitempty"`
}

// ParseOSRelease parses os-release(5) content.
func ParseOSRelease(r io.Reader) OSRelease {
	vals := map[string]string{}
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || line[0] == '#' {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		vals[strings.TrimSpace(k)] = unquote(strings.TrimSpace(v))
	}
	o := OSRelease{
		ID:         strings.ToLower(vals["ID"]),
		Name:       vals["NAME"],
		PrettyName: vals["PRETTY_NAME"],
		VersionID:  vals["VERSION_ID"],
		Logo:       vals["LOGO"],
		ANSIColor:  vals["ANSI_COLOR"],
	}
	if like := strings.Fields(strings.ToLower(vals["ID_LIKE"])); len(like) > 0 {
		o.IDLike = like
	}
	if o.ID == "" {
		o.ID = "linux"
	}
	if o.Name == "" {
		o.Name = "Linux"
	}
	if o.PrettyName == "" {
		o.PrettyName = o.Name
	}
	return o
}

// unquote handles the shell-like quoting allowed by os-release(5).
func unquote(v string) string {
	if len(v) >= 2 && (v[0] == '"' || v[0] == '\'') && v[len(v)-1] == v[0] {
		q := v[0]
		v = v[1 : len(v)-1]
		if q == '\'' {
			return v
		}
		var sb strings.Builder
		for i := 0; i < len(v); i++ {
			if v[i] == '\\' && i+1 < len(v) && strings.IndexByte("\\$\"`", v[i+1]) >= 0 {
				i++
			}
			sb.WriteByte(v[i])
		}
		return sb.String()
	}
	return v
}

// distroColors maps os-release IDs to brand colours.
var distroColors = map[string]string{
	"windows":     windowsBrandColor,
	"arch":        "#1793D1",
	"ubuntu":      "#E95420",
	"linuxmint":   "#87CF3E",
	"fedora":      "#51A2DA",
	"debian":      "#D70A53",
	"manjaro":     "#35BF5C",
	"pop":         "#48B9C7",
	"endeavouros": "#7F3FBF",
}

// DefaultDistroColor is used for unknown distributions.
const DefaultDistroColor = "#3AB4F2"

// DistroColor returns the brand colour for an os-release ID.
func DistroColor(id string) string {
	id = strings.ToLower(id)
	if c, ok := distroColors[id]; ok {
		return c
	}
	if strings.HasPrefix(id, "opensuse") {
		return "#73BA25"
	}
	return DefaultDistroColor
}

// FindLogo looks for the icon named by os-release LOGO in the usual places
// and returns its path, or "" when none exists. Only plain icon names are
// accepted.
func FindLogo(name string) string {
	if name == "" || strings.ContainsAny(name, "/\\") || strings.HasPrefix(name, ".") || len(name) > 128 {
		return ""
	}
	var candidates []string
	for _, ext := range []string{".svg", ".png"} {
		candidates = append(candidates, filepath.Join("/usr/share/pixmaps", name+ext))
	}
	candidates = append(candidates, filepath.Join("/usr/share/icons/hicolor/scalable/apps", name+".svg"))
	for _, size := range []string{"512x512", "256x256", "128x128", "64x64", "48x48"} {
		candidates = append(candidates, filepath.Join("/usr/share/icons/hicolor", size, "apps", name+".png"))
	}
	for _, p := range candidates {
		if fi, err := os.Stat(p); err == nil && fi.Mode().IsRegular() {
			return p
		}
	}
	return ""
}

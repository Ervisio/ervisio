package software

import (
	"context"
	"strings"
	"time"

	"github.com/ervisio/ervisio/server/internal/rpc"
)

// winget manages packages with the Windows Package Manager. Output is text
// (tables); see winget_parse.go. It is detected by `winget` in PATH, so it is
// never found on other systems.
type winget struct{}

func newWinget() *winget { return &winget{} }

func (w *winget) Name() string    { return "winget" }
func (w *winget) Kind() string    { return KindRepo }
func (w *winget) Available() bool { return have("winget") }

// Flags that keep winget from ever prompting.
var wingetCommon = []string{"--accept-source-agreements", "--disable-interactivity"}

func wingetArgs(verb string, extra ...string) []string {
	return append(append([]string{verb}, extra...), wingetCommon...)
}

func (w *winget) ListInstalled(ctx context.Context) ([]Package, error) {
	out, err := run(ctx, 2*time.Minute, nil, "winget", wingetArgs("list")...)
	if err != nil && out == "" {
		return nil, err
	}
	return parseWingetList(out), nil
}

func (w *winget) ListUpdates(ctx context.Context) ([]Update, error) {
	out, err := run(ctx, 3*time.Minute, nil, "winget", wingetArgs("upgrade")...)
	if err != nil && out == "" {
		return nil, err
	}
	return parseWingetUpgrades(out), nil
}

func (w *winget) Refresh(ctx context.Context) error {
	_, err := run(ctx, 3*time.Minute, nil, "winget", "source", "update", "--disable-interactivity")
	return err
}

func (w *winget) Search(ctx context.Context, q string) ([]Result, error) {
	q = strings.TrimSpace(q)
	if q == "" {
		return nil, nil
	}
	out, err := run(ctx, 2*time.Minute, nil, "winget", wingetArgs("search", "--query", q)...)
	if err != nil && out == "" {
		return nil, err // exit 1 with a table-less message = no match
	}
	set := map[string]bool{}
	if ins, e := w.ListInstalled(ctx); e == nil {
		for _, p := range ins {
			set[p.Name] = true
		}
	}
	return parseWingetSearch(out, set), nil
}

func (w *winget) Info(ctx context.Context, name string) (*Detail, error) {
	if err := validNames([]string{name}); err != nil {
		return nil, err
	}
	out, err := run(ctx, time.Minute, nil, "winget", wingetArgs("show", "--id", name, "-e")...)
	if err != nil && out == "" {
		return nil, rpc.Errorf(rpc.NotFound, "No winget package called %q was found.", name)
	}
	title, fl := parseWingetShow(out)
	if len(fl) == 0 && title == "" {
		return nil, rpc.Errorf(rpc.NotFound, "No winget package called %q was found.", name)
	}
	if title != "" {
		fl = append([]Field{{"Name", title}}, fl...)
	}
	installed := false
	if ins, e := w.ListInstalled(ctx); e == nil {
		for _, p := range ins {
			installed = installed || p.Name == name
		}
	}
	return &Detail{Name: name, Source: "winget", Kind: KindRepo, Installed: installed,
		Version: fieldValue(fl, "Version"), Description: fieldValue(fl, "Description", "Short Description"), Fields: fl}, nil
}

var wingetAgree = []string{"--silent", "--accept-package-agreements", "--accept-source-agreements", "--disable-interactivity"}

func (w *winget) perPackage(title, verb string, pkgs []string, flags []string) Plan {
	var steps []Step
	for _, p := range pkgs {
		args := append([]string{verb, "--id", p, "-e"}, flags...)
		steps = append(steps, Step{Title: title, Name: "winget", Args: args, Parse: parseWingetLine})
	}
	return Plan{Steps: steps}
}

func (w *winget) Install(pkgs []string) (Plan, error) {
	if err := validNames(pkgs); err != nil || len(pkgs) == 0 {
		return Plan{}, orInvalid(err)
	}
	return w.perPackage("install", "install", pkgs, wingetAgree), nil
}

func (w *winget) Remove(pkgs []string) (Plan, error) {
	if err := validNames(pkgs); err != nil || len(pkgs) == 0 {
		return Plan{}, orInvalid(err)
	}
	return w.perPackage("remove", "uninstall", pkgs, []string{"--silent", "--accept-source-agreements", "--disable-interactivity"}), nil
}

func (w *winget) Upgrade(pkgs []string) (Plan, error) {
	if err := validNames(pkgs); err != nil {
		return Plan{}, err
	}
	if len(pkgs) == 0 {
		return Plan{Steps: []Step{{Title: "upgrade", Name: "winget", Args: append([]string{"upgrade", "--all"}, wingetAgree...), Parse: parseWingetLine}}}, nil
	}
	return w.perPackage("upgrade", "upgrade", pkgs, wingetAgree), nil
}

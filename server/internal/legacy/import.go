package legacy

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/ervisio/ervisio/server/internal/brand"
)

// Import copies LinuxAdmin's system data to the Ervisio locations. A
// location that already holds Ervisio data is left alone; LinuxAdmin's
// files are never changed.
type Import struct {
	Paths Paths
	// Mark leaves an import marker in the copied folders and records what
	// was created, so Undo can take it back; Finalize ends that. Without
	// Mark the import is final at once.
	Mark bool
	Logf func(format string, args ...any)
}

// Result says what an Import copied.
type Result struct {
	Config  bool     `json:"config"`
	State   bool     `json:"state"`
	PAM     bool     `json:"pam"`
	DropIns []string `json:"dropIns,omitempty"`
	// Created lists the paths the import created, for Undo.
	Created []string `json:"created,omitempty"`
}

// Any reports whether anything was copied.
func (r *Result) Any() bool {
	return r.Config || r.State || r.PAM || len(r.DropIns) > 0
}

func (im *Import) logf(format string, args ...any) {
	if im.Logf != nil {
		im.Logf(format, args...)
	}
}

// Run imports what is not imported yet. It can be repeated: a folder left
// by an interrupted marked import is copied again.
func (im *Import) Run() (*Result, error) {
	res := &Result{}
	p := im.Paths
	ok, err := im.copyDir(brand.LegacyConfigDir, brand.ConfigDir, skipConfig, editConfig)
	if err != nil {
		return res, err
	}
	if ok {
		res.Config = true
		res.Created = append(res.Created, p.At(brand.ConfigDir))
		im.logf("copied %s to %s", brand.LegacyConfigDir, brand.ConfigDir)
	}
	ok, err = im.copyDir(brand.LegacyStateDir, brand.StateDir, skipState, nil)
	if err != nil {
		return res, err
	}
	if ok {
		res.State = true
		res.Created = append(res.Created, p.At(brand.StateDir))
		im.logf("copied %s to %s", brand.LegacyStateDir, brand.StateDir)
	}
	if ok, err := im.pam(); err != nil {
		return res, err
	} else if ok {
		res.PAM = true
		res.Created = append(res.Created, p.At(pamDir+"/"+brand.PAMService))
		im.logf("wrote %s/%s from %s/%s", pamDir, brand.PAMService, pamDir, brand.LegacyPAMService)
	}
	created, err := im.dropIns()
	for _, f := range created {
		res.DropIns = append(res.DropIns, filepath.Base(f))
		res.Created = append(res.Created, f)
	}
	if len(created) > 0 {
		im.logf("copied the drop-ins of %s: %s", brand.LegacyServiceUnit, strings.Join(res.DropIns, ", "))
	}
	return res, err
}

// copyDir copies the folder old to new unless new already holds Ervisio
// data (files, and no import marker of an unfinished import).
func (im *Import) copyDir(old, new string, skip func(string) bool, edit func(string) error) (bool, error) {
	oldP, newP := im.Paths.At(old), im.Paths.At(new)
	removeStale(newP)
	if !isDir(oldP) {
		return false, nil
	}
	if exists(newP) {
		if !isDir(newP) {
			return false, fmt.Errorf("%s exists and is not a folder", newP)
		}
		if hasFiles(newP) && !exists(filepath.Join(newP, importMarker)) {
			return false, nil // Ervisio data: keep it
		}
	}
	err := installTree(oldP, newP, skip, func(tmp string) error {
		if edit != nil {
			if err := edit(tmp); err != nil {
				return err
			}
		}
		if im.Mark {
			return os.WriteFile(filepath.Join(tmp, importMarker), []byte("Copied from "+old+" by "+brand.DaemonBinary+"; removed when the move to "+brand.Name+" is complete.\n"), 0o600)
		}
		return nil
	})
	return err == nil, err
}

func skipConfig(rel string) bool { return rel == NoteFile || rel == importMarker }

func skipState(rel string) bool {
	switch rel {
	case "updates/staging", "updates/lock", importMarker:
		return true
	}
	return false
}

// editConfig renames linuxadmin.conf (and its backups) to ervisio.conf
// and points paths inside /etc/linuxadmin to the copy in /etc/ervisio.
func editConfig(dir string) error {
	ents, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	oldBase := filepath.Base(brand.LegacyConfigPath)
	newBase := filepath.Base(brand.ConfigPath)
	for _, e := range ents {
		if rest, ok := strings.CutPrefix(e.Name(), oldBase); ok && e.Type().IsRegular() {
			if err := os.Rename(filepath.Join(dir, e.Name()), filepath.Join(dir, newBase+rest)); err != nil {
				return err
			}
		}
	}
	conf := filepath.Join(dir, newBase)
	b, err := os.ReadFile(conf)
	if notExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	fi, err := os.Stat(conf)
	if err != nil {
		return err
	}
	out := rewriteConfig(b)
	if bytes.Equal(out, b) {
		return nil
	}
	return writeFileAtomic(conf, out, fi.Mode().Perm())
}

// rewriteConfig moves paths under /etc/linuxadmin/ to /etc/ervisio/ (the
// folder is copied whole) and renames the product in comments.
func rewriteConfig(b []byte) []byte {
	lines := strings.SplitAfter(string(b), "\n")
	for i, l := range lines {
		l = strings.ReplaceAll(l, brand.LegacyConfigDir+"/", brand.ConfigDir+"/")
		if strings.HasPrefix(strings.TrimSpace(l), "#") {
			l = strings.ReplaceAll(l, "systemctl restart "+brand.LegacySlug, "systemctl restart "+brand.Slug)
			l = strings.ReplaceAll(l, filepath.Base(brand.LegacyConfigPath), filepath.Base(brand.ConfigPath))
			l = strings.ReplaceAll(l, brand.LegacyName, brand.Name)
		}
		lines[i] = l
	}
	return []byte(strings.Join(lines, ""))
}

// legacyPAMHeader is how the PAM files written by LinuxAdmin start.
const legacyPAMHeader = "PAM service for " + brand.LegacyName

// pam writes /etc/pam.d/ervisio from /etc/pam.d/linuxadmin when the first
// does not exist. A file LinuxAdmin wrote gets the new name in its comments;
// one the administrator wrote is copied as it is.
func (im *Import) pam() (bool, error) {
	oldP := im.Paths.At(pamDir + "/" + brand.LegacyPAMService)
	newP := im.Paths.At(pamDir + "/" + brand.PAMService)
	if exists(newP) || !isRegular(oldP) {
		return false, nil
	}
	b, err := os.ReadFile(oldP)
	if err != nil {
		return false, err
	}
	if bytes.Contains(b, []byte(legacyPAMHeader)) {
		b = RewritePAM(b)
	}
	if err := writeFileAtomic(newP, b, 0o644); err != nil {
		return false, err
	}
	return true, nil
}

// RewritePAM renames the product in the comments of a PAM file LinuxAdmin
// wrote (the rules stay as they are).
func RewritePAM(b []byte) []byte {
	lines := strings.SplitAfter(string(b), "\n")
	for i, l := range lines {
		if strings.HasPrefix(l, "#") {
			l = strings.ReplaceAll(l, pamDir+"/"+brand.LegacyPAMService, pamDir+"/"+brand.PAMService)
			l = strings.ReplaceAll(l, brand.LegacyDaemonBinary, brand.DaemonBinary)
			l = strings.ReplaceAll(l, brand.LegacyName, brand.Name)
		}
		lines[i] = l
	}
	return []byte(strings.Join(lines, ""))
}

// dropIns copies the *.conf drop-ins of linuxadmin.service (the Docker
// bridge ordering written by install.sh, overrides made in Services) to
// ervisio.service.d, without replacing any that exist.
func (im *Import) dropIns() ([]string, error) {
	oldD, newD := im.Paths.At(legacyDropInDir), im.Paths.At(newDropInDir)
	ents, err := os.ReadDir(oldD)
	if err != nil {
		return nil, nil
	}
	sort.Slice(ents, func(i, j int) bool { return ents[i].Name() < ents[j].Name() })
	var created []string
	for _, e := range ents {
		if !e.Type().IsRegular() || !strings.HasSuffix(e.Name(), ".conf") {
			continue
		}
		dst := filepath.Join(newD, e.Name())
		if exists(dst) {
			continue
		}
		b, err := os.ReadFile(filepath.Join(oldD, e.Name()))
		if err != nil {
			return created, err
		}
		lines := strings.SplitAfter(string(b), "\n")
		for i, l := range lines {
			if strings.HasPrefix(l, "#") {
				lines[i] = strings.ReplaceAll(l, brand.LegacyName, brand.Name)
			}
		}
		if err := writeFileAtomic(dst, []byte(strings.Join(lines, "")), 0o644); err != nil {
			return created, err
		}
		created = append(created, dst)
	}
	return created, nil
}

// Undo removes what a marked import created: folders that still carry the
// marker, and the files it wrote.
func Undo(p Paths, r *Result) {
	if r == nil {
		return
	}
	for i := len(r.Created) - 1; i >= 0; i-- {
		c := r.Created[i]
		fi, err := os.Lstat(c)
		if err != nil {
			continue
		}
		if fi.IsDir() {
			if exists(filepath.Join(c, importMarker)) {
				os.RemoveAll(c)
			}
			continue
		}
		os.Remove(c)
	}
	os.Remove(p.At(newDropInDir)) // only when empty
}

// Pending reports whether a marked import has not been finalized.
func Pending(p Paths) bool {
	return exists(filepath.Join(p.At(brand.ConfigDir), importMarker)) ||
		exists(filepath.Join(p.At(brand.StateDir), importMarker))
}

// Finalize makes a marked import permanent and leaves a note in the
// LinuxAdmin configuration folder.
func Finalize(p Paths) error {
	for _, d := range []string{brand.ConfigDir, brand.StateDir} {
		if err := os.Remove(filepath.Join(p.At(d), importMarker)); err != nil && !notExist(err) {
			return err
		}
	}
	return WriteNote(p)
}

// WriteNote explains in /etc/linuxadmin where everything went (once).
func WriteNote(p Paths) error {
	dir := p.At(brand.LegacyConfigDir)
	if !isDir(dir) {
		return nil
	}
	f := filepath.Join(dir, NoteFile)
	if exists(f) {
		return nil
	}
	return writeFileAtomic(f, []byte(noteText), 0o644)
}

var noteText = brand.LegacyName + " is now " + brand.Name + `.

This folder is no longer read. Its contents were copied to:

  ` + brand.ConfigPath + `      (was ` + brand.LegacyConfigPath + `)
  ` + brand.ConfigDir + `/tls              (the same certificate)
  ` + brand.StateDir + `                (was ` + brand.LegacyStateDir + `: plugins, update state)
  /etc/pam.d/` + brand.PAMService + `            (was /etc/pam.d/` + brand.LegacyPAMService + `)

and the service is ` + brand.ServiceUnit + ` (was ` + brand.LegacyServiceUnit + `).

` + brand.LegacyName + `'s files are kept so you can go back to it:

  systemctl disable --now ` + brand.ServiceUnit + ` && systemctl enable --now ` + brand.LegacyServiceUnit + `

Once ` + brand.Name + ` works for you, remove them with:

  ` + brand.DaemonBinary + ` --remove-legacy            (programs and unit; keeps this folder and ` + brand.LegacyStateDir + `)
  ` + brand.DaemonBinary + ` --remove-legacy --purge    (everything, this folder included)

Packaged installs (.deb, .rpm, AUR) are removed by the package manager instead.
See ` + brand.RepoURL + `/blob/main/docs/RELEASING.md#rename-transition
`

package software

import (
	"regexp"
	"strconv"
	"strings"
	"unicode"
)

// Platform-neutral parsers for winget's text output.

var wingetANSIRe = regexp.MustCompile(`\x1b\[[0-9;?]*[A-Za-z]`)

// cleanWingetOutput removes what the progress spinner and progress bars leave in
// captured output: ANSI sequences, backspaces and everything before the last
// carriage return of a line.
func cleanWingetOutput(out string) []string {
	out = wingetANSIRe.ReplaceAllString(out, "")
	out = strings.ReplaceAll(out, "\r\n", "\n")
	var lines []string
	for _, l := range strings.Split(out, "\n") {
		if i := strings.LastIndex(l, "\r"); i >= 0 {
			l = l[i+1:]
		}
		l = strings.ReplaceAll(l, "\b", "")
		lines = append(lines, strings.TrimRight(l, " \t"))
	}
	return lines
}

var spinnerRe = regexp.MustCompile(`^[-\\|/]\s*`)

func isSeparator(l string) bool {
	l = strings.TrimSpace(l)
	return len(l) >= 3 && strings.Trim(l, "-") == ""
}

// runeWidth is the number of terminal cells r occupies (winget pads by cells).
func runeWidth(r rune) int {
	switch {
	case r == 0 || unicode.Is(unicode.Mn, r) || unicode.Is(unicode.Me, r) || r == 0x200b || r == 0x200d:
		return 0
	case r >= 0x1100 && (r <= 0x115f || r == 0x2329 || r == 0x232a ||
		(r >= 0x2e80 && r <= 0xa4cf && r != 0x303f) ||
		(r >= 0xac00 && r <= 0xd7a3) ||
		(r >= 0xf900 && r <= 0xfaff) ||
		(r >= 0xfe30 && r <= 0xfe6f) ||
		(r >= 0xff00 && r <= 0xff60) ||
		(r >= 0xffe0 && r <= 0xffe6) ||
		(r >= 0x1f300 && r <= 0x1f64f) ||
		(r >= 0x1f900 && r <= 0x1f9ff) ||
		(r >= 0x20000 && r <= 0x3fffd)):
		return 2
	}
	return 1
}

// cellSlice returns the part of s between display columns [from, to) (to < 0 = end).
func cellSlice(s string, from, to int) string {
	var b strings.Builder
	col := 0
	for _, r := range s {
		w := runeWidth(r)
		if col >= from && (to < 0 || col < to) {
			b.WriteRune(r)
		}
		col += w
		if to >= 0 && col >= to {
			break
		}
	}
	return strings.TrimSpace(b.String())
}

// columnStarts returns the display column where each header word group starts.
// Header titles are single words, so every word starts a column.
func columnStarts(header string) []int {
	var starts []int
	col, prevSpaces := 0, 1
	for _, r := range header {
		if r == ' ' {
			prevSpaces++
		} else {
			if prevSpaces >= 1 {
				starts = append(starts, col)
			}
			prevSpaces = 0
		}
		col += runeWidth(r)
	}
	return starts
}

// wingetTable parses every table of winget output into rows of cells (one per
// header column, positional). A table is a header line, a dashes line and rows
// up to the next blank line.
func wingetTable(out string) [][]string {
	lines := cleanWingetOutput(out)
	var rows [][]string
	for i := 1; i < len(lines); i++ {
		if !isSeparator(lines[i]) {
			continue
		}
		header := lines[i-1]
		if strings.TrimSpace(header) == "" {
			continue
		}
		if !strings.HasPrefix(header, "Name") && !strings.HasPrefix(header, "Id") {
			header = spinnerRe.ReplaceAllString(header, "") // a spinner glyph before the header
			if !isHeaderWord(header) {
				// localized header: keep it as is, unless it starts with a lone spinner glyph
				header = lines[i-1]
			}
		}
		starts := columnStarts(header)
		if len(starts) < 2 {
			continue
		}
		for i++; i < len(lines); i++ {
			l := lines[i]
			if strings.TrimSpace(l) == "" {
				break
			}
			if isSeparator(l) {
				i--
				break
			}
			row := make([]string, len(starts))
			for c := range starts {
				end := -1
				if c+1 < len(starts) {
					end = starts[c+1]
				}
				row[c] = strings.TrimSpace(strings.TrimSuffix(cellSlice(l, starts[c], end), "…"))
			}
			rows = append(rows, row)
		}
	}
	return rows
}

func isHeaderWord(h string) bool { return strings.HasPrefix(h, "Name") || strings.HasPrefix(h, "Id") }

var wingetIDRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._+-]*$`)

// parseWingetList parses `winget list`: Name, Id, Version, [Available,] [Source].
// Entries without a winget id (Add/Remove Programs only) are kept with the id
// shown by winget; they cannot be managed by id.
func parseWingetList(out string) []Package {
	var pkgs []Package
	for _, r := range wingetTable(out) {
		if len(r) < 3 || r[1] == "" || strings.Contains(r[1], " ") {
			continue
		}
		src := "winget"
		if len(r) >= 5 && r[4] != "" {
			src = r[4]
		}
		pkgs = append(pkgs, Package{Name: r[1], Title: r[0], Version: r[2], Source: src, Kind: KindRepo, Reason: "explicit"})
	}
	return pkgs
}

// parseWingetUpgrades parses `winget upgrade`: Name, Id, Version, Available, Source.
func parseWingetUpgrades(out string) []Update {
	var ups []Update
	seen := map[string]bool{}
	for _, r := range wingetTable(out) {
		if len(r) < 4 || r[1] == "" || r[3] == "" || strings.Contains(r[1], " ") || seen[r[1]] {
			continue
		}
		seen[r[1]] = true
		src := "winget"
		if len(r) >= 5 && r[4] != "" {
			src = r[4]
		}
		ups = append(ups, Update{Name: r[1], Title: r[0], From: r[2], To: r[3], Source: src, Kind: KindRepo, Notes: []string{}})
	}
	return ups
}

// parseWingetSearch parses `winget search`: Name, Id, Version, [Match,] [Source].
func parseWingetSearch(out string, installed map[string]bool) []Result {
	var res []Result
	for _, r := range wingetTable(out) {
		if len(r) < 3 || !wingetIDRe.MatchString(r[1]) {
			continue
		}
		src := "winget"
		if len(r) >= 4 && r[len(r)-1] != "" {
			src = r[len(r)-1]
		}
		res = append(res, Result{Name: r[1], Title: r[0], Version: r[2], Source: src, Kind: KindRepo, Installed: installed[r[1]]})
		if len(res) >= 40 {
			break
		}
	}
	return res
}

var wingetStepRe = regexp.MustCompile(`^\((\d+)/(\d+)\)\s+Found\s+(.+?)\s+\[`)

// parseWingetLine reads "(2/5) Found Git [Git.Git] Version 2.50" lines of upgrade --all.
func parseWingetLine(line string, p *Progress) bool {
	if g := wingetStepRe.FindStringSubmatch(strings.TrimSpace(line)); g != nil {
		n, _ := strconv.Atoi(g[1])
		t, _ := strconv.Atoi(g[2])
		p.Done, p.Total, p.Current = n-1, t, g[3]
		return true
	}
	return false
}

// parseWingetShow parses `winget show` into fields: the "Found Name [Id]" line
// gives the title; the key/value stanza stops at the Installer section.
func parseWingetShow(out string) (title string, fields []Field) {
	lines := cleanWingetOutput(out)
	var body []string
	for _, l := range lines {
		t := strings.TrimSpace(l)
		if strings.HasPrefix(t, "Found ") && title == "" {
			if i := strings.LastIndex(t, " ["); i > 6 {
				title = t[6:i]
			}
			continue
		}
		if len(l) > 0 && l[0] != ' ' && strings.HasSuffix(t, ":") && len(body) > 0 && (t == "Installer:" || t == "Installers:") {
			break
		}
		body = append(body, l)
	}
	// "Key:" alone followed by indented lines is a multi-line value: fold it into one line.
	var folded []string
	for i := 0; i < len(body); i++ {
		l := body[i]
		if len(l) > 0 && l[0] != ' ' && strings.HasSuffix(l, ":") {
			var parts []string
			for i+1 < len(body) && strings.HasPrefix(body[i+1], " ") && strings.TrimSpace(body[i+1]) != "" {
				i++
				parts = append(parts, strings.TrimSpace(body[i]))
			}
			if len(parts) > 0 {
				l += " " + strings.Join(parts, " ")
			}
		}
		folded = append(folded, l)
	}
	return title, parseKV(strings.TrimLeft(strings.Join(folded, "\n"), "\n"))
}

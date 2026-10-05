package software

import "testing"

const wingetListFixture = "   - \r   \\ \r   | \r" +
	"Name                           Id                         Version        Available      Source\r\n" +
	"---------------------------------------------------------------------------------------------\r\n" +
	"Git                            Git.Git                    2.45.1         2.46.0         winget\r\n" +
	"Visual Studio Code             Microsoft.VisualStudioCode 1.90.0         1.91.1         winget\r\n" +
	"微信                           Tencent.WeChat             3.9.11                        winget\r\n" +
	"Microsoft Edge WebView2 Runti… ARP\\Machine\\X64\\Edge       126.0.2592.68\r\n" +
	"Notepad++ (64-bit x64)         Notepad++.Notepad++        8.6.9          8.7            winget\r\n" +
	"3 upgrades available.\r\n"

func TestParseWingetList(t *testing.T) {
	pk := parseWingetList(wingetListFixture)
	if len(pk) != 5 {
		t.Fatalf("got %d packages: %+v", len(pk), pk)
	}
	if pk[0].Name != "Git.Git" || pk[0].Title != "Git" || pk[0].Version != "2.45.1" || pk[0].Source != "winget" {
		t.Errorf("git: %+v", pk[0])
	}
	if pk[1].Name != "Microsoft.VisualStudioCode" || pk[1].Title != "Visual Studio Code" {
		t.Errorf("vscode: %+v", pk[1])
	}
	if pk[2].Title != "微信" || pk[2].Name != "Tencent.WeChat" || pk[2].Version != "3.9.11" {
		t.Errorf("wide chars: %+v", pk[2])
	}
	if pk[3].Name != "ARP\\Machine\\X64\\Edge" || pk[3].Source != "winget" {
		t.Errorf("arp: %+v", pk[3])
	}
	if pk[4].Title != "Notepad++ (64-bit x64)" {
		t.Errorf("name with spaces: %+v", pk[4])
	}
}

func TestParseWingetUpgrades(t *testing.T) {
	out := wingetListFixture + "\r\nThe following packages have an upgrade available, but require explicit targeting for upgrade:\r\n" +
		"Name  Id        Version Available Source\r\n" +
		"-----------------------------------------\r\n" +
		"Foo   Foo.Bar   1.0     1.1       winget\r\n" +
		"1 upgrades available.\r\n"
	ups := parseWingetUpgrades(out)
	if len(ups) != 4 {
		t.Fatalf("got %d: %+v", len(ups), ups)
	}
	u := ups[1]
	if u.Name != "Microsoft.VisualStudioCode" || u.From != "1.90.0" || u.To != "1.91.1" || u.Kind != KindRepo {
		t.Errorf("%+v", u)
	}
	if ups[3].Name != "Foo.Bar" || ups[3].To != "1.1" {
		t.Errorf("second table: %+v", ups[3])
	}
	if got := parseWingetUpgrades("No installed package found matching input criteria.\r\n"); len(got) != 0 {
		t.Errorf("empty: %+v", got)
	}
}

func TestParseWingetSearch(t *testing.T) {
	out := "-\r\\\r|\r" +
		"Name       Id                Version Match      Source\r\n" +
		"-----------------------------------------------------\r\n" +
		"Git        Git.Git           2.46.0  Tag: git   winget\r\n" +
		"GitHub CLI GitHub.cli        2.52.0             winget\r\n"
	res := parseWingetSearch(out, map[string]bool{"Git.Git": true})
	if len(res) != 2 || res[0].Name != "Git.Git" || !res[0].Installed || res[1].Title != "GitHub CLI" || res[1].Source != "winget" {
		t.Errorf("%+v", res)
	}
}

func TestParseWingetLine(t *testing.T) {
	var p Progress
	if !parseWingetLine("(2/5) Found Git [Git.Git] Version 2.46.0", &p) || p.Done != 1 || p.Total != 5 || p.Current != "Git" {
		t.Errorf("%+v", p)
	}
	if parseWingetLine("  ██████▒▒  50%", &p) {
		t.Error("bar should not parse")
	}
}

func TestParseWingetShow(t *testing.T) {
	out := "Found Git [Git.Git]\r\nVersion: 2.46.0\r\nPublisher: The Git Development Community\r\nDescription:\r\n  Git for Windows\r\n  more\r\nInstaller:\r\n  Type: inno\r\n"
	title, f := parseWingetShow(out)
	if title != "Git" || fieldValue(f, "Version") != "2.46.0" || fieldValue(f, "Description") != "Git for Windows more" || fieldValue(f, "Type") != "" {
		t.Errorf("%q %+v", title, f)
	}
}

func TestWingetPlans(t *testing.T) {
	w := newWinget()
	p, err := w.Install([]string{"Git.Git"})
	if err != nil || len(p.Steps) != 1 || p.Steps[0].Args[0] != "install" || p.Steps[0].Args[1] != "--id" {
		t.Fatalf("%+v %v", p, err)
	}
	if _, err := w.Install([]string{"--force"}); err == nil {
		t.Error("option-like name accepted")
	}
	p, _ = w.Upgrade(nil)
	if p.Steps[0].Args[1] != "--all" {
		t.Errorf("%+v", p)
	}
	p, _ = w.Remove([]string{"A.B", "C.D"})
	if len(p.Steps) != 2 || p.Steps[1].Args[0] != "uninstall" {
		t.Errorf("%+v", p)
	}
}

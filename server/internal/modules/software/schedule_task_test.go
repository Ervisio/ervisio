package software

import (
	"strings"
	"testing"
)

func TestTaskXMLRoundTrip(t *testing.T) {
	x := renderTaskXML("03:30", []taskAction{{Path: `C:\Program Files\winget.exe`, Args: []string{"upgrade", "--all", "a b"}}})
	for _, w := range []string{"S-1-5-18", "<StartBoundary>2024-01-01T03:30:00", `<Command>C:\Program Files\winget.exe</Command>`, `<Arguments>upgrade --all &#34;a b&#34;</Arguments>`, "StartWhenAvailable"} {
		if !strings.Contains(x, w) {
			t.Errorf("missing %q in\n%s", w, x)
		}
	}
	if got := parseTaskTime(x); got != "03:30" {
		t.Errorf("got %q", got)
	}
	if parseTaskTime("<Task/>") != "" {
		t.Error("no time expected")
	}
}

func TestWinQuoteAndUTF16(t *testing.T) {
	if got := winQuote(`a\"b`); got != `"a\\\"b"` {
		t.Errorf("got %s", got)
	}
	if got := winQuote(`C:\x y\`); got != `"C:\x y\\"` {
		t.Errorf("got %s", got)
	}
	if got := decodeText(encodeUTF16("héllo <x>")); got != "héllo <x>" {
		t.Errorf("got %q", got)
	}
	if !isAccessDenied("ERROR: Access is denied.") || isAccessDenied("ERROR: not found") {
		t.Error("access denied detection")
	}
}

// Command devclient is a tiny development client for ervisiod: it makes
// /api/rpc calls and opens WebSocket streams, printing the JSON it gets.
// Against `ervisiod --dev --dev-insecure-noauth`, redeem the one-time
// URL the daemon prints once with `login`, then pass the printed token with
// -cookie (or in $ERVISIO_SESSION):
//
//	go run ./tools/devclient login 'http://127.0.0.1:9090/api/dev/noauth?token=…'
//	export ERVISIO_SESSION=<printed token>
//	go run ./tools/devclient rpc system.host
//	go run ./tools/devclient rpc prefs.set '{"key":"theme","value":"oled"}'
//	go run ./tools/devclient -n 3 stream system.metricsStream '{"interval":500}'
//	go run ./tools/devclient -admin rpc system.power '{"action":"reboot"}'
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/coder/websocket"
)

func main() {
	base := flag.String("url", "http://127.0.0.1:9090", "daemon base URL")
	cookie := flag.String("cookie", os.Getenv("ERVISIO_SESSION"), "ervisio_session token (default $ERVISIO_SESSION; get one with `login <noauth-url>`)")
	admin := flag.Bool("admin", false, "send admin:true")
	n := flag.Int("n", 0, "stream: stop after n data frames (0 = until end / Ctrl+C)")
	timeout := flag.Duration("timeout", 30*time.Second, "overall timeout")
	flag.Usage = func() {
		fmt.Fprintln(os.Stderr, "usage: devclient [flags] rpc|stream <method> [params-json]\n       devclient login <one-time noauth URL>")
		flag.PrintDefaults()
	}
	flag.Parse()
	if flag.NArg() < 2 {
		flag.Usage()
		os.Exit(2)
	}
	mode, method := flag.Arg(0), flag.Arg(1)
	if mode == "login" {
		login(method)
		return
	}
	params := json.RawMessage("{}")
	if flag.NArg() > 2 {
		params = json.RawMessage(flag.Arg(2))
		if !json.Valid(params) {
			fail("params are not valid JSON")
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()
	hdr := http.Header{}
	hdr.Set("X-Requested-With", "ervisio")
	if *cookie != "" {
		hdr.Set("Cookie", "ervisio_session="+*cookie)
	}
	switch mode {
	case "rpc":
		body, _ := json.Marshal(map[string]any{"method": method, "params": params, "admin": *admin})
		req, _ := http.NewRequestWithContext(ctx, http.MethodPost, *base+"/api/rpc", bytes.NewReader(body))
		req.Header = hdr
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			fail(err.Error())
		}
		defer resp.Body.Close()
		out, _ := io.ReadAll(resp.Body)
		fmt.Printf("HTTP %d\n%s", resp.StatusCode, out)
	case "stream":
		u, _ := url.Parse(*base)
		u.Scheme = strings.Replace(u.Scheme, "http", "ws", 1)
		u.Path = "/api/ws"
		c, _, err := websocket.Dial(ctx, u.String(), &websocket.DialOptions{HTTPHeader: hdr})
		if err != nil {
			fail(err.Error())
		}
		defer c.CloseNow()
		open, _ := json.Marshal(map[string]any{"ch": 1, "op": "open", "method": method, "params": params, "admin": *admin})
		if err := c.Write(ctx, websocket.MessageText, open); err != nil {
			fail(err.Error())
		}
		got := 0
		for {
			_, data, err := c.Read(ctx)
			if err != nil {
				fail(err.Error())
			}
			fmt.Println(string(data))
			var f struct{ Op string }
			_ = json.Unmarshal(data, &f)
			if f.Op == "end" || f.Op == "error" {
				return
			}
			got++
			if *n > 0 && got >= *n {
				closeMsg, _ := json.Marshal(map[string]any{"ch": 1, "op": "close"})
				_ = c.Write(ctx, websocket.MessageText, closeMsg)
				*n = 0 // read until "end"
			}
		}
	default:
		flag.Usage()
		os.Exit(2)
	}
}

// login redeems a --dev-insecure-noauth one-time URL and prints the
// session token.
func login(u string) {
	c := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := c.Get(u)
	if err != nil {
		fail(err.Error())
	}
	resp.Body.Close()
	for _, ck := range resp.Cookies() {
		if ck.Name == "ervisio_session" && ck.Value != "" {
			fmt.Println(ck.Value)
			return
		}
	}
	fail(fmt.Sprintf("no session cookie (HTTP %d): the token is wrong or was already used", resp.StatusCode))
}

func fail(msg string) {
	fmt.Fprintln(os.Stderr, "devclient:", msg)
	os.Exit(1)
}

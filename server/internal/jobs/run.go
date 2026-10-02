package jobs

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/ervisio/ervisio/server/internal/audit"
	"github.com/ervisio/ervisio/server/internal/modules/plugins"
	"github.com/ervisio/ervisio/server/internal/notify"
	"github.com/ervisio/ervisio/server/internal/rpc"
)

// outcome is what a later step or condition can see of a step that ran.
type outcome struct {
	ok     bool
	output string // trimmed stdout, or the body of an HTTP call
	stdout string
	stderr string
	exit   string
	status string
	body   string
}

func hashOut(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

func errText(err error) string {
	var re *rpc.Error
	if errors.As(err, &re) {
		return re.Message
	}
	return err.Error()
}

// tail keeps the last n bytes of s (cut on a character boundary).
func tail(s string, n int) (string, bool) {
	if len(s) <= n {
		return s, false
	}
	s = s[len(s)-n:]
	for len(s) > 0 && s[0]&0xC0 == 0x80 {
		s = s[1:]
	}
	return "…" + s, true
}

// logger keeps a run's logs inside runLogBudget.
type logBudget struct{ left int }

func (b *logBudget) keep(s string, n int) (string, bool) {
	out, cut := tail(s, n)
	if len(out) > b.left {
		return "", true
	}
	b.left -= len(out)
	return out, cut
}

func (m *Manager) execute(ctx context.Context, in *Instance, run *Run, tr Trigger) {
	defer m.wg.Done()
	// Wait for a free slot (at most MaxRunning runs at once).
	select {
	case m.sem <- struct{}{}:
		defer func() { <-m.sem }()
	case <-ctx.Done():
		m.mu.Lock()
		run.Status, run.Error, run.Ended = "cancelled", "Cancelled before it started.", m.env.Now().UnixMilli()
		m.mu.Unlock()
		m.finish(in, run, nil, "cancelled")
		return
	}
	m.mu.Lock()
	run.Status = "running"
	m.mu.Unlock()

	status, errMsg, outputs := m.runSteps(ctx, in, run, tr)

	m.mu.Lock()
	run.Status, run.Error, run.Ended = status, errMsg, m.env.Now().UnixMilli()
	m.mu.Unlock()
	prev := m.finish(in, run, outputs, status)
	m.auditRunEnd(in, run, tr)
	m.alert(in, status, prev, errMsg)
}

// alert tells the "jobs" notification channels when an instance starts
// failing and when it recovers (not on every failed run).
func (m *Manager) alert(in *Instance, status, prev, errMsg string) {
	if m.env.Alert == nil {
		return
	}
	name := in.Name
	if name == "" {
		name = in.Job
	}
	bad := func(s string) bool { return s == "failed" || s == "timeout" }
	switch {
	case bad(status) && !bad(prev):
		m.env.Alert(notify.Message{Title: fmt.Sprintf("Job failed: %s (%s)", name, in.Plugin), Body: errMsg, Level: "error", Link: "/settings#pluginjobs"})
	case status == "ok" && bad(prev):
		m.env.Alert(notify.Message{Title: fmt.Sprintf("Job works again: %s (%s)", name, in.Plugin), Level: "success", Link: "/settings#pluginjobs"})
	}
}

// runSteps does the work of one run and returns its final status, an
// error message and the step output hashes to remember.
func (m *Manager) runSteps(ctx context.Context, in *Instance, run *Run, tr Trigger) (status, errMsg string, outputs map[string]string) {
	if reason, disable := m.runnable(in); reason != "" {
		if disable {
			m.mu.Lock()
			if cur := m.findLocked(in.ID); cur != nil && cur.Enabled {
				m.disableLocked(cur, reason)
			}
			m.mu.Unlock()
		}
		return "failed", reason, nil
	}
	man, _ := m.env.Manifest(in.Plugin)
	job := man.Job(in.Job)
	owner, _ := m.env.Account(in.Owner)

	params := map[string]string{}
	for k, v := range in.Params {
		params[k] = v
	}
	for k, v := range tr.Params {
		params[k] = v
	}
	params, err := job.ResolveParams(params)
	if err != nil {
		return "failed", err.Error(), nil
	}

	jctx, cancel := context.WithTimeout(ctx, time.Duration(job.Timeout())*time.Second)
	defer cancel()
	ex := m.env.NewExecutor(owner)
	defer ex.Close()
	groups := groupSet(owner)
	root := owner.IsRoot()
	budget := &logBudget{left: runLogBudget}
	results := map[string]*outcome{}
	outputs = map[string]string{}
	for k, v := range in.Outputs {
		outputs[k] = v
	}
	label := in.Name
	if label == "" {
		label = in.ID
	}

	for i := range job.Steps {
		s := &job.Steps[i]
		sr := StepRun{ID: s.ID, Kind: s.Kind()}
		if s.If != nil && !holds(s.If, results, in.Outputs) {
			sr.Status = "skipped"
			m.addStep(run, sr)
			continue
		}
		val := func(r plugins.Ref) (string, error) {
			switch r.Kind {
			case "param":
				return params[r.Name], nil
			case "job":
				return job.Name, nil
			case "instance":
				return label, nil
			case "plugin":
				return man.Name, nil
			}
			o := results[r.Name]
			if o == nil {
				return "", nil
			}
			switch r.Field {
			case "stdout":
				return o.stdout, nil
			case "stderr":
				return o.stderr, nil
			case "exitCode":
				return o.exit, nil
			case "status":
				return o.status, nil
			}
			return o.body, nil
		}
		started := m.env.Now()
		sr.Started = started.UnixMilli()
		o, failMsg := m.doStep(jctx, ex, man, in, tr, s, &sr, val, root, groups)
		sr.Duration = m.env.Now().Sub(started).Milliseconds()
		if o != nil {
			results[s.ID] = o
			if o.ok {
				outputs[s.ID] = hashOut(o.output)
			}
		}
		sr.Stdout, sr.Truncated = budget.keep(sr.Stdout, stdoutKeep)
		var cut bool
		sr.Stderr, cut = budget.keep(sr.Stderr, stderrKeep)
		sr.Truncated = sr.Truncated || cut
		if failMsg == "" {
			sr.Status = "ok"
			m.addStep(run, sr)
			continue
		}
		sr.Status, sr.Error = "failed", failMsg
		if jctx.Err() == nil && s.ContinueOnError {
			sr.Handled = true
			m.addStep(run, sr)
			continue
		}
		m.addStep(run, sr)
		switch {
		case errors.Is(jctx.Err(), context.DeadlineExceeded):
			return "timeout", fmt.Sprintf("The job took longer than %d seconds.", job.Timeout()), outputs
		case ctx.Err() != nil:
			return "cancelled", "The run was cancelled.", outputs
		}
		return "failed", fmt.Sprintf("Step %s failed: %s", s.ID, failMsg), outputs
	}
	return "ok", "", outputs
}

func (m *Manager) addStep(run *Run, sr StepRun) {
	m.mu.Lock()
	run.Steps = append(run.Steps, sr)
	m.mu.Unlock()
}

// holds evaluates a step's condition.
func holds(c *plugins.JobCond, res map[string]*outcome, prev map[string]string) bool {
	r := res[c.Step]
	if r == nil {
		return false
	}
	switch c.When {
	case "ok":
		return r.ok
	case "failed":
		return !r.ok
	case "changed":
		return r.ok && prev[c.Step] != hashOut(r.output)
	case "unchanged":
		return r.ok && prev[c.Step] == hashOut(r.output)
	case "differs", "same":
		o := res[c.Other]
		if o == nil {
			return false
		}
		return (r.output != o.output) == (c.When == "differs")
	}
	return false
}

// doStep runs one step. It returns what later steps see (nil for a step
// that did not produce anything) and, on failure, why.
func (m *Manager) doStep(ctx context.Context, ex Executor, man *plugins.Manifest, in *Instance, tr Trigger, s *plugins.JobStep, sr *StepRun,
	val func(plugins.Ref) (string, error), root bool, groups map[string]bool) (*outcome, string) {
	switch s.Kind() {
	case "command":
		args := make([]string, len(s.Args))
		for i, a := range s.Args {
			v, err := plugins.RenderTemplate(a, val, nil)
			if err != nil {
				return nil, err.Error()
			}
			args[i] = v
		}
		cmd := man.Command(s.Command)
		admin := cmd.Admin && !root && !(cmd.AdminUnlessGroup != "" && groups[cmd.AdminUnlessGroup])
		sr.Admin = admin
		target := audit.CommandTarget(s.Command, args)
		r, err := ex.Exec(ctx, admin, plugins.ExecParams{Plugin: in.Plugin, Command: s.Command, Args: args})
		if err != nil {
			m.auditStep(in, tr, "command", "", target, admin, nil, false, errText(err))
			return nil, errText(err)
		}
		code := r.ExitCode
		m.auditStep(in, tr, "command", "", target, admin, &code, code == 0, "")
		sr.ExitCode, sr.Stdout, sr.Stderr = &code, r.Stdout, r.Stderr
		o := &outcome{ok: code == 0, output: strings.TrimSpace(r.Stdout), stdout: clip(strings.TrimSpace(r.Stdout), 4096), stderr: clip(strings.TrimSpace(r.Stderr), 4096), exit: strconv.Itoa(code)}
		if code != 0 {
			return o, fmt.Sprintf("The command exited with code %d.", code)
		}
		return o, ""

	case "http":
		h := s.HTTP
		api := man.API(h.API)
		path, err := plugins.RenderTemplate(h.Path, val, url.PathEscape)
		if err != nil {
			return nil, err.Error()
		}
		query, err := plugins.RenderTemplate(h.Query, val, url.QueryEscape)
		if err != nil {
			return nil, err.Error()
		}
		esc := func(v string) string { return v }
		if h.JSON {
			esc = func(v string) string { b, _ := json.Marshal(v); return string(b[1 : len(b)-1]) }
		}
		body, err := plugins.RenderTemplate(h.Body, val, esc)
		if err != nil {
			return nil, err.Error()
		}
		var hdr map[string]string
		if len(h.Headers) > 0 {
			hdr = map[string]string{}
			for k, v := range h.Headers {
				if hdr[k], err = plugins.RenderTemplate(v, val, nil); err != nil {
					return nil, err.Error()
				}
			}
		}
		admin := api.Admin && !root && !(api.AdminUnlessGroup != "" && groups[api.AdminUnlessGroup])
		sr.Admin = admin
		logged := h.Method != "GET" && h.Method != "HEAD"
		target := audit.HTTPTarget(h.Method, path, query)
		r, err := ex.HTTP(ctx, admin, plugins.HTTPParams{Plugin: in.Plugin, Name: h.API, Method: h.Method, Path: path, Query: query, Headers: hdr, Body: body, JSON: h.JSON})
		if err != nil {
			if logged {
				m.auditStep(in, tr, "http", h.API, target, admin, nil, false, errText(err))
			}
			return nil, errText(err)
		}
		if logged {
			st := r.Status
			m.auditStep(in, tr, "http", h.API, target, admin, &st, st < 400, "")
		}
		text := r.Body
		if r.B64 {
			if b, err := base64.StdEncoding.DecodeString(r.Body); err == nil {
				text = string(b)
			}
		}
		sr.HTTPStatus, sr.Stdout = r.Status, strings.ToValidUTF8(text, "?")
		o := &outcome{ok: r.Status >= 200 && r.Status < 300, output: strings.TrimSpace(text), status: strconv.Itoa(r.Status), body: clip(strings.TrimSpace(text), 4096)}
		if !o.ok {
			return o, fmt.Sprintf("The server answered %d.", r.Status)
		}
		return o, ""

	case "notify":
		n := s.Notify
		msg := notify.Message{Level: n.Level, Source: man.Name}
		var err error
		if msg.Title, err = plugins.RenderTemplate(n.Title, val, nil); err == nil {
			if msg.Body, err = plugins.RenderTemplate(n.Body, val, nil); err == nil {
				msg.Link, err = plugins.RenderTemplate(n.Link, val, nil)
			}
		}
		if err != nil {
			return nil, err.Error()
		}
		if !notify.ValidLink(msg.Link) {
			msg.Link = ""
		}
		if m.env.Notify == nil {
			return nil, "Notifications are not available."
		}
		if err := m.env.Notify(ctx, in.Plugin, msg); err != nil {
			return nil, errText(err)
		}
		sr.Stdout = msg.Title
		return nil, ""
	}
	return nil, "unknown step"
}

func clip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return strings.ToValidUTF8(s[:n], "")
}

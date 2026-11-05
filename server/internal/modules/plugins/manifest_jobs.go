package plugins

import (
	"fmt"
	"regexp"
	"strings"
)

// capabilities.jobs: named, declarative sequences of the plugin's own
// declared commands and HTTP calls. The daemon runs them in the background
// (internal/jobs) for job instances that plugins create at runtime; the
// manifest only says WHAT a job may do, never when or for whom.
//
// A job is deliberately not a script: an ordered list of steps, each a
// declared command, a declared HTTP call or a notification, optionally
// guarded by one condition on an earlier step, with {param.x} and
// {step.id.field} placeholders in arguments, paths and bodies.

// Limits applied to jobs.
const (
	maxJobs         = 16
	maxJobSteps     = 16
	maxJobParams    = 8
	maxJobTimeout   = 6 * 3600 // a whole run (a big volume backup runs for hours)
	maxStepTimeout  = 6 * 3600 // one command step
	defaultJobTime  = 300
	maxParamLen     = 1024
	defaultParamLen = 256
)

var (
	jobIDRe = regexp.MustCompile(`^[a-z][a-z0-9_]{0,23}$`)
	// tokenRe is every placeholder a template knows.
	tokenRe = regexp.MustCompile(`\{(param\.[a-z][a-z0-9_]{0,23}|step\.[a-z][a-z0-9_]{0,23}\.(?:stdout|stderr|exitCode|status|body)|job|instance|plugin)\}`)
	// looseRe finds anything that looks like a param or step reference, so a
	// typo is an error instead of text passed on as it is.
	looseRe = regexp.MustCompile(`\{(?:param|step)\.[^{}]*\}`)

	jobLevels = map[string]bool{"info": true, "success": true, "warn": true, "error": true}
	jobWhens  = map[string]bool{"ok": true, "failed": true, "changed": true, "unchanged": true, "differs": true, "same": true}
)

// JobDef is one entry of capabilities.jobs.
type JobDef struct {
	Name        string     `json:"name"`
	Description string     `json:"description,omitempty"`
	Params      []JobParam `json:"params,omitempty"`
	Steps       []JobStep  `json:"steps"`
	// TimeoutSec bounds the whole run (default 300, max 21600 = 6 h).
	TimeoutSec int `json:"timeoutSec,omitempty"`
	// Webhook lists the params a webhook call may set (from its JSON body
	// or query string). Everything else in the call is ignored.
	Webhook *JobWebhook `json:"webhook,omitempty"`
}

// JobWebhook is JobDef.Webhook.
type JobWebhook struct {
	Params []string `json:"params"`
}

// JobParam is a value a job instance supplies. It must match Pattern
// completely, in the manifest default, at creation and at every run.
type JobParam struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Pattern     string `json:"pattern"`
	// MaxLen defaults to 256 (max 1024).
	MaxLen int `json:"maxLen,omitempty"`
	// Default is used when an instance gives no value; without one the
	// param is required.
	Default *string `json:"default,omitempty"`
	re      *regexp.Regexp
}

// JobStep is one step of a job: exactly one of Command, HTTP and Notify.
type JobStep struct {
	ID string `json:"id"`
	// Command names a capabilities.commands entry; Args fill its {N} slots
	// in order (templates, validated against the command's arg patterns
	// when the step runs).
	Command string   `json:"command,omitempty"`
	Args    []string `json:"args,omitempty"`
	HTTP    *JobHTTP `json:"http,omitempty"`
	// Notify sends a notification through the notification channels
	// (needs capabilities.notify).
	Notify *JobNotify `json:"notify,omitempty"`
	// If runs the step only when the condition holds; otherwise it is
	// skipped.
	If *JobCond `json:"if,omitempty"`
	// TimeoutSec (command steps) replaces the command's own timeout
	// (capabilities.commands[].timeoutSec: default 30, max 600) for this
	// step, up to 21600 s (6 h). It cannot exceed the job's timeoutSec.
	TimeoutSec int `json:"timeoutSec,omitempty"`
	// ContinueOnError keeps the job going when this step fails, and the
	// failure does not fail the run: a later step can handle it (an
	// `if: {step, when: "failed"}` notify step). Without it a failed step
	// ends the run as failed.
	ContinueOnError bool `json:"continueOnError,omitempty"`
}

// JobHTTP is an HTTP step: a call to a capabilities.http entry. Path, Query
// and Headers take only {param.x} placeholders (their values are escaped
// for a URL); Body also {step.id.field} ones (escaped for JSON when JSON is
// set).
type JobHTTP struct {
	API     string            `json:"api"`
	Method  string            `json:"method"`
	Path    string            `json:"path"`
	Query   string            `json:"query,omitempty"`
	Headers map[string]string `json:"headers,omitempty"`
	Body    string            `json:"body,omitempty"`
	// JSON sends the body as application/json.
	JSON bool `json:"json,omitempty"`
}

// JobNotify is a notification step. Title, Body and Link take placeholders.
type JobNotify struct {
	Title string `json:"title"`
	Body  string `json:"body,omitempty"`
	// Level is info (default), success, warn or error.
	Level string `json:"level,omitempty"`
	Link  string `json:"link,omitempty"`
}

// JobCond guards a step on an earlier step of the same job.
//
//	ok         the step ran and succeeded (exit code 0, or an HTTP 2xx)
//	failed     the step ran and failed
//	changed    the step's output differs from the previous run's of this instance (or there is none)
//	unchanged  the opposite
//	differs    the step's output differs from step Other's output in this run
//	same       the outputs are equal
//
// Outputs are the trimmed stdout of a command or the body of an HTTP call.
// A condition on a step that did not run is false.
type JobCond struct {
	Step  string `json:"step"`
	When  string `json:"when"`
	Other string `json:"other,omitempty"`
}

// Kind of a step: "command", "http" or "notify".
func (s *JobStep) Kind() string {
	switch {
	case s.Command != "":
		return "command"
	case s.HTTP != nil:
		return "http"
	case s.Notify != nil:
		return "notify"
	}
	return ""
}

// Job returns the job called name, or nil.
func (m *Manifest) Job(name string) *JobDef {
	for i := range m.Capabilities.Jobs {
		if m.Capabilities.Jobs[i].Name == name {
			return &m.Capabilities.Jobs[i]
		}
	}
	return nil
}

// Command returns the declared command called name, or nil.
func (m *Manifest) Command(name string) *Command {
	for i := range m.Capabilities.Commands {
		if m.Capabilities.Commands[i].Name == name {
			return &m.Capabilities.Commands[i]
		}
	}
	return nil
}

// jobStepAllows reports whether a job step runs the command with a
// timeoutSec of at least sec.
func (m *Manifest) jobStepAllows(command string, sec int) bool {
	if sec < 0 || sec > maxStepTimeout {
		return false
	}
	for i := range m.Capabilities.Jobs {
		for k := range m.Capabilities.Jobs[i].Steps {
			s := &m.Capabilities.Jobs[i].Steps[k]
			if s.Command == command && s.TimeoutSec >= sec {
				return true
			}
		}
	}
	return false
}

// API returns the declared HTTP API called name, or nil.
func (m *Manifest) API(name string) *HTTPAPI {
	for i := range m.Capabilities.HTTP {
		if m.Capabilities.HTTP[i].Name == name {
			return &m.Capabilities.HTTP[i]
		}
	}
	return nil
}

// Param returns the job's param called name, or nil.
func (j *JobDef) Param(name string) *JobParam {
	for i := range j.Params {
		if j.Params[i].Name == name {
			return &j.Params[i]
		}
	}
	return nil
}

// Step returns the step with the id, or nil.
func (j *JobDef) Step(id string) *JobStep {
	for i := range j.Steps {
		if j.Steps[i].ID == id {
			return &j.Steps[i]
		}
	}
	return nil
}

// Timeout is the run timeout in seconds.
func (j *JobDef) Timeout() int {
	if j.TimeoutSec > 0 {
		return j.TimeoutSec
	}
	return defaultJobTime
}

// CheckValue checks one param value against the param's pattern and length.
func (p *JobParam) CheckValue(v string) error {
	max := p.MaxLen
	if max == 0 {
		max = defaultParamLen
	}
	if len(v) > max {
		return fmt.Errorf("%s is longer than %d characters", p.Name, max)
	}
	for _, r := range v {
		if r < 0x20 || r == 0x7f {
			return fmt.Errorf("%s holds a control character", p.Name)
		}
	}
	re := p.re
	if re == nil {
		var err error
		if re, err = compileSpec(p.Pattern); err != nil {
			return err
		}
	}
	if !re.MatchString(v) {
		return fmt.Errorf("%q is not allowed for %s", v, p.Name)
	}
	return nil
}

// ResolveParams checks the values an instance gives against the job's
// params, adds the defaults and refuses unknown names.
func (j *JobDef) ResolveParams(given map[string]string) (map[string]string, error) {
	for k := range given {
		if j.Param(k) == nil {
			return nil, fmt.Errorf("the job %s has no parameter %q", j.Name, k)
		}
	}
	out := map[string]string{}
	for i := range j.Params {
		p := &j.Params[i]
		v, ok := given[p.Name]
		if !ok {
			if p.Default == nil {
				return nil, fmt.Errorf("the parameter %s is required", p.Name)
			}
			v = *p.Default
		}
		if err := p.CheckValue(v); err != nil {
			return nil, err
		}
		out[p.Name] = v
	}
	return out, nil
}

// JobNeedsAdmin reports whether running the job needs the root bridge for a
// user (root, or a member of groups): any step uses an admin command or HTTP
// API the user is not exempt from. Such an instance needs an administrator's
// approval (docs/api/jobs.md).
func (m *Manifest) JobNeedsAdmin(j *JobDef, isRoot bool, groups map[string]bool) bool {
	if isRoot {
		return false
	}
	for i := range j.Steps {
		s := &j.Steps[i]
		switch {
		case s.Command != "":
			if c := m.Command(s.Command); c != nil && adminApplies(c.Admin, c.AdminUnlessGroup, groups) {
				return true
			}
		case s.HTTP != nil:
			if a := m.API(s.HTTP.API); a != nil && adminApplies(a.Admin, a.AdminUnlessGroup, groups) {
				return true
			}
		}
	}
	return false
}

// adminApplies reports whether an admin-only command or API still needs the
// root bridge for a user in these groups.
func adminApplies(admin bool, unlessGroup string, groups map[string]bool) bool {
	return admin && !(unlessGroup != "" && groups[unlessGroup])
}

// ---- templates ----

// Ref is one placeholder of a template.
type Ref struct {
	// Kind is param, step, job, instance or plugin.
	Kind string
	// Name is the param name or the step id.
	Name string
	// Field is stdout, stderr, exitCode, status or body (steps only).
	Field string
}

// TemplateRefs lists the placeholders of s, or says which one is malformed.
func TemplateRefs(s string) ([]Ref, error) {
	for _, b := range looseRe.FindAllString(s, -1) {
		if !tokenRe.MatchString(b) {
			return nil, fmt.Errorf("%s is not a placeholder (use {param.name} or {step.id.stdout})", b)
		}
	}
	var out []Ref
	for _, m := range tokenRe.FindAllStringSubmatch(s, -1) {
		parts := strings.Split(m[1], ".")
		r := Ref{Kind: parts[0]}
		if len(parts) > 1 {
			r.Name = parts[1]
		}
		if len(parts) > 2 {
			r.Field = parts[2]
		}
		out = append(out, r)
	}
	return out, nil
}

// HasRefs reports whether s holds a placeholder.
func HasRefs(s string) bool { return tokenRe.MatchString(s) }

// RenderTemplate replaces the placeholders of s. val gives a placeholder's
// value; esc (may be nil) escapes it for where it lands. Text that is not a
// placeholder (JSON braces, "{0}") is kept as it is.
func RenderTemplate(s string, val func(Ref) (string, error), esc func(string) string) (string, error) {
	if _, err := TemplateRefs(s); err != nil {
		return "", err
	}
	var firstErr error
	out := tokenRe.ReplaceAllStringFunc(s, func(tok string) string {
		refs, _ := TemplateRefs(tok)
		if len(refs) != 1 {
			return tok
		}
		v, err := val(refs[0])
		if err != nil && firstErr == nil {
			firstErr = err
		}
		if esc != nil {
			v = esc(v)
		}
		return v
	})
	return out, firstErr
}

// ---- validation ----

func (c *Capabilities) normaliseJobs() {
	for i := range c.Jobs {
		j := &c.Jobs[i]
		if j.Params == nil {
			j.Params = []JobParam{}
		}
		if j.Steps == nil {
			j.Steps = []JobStep{}
		}
		for k := range j.Steps {
			if j.Steps[k].Args == nil && j.Steps[k].Command != "" {
				j.Steps[k].Args = []string{}
			}
		}
	}
}

func (c *Capabilities) validateJobs() error {
	if len(c.Jobs) > maxJobs {
		return fmt.Errorf("capabilities.jobs: at most %d jobs", maxJobs)
	}
	m := &Manifest{Capabilities: *c}
	seen := map[string]bool{}
	for i := range c.Jobs {
		j := &c.Jobs[i]
		if err := m.validateJob(j); err != nil {
			return fmt.Errorf("job %q: %v", j.Name, err)
		}
		if seen[j.Name] {
			return fmt.Errorf("job %q is declared twice", j.Name)
		}
		seen[j.Name] = true
	}
	return nil
}

func (m *Manifest) validateJob(j *JobDef) error {
	if !cmdNameRe.MatchString(j.Name) {
		return fmt.Errorf("name must be letters, digits, _ or - (max 32)")
	}
	if len(j.Description) > 300 {
		return fmt.Errorf("description is too long")
	}
	if j.TimeoutSec < 0 || j.TimeoutSec > maxJobTimeout {
		return fmt.Errorf("timeoutSec must be between 0 and %d", maxJobTimeout)
	}
	pnames, err := validateJobParams(j)
	if err != nil {
		return err
	}
	if err := validateWebhookParams(j.Webhook, pnames); err != nil {
		return err
	}
	return m.validateJobSteps(j, pnames)
}

// validateJobParams checks the params and compiles their patterns; it
// returns the set of param names.
func validateJobParams(j *JobDef) (map[string]bool, error) {
	if len(j.Params) > maxJobParams {
		return nil, fmt.Errorf("at most %d params", maxJobParams)
	}
	pnames := map[string]bool{}
	for i := range j.Params {
		p := &j.Params[i]
		if !jobIDRe.MatchString(p.Name) {
			return nil, fmt.Errorf("param name %q must be lowercase letters, digits or _ (max 24), starting with a letter", p.Name)
		}
		if pnames[p.Name] {
			return nil, fmt.Errorf("param %q is declared twice", p.Name)
		}
		pnames[p.Name] = true
		if err := validateJobParam(p); err != nil {
			return nil, err
		}
	}
	return pnames, nil
}

func validateJobParam(p *JobParam) error {
	if p.Pattern == "" || len(p.Pattern) > 256 {
		return fmt.Errorf("param %q needs a pattern (a regular expression the whole value must match)", p.Name)
	}
	re, err := compileSpec(p.Pattern)
	if err != nil {
		return fmt.Errorf("param %q pattern: %v", p.Name, err)
	}
	p.re = re
	if p.MaxLen < 0 || p.MaxLen > maxParamLen {
		return fmt.Errorf("param %q: maxLen is out of range", p.Name)
	}
	if p.Default != nil {
		if err := p.CheckValue(*p.Default); err != nil {
			return fmt.Errorf("param %q default: %v", p.Name, err)
		}
	}
	return nil
}

func validateWebhookParams(w *JobWebhook, pnames map[string]bool) error {
	if w == nil {
		return nil
	}
	if len(w.Params) > maxJobParams {
		return fmt.Errorf("webhook.params has too many entries")
	}
	for _, n := range w.Params {
		if !pnames[n] {
			return fmt.Errorf("webhook.params names %q, which is not a param of the job", n)
		}
	}
	return nil
}

func (m *Manifest) validateJobSteps(j *JobDef, pnames map[string]bool) error {
	if len(j.Steps) == 0 || len(j.Steps) > maxJobSteps {
		return fmt.Errorf("a job needs between 1 and %d steps", maxJobSteps)
	}
	earlier := map[string]*JobStep{}
	for i := range j.Steps {
		s := &j.Steps[i]
		if !jobIDRe.MatchString(s.ID) {
			return fmt.Errorf("step id %q must be lowercase letters, digits or _ (max 24), starting with a letter", s.ID)
		}
		if earlier[s.ID] != nil {
			return fmt.Errorf("step id %q is used twice", s.ID)
		}
		if stepActionCount(s) != 1 {
			return fmt.Errorf("step %q must have exactly one of command, http and notify", s.ID)
		}
		if err := m.validateStep(s, pnames, earlier); err != nil {
			return fmt.Errorf("step %q: %v", s.ID, err)
		}
		if err := validateStepTimeout(s, j.Timeout()); err != nil {
			return err
		}
		if err := validateStepCond(s, earlier); err != nil {
			return err
		}
		earlier[s.ID] = s
	}
	return nil
}

// stepActionCount is how many of command, http and notify the step sets.
func stepActionCount(s *JobStep) int {
	n := 0
	if s.Command != "" {
		n++
	}
	if s.HTTP != nil {
		n++
	}
	if s.Notify != nil {
		n++
	}
	return n
}

func validateStepTimeout(s *JobStep, jobTimeout int) error {
	if s.TimeoutSec == 0 {
		return nil
	}
	switch {
	case s.Kind() != "command":
		return fmt.Errorf("step %q: timeoutSec is only for command steps", s.ID)
	case s.TimeoutSec < 0 || s.TimeoutSec > maxStepTimeout:
		return fmt.Errorf("step %q: timeoutSec must be between 0 and %d", s.ID, maxStepTimeout)
	case s.TimeoutSec > jobTimeout:
		return fmt.Errorf("step %q: timeoutSec %d is longer than the job's timeoutSec (%d); raise the job's too", s.ID, s.TimeoutSec, jobTimeout)
	}
	return nil
}

func validateStepCond(s *JobStep, earlier map[string]*JobStep) error {
	cnd := s.If
	if cnd == nil {
		return nil
	}
	ref := earlier[cnd.Step]
	if ref == nil {
		return fmt.Errorf("step %q: if.step %q is not an earlier step", s.ID, cnd.Step)
	}
	if !jobWhens[cnd.When] {
		return fmt.Errorf("step %q: if.when must be one of ok, failed, changed, unchanged, differs, same", s.ID)
	}
	comparesTwoSteps := cnd.When == "differs" || cnd.When == "same"
	needsOutput := cnd.When == "changed" || cnd.When == "unchanged" || comparesTwoSteps
	if needsOutput && ref.Kind() == "notify" {
		return fmt.Errorf("step %q: a notify step has no output to compare", s.ID)
	}
	return validateCondOther(s, comparesTwoSteps, earlier)
}

// validateCondOther checks if.other: required for differs and same, not
// allowed otherwise.
func validateCondOther(s *JobStep, comparesTwoSteps bool, earlier map[string]*JobStep) error {
	if !comparesTwoSteps {
		if s.If.Other != "" {
			return fmt.Errorf("step %q: if.other is only for differs and same", s.ID)
		}
		return nil
	}
	if o := earlier[s.If.Other]; o == nil || o.Kind() == "notify" {
		return fmt.Errorf("step %q: if.other must name an earlier command or http step", s.ID)
	}
	return nil
}

func (m *Manifest) validateStep(s *JobStep, params map[string]bool, earlier map[string]*JobStep) error {
	refs := stepRefChecker{params: params, earlier: earlier}
	switch s.Kind() {
	case "command":
		return m.validateCommandStep(s, refs)
	case "http":
		return m.validateHTTPStep(s.HTTP, refs)
	case "notify":
		return m.validateNotifyStep(s.Notify, refs)
	}
	return nil
}

// stepRefChecker checks that every placeholder in a step's text names a
// param of the job or an earlier step that has that field.
type stepRefChecker struct {
	params  map[string]bool
	earlier map[string]*JobStep
}

// check validates the placeholders of text; allowStep says whether step
// outputs may be used there.
func (rc stepRefChecker) check(where, text string, allowStep bool) error {
	refs, err := TemplateRefs(text)
	if err != nil {
		return fmt.Errorf("%s: %v", where, err)
	}
	for _, r := range refs {
		switch r.Kind {
		case "param":
			if !rc.params[r.Name] {
				return fmt.Errorf("%s: {param.%s} is not a param of the job", where, r.Name)
			}
		case "step":
			if err := rc.checkStepRef(where, r, allowStep); err != nil {
				return err
			}
		}
	}
	return nil
}

func (rc stepRefChecker) checkStepRef(where string, r Ref, allowStep bool) error {
	if !allowStep {
		return fmt.Errorf("%s: step outputs cannot be used here", where)
	}
	st := rc.earlier[r.Name]
	if st == nil {
		return fmt.Errorf("%s: {step.%s.%s} is not an earlier step", where, r.Name, r.Field)
	}
	if !stepHasField(st, r.Field) {
		return fmt.Errorf("%s: step %s has no %s", where, r.Name, r.Field)
	}
	return nil
}

func stepHasField(st *JobStep, field string) bool {
	switch st.Kind() {
	case "command":
		return field == "stdout" || field == "stderr" || field == "exitCode"
	case "http":
		return field == "status" || field == "body"
	}
	return false
}

func (m *Manifest) validateCommandStep(s *JobStep, refs stepRefChecker) error {
	cmd := m.Command(s.Command)
	if cmd == nil {
		return fmt.Errorf("command %q is not declared in capabilities.commands", s.Command)
	}
	if cmd.PTY {
		return fmt.Errorf("command %q is a terminal command", s.Command)
	}
	if len(s.Args) != len(cmd.Args) {
		return fmt.Errorf("command %q takes %d argument(s), the step gives %d", s.Command, len(cmd.Args), len(s.Args))
	}
	for i, a := range s.Args {
		if err := validateStepArg(cmd, i, a, refs); err != nil {
			return err
		}
	}
	return nil
}

func validateStepArg(cmd *Command, i int, a string, refs stepRefChecker) error {
	if len(a) > 1024 || strings.ContainsRune(a, 0) {
		return fmt.Errorf("args[%d] is not valid", i)
	}
	if err := refs.check(fmt.Sprintf("args[%d]", i), a, true); err != nil {
		return err
	}
	if HasRefs(a) {
		return nil
	}
	// Fixed: check it against the command's pattern now.
	one := Command{Name: cmd.Name, Argv: []string{"x", "{0}"}, Args: []ArgSpec{cmd.Args[i]}}
	if _, err := Substitute(&one, []string{a}); err != nil {
		return fmt.Errorf("args[%d]: %v", i, err)
	}
	return nil
}

func (m *Manifest) validateHTTPStep(h *JobHTTP, refs stepRefChecker) error {
	api := m.API(h.API)
	if api == nil {
		return fmt.Errorf("http api %q is not declared in capabilities.http", h.API)
	}
	if !httpMethods[h.Method] {
		return fmt.Errorf("http.method %q must be one of GET, HEAD, POST, PUT, PATCH, DELETE, OPTIONS", h.Method)
	}
	if h.Path == "" || h.Path[0] != '/' || len(h.Path) > 1024 {
		return fmt.Errorf("http.path must start with /")
	}
	if err := validateHTTPRefs(h, refs); err != nil {
		return err
	}
	if err := validateStepHeaders(h.Headers, api, refs); err != nil {
		return err
	}
	if err := validateFixedPath(h, api); err != nil {
		return err
	}
	if !HasRefs(h.Query) && !validQuery(h.Query) {
		return fmt.Errorf("http.query holds characters that are not allowed")
	}
	return nil
}

func validateHTTPRefs(h *JobHTTP, refs stepRefChecker) error {
	if err := refs.check("http.path", h.Path, false); err != nil {
		return err
	}
	if err := refs.check("http.query", h.Query, false); err != nil {
		return err
	}
	if err := refs.check("http.body", h.Body, true); err != nil {
		return err
	}
	if len(h.Body) > 64<<10 {
		return fmt.Errorf("http.body is larger than 64 KiB")
	}
	return nil
}

func validateStepHeaders(headers map[string]string, api *HTTPAPI, refs stepRefChecker) error {
	if len(headers) > maxHTTPHeaders {
		return fmt.Errorf("http.headers has too many entries")
	}
	for k, v := range headers {
		if !apiAllowsHeader(api, k) {
			return fmt.Errorf("http.headers: %q is not in the api's headers", k)
		}
		if err := refs.check("http.headers", v, false); err != nil {
			return err
		}
	}
	return nil
}

func apiAllowsHeader(api *HTTPAPI, name string) bool {
	for _, ah := range api.Headers {
		if strings.EqualFold(ah, name) {
			return true
		}
	}
	return false
}

// validateFixedPath checks a path without placeholders against the api's
// rules now, since it cannot change at run time.
func validateFixedPath(h *JobHTTP, api *HTTPAPI) error {
	if HasRefs(h.Path) {
		return nil
	}
	dec, ok := cleanHTTPPath(h.Path)
	if !ok {
		return fmt.Errorf("http.path %q is not a valid request path", h.Path)
	}
	if !api.match(h.Method, dec) {
		return fmt.Errorf("http: %s %s is not allowed by the rules of %q", h.Method, dec, h.API)
	}
	return nil
}

func (m *Manifest) validateNotifyStep(n *JobNotify, refs stepRefChecker) error {
	if !m.Capabilities.Notify {
		return fmt.Errorf("a notify step needs capabilities.notify: true")
	}
	if strings.TrimSpace(n.Title) == "" || len(n.Title) > 300 || len(n.Body) > 4000 || len(n.Link) > 500 {
		return fmt.Errorf("notify needs a title (max 300 characters) and a body of at most 4000")
	}
	if n.Level != "" && !jobLevels[n.Level] {
		return fmt.Errorf("notify.level must be info, success, warn or error")
	}
	for _, f := range []struct{ w, t string }{{"title", n.Title}, {"body", n.Body}, {"link", n.Link}} {
		if err := refs.check("notify."+f.w, f.t, true); err != nil {
			return err
		}
	}
	return nil
}

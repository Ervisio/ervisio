package plugins

import (
	"strings"
	"testing"
)

// jobsManifest returns a manifest with commands, an HTTP API and the given
// jobs JSON (the value of capabilities.jobs).
func jobsManifest(jobs string, notify bool) string {
	n := ""
	if notify {
		n = `"notify": true,`
	}
	return `{
  "id": "jobs-test", "name": "Jobs test", "version": "1.0.0", "entry": "index.js",
  "capabilities": {
    ` + n + `
    "commands": [
      {"name": "git-fetch", "argv": ["git", "-C", "{0}", "fetch"], "args": [{"pattern": "/opt/stacks/[a-z0-9_.-]+"}]},
      {"name": "git-rev", "argv": ["git", "-C", "{0}", "rev-parse", "{1}"], "args": [{"pattern": "/opt/stacks/[a-z0-9_.-]+"}, {"pattern": "HEAD|@\\{u\\}"}]},
      {"name": "root-cmd", "admin": true, "argv": ["true"]},
      {"name": "term", "argv": ["sh"], "pty": true}
    ],
    "http": [
      {"name": "docker", "socket": "/var/run/docker.sock", "headers": ["X-Test"],
       "rules": [{"methods": ["GET", "POST"], "path": "/containers/[a-z0-9_.-]+/(start|stop)"}, {"methods": ["GET"], "path": "/containers/json"}]}
    ],
    "jobs": ` + jobs + `
  },
  "contributes": {"pages": [], "widgets": [], "snippets": []},
  "visibleTo": {"groups": []}
}`
}

func TestJobsManifestValid(t *testing.T) {
	m, err := ParseManifest([]byte(jobsManifest(`[
	 {"name": "poll", "description": "Git poll", "timeoutSec": 120,
	  "params": [{"name": "dir", "pattern": "/opt/stacks/[a-z0-9_.-]+"}, {"name": "tag", "pattern": "[a-z0-9.]+", "default": "latest"}],
	  "webhook": {"params": ["tag"]},
	  "steps": [
	   {"id": "fetch", "command": "git-fetch", "args": ["{param.dir}"]},
	   {"id": "local", "command": "git-rev", "args": ["{param.dir}", "HEAD"]},
	   {"id": "remote", "command": "git-rev", "args": ["{param.dir}", "@{u}"]},
	   {"id": "start", "if": {"step": "remote", "when": "differs", "other": "local"},
	    "http": {"api": "docker", "method": "POST", "path": "/containers/{param.tag}/start", "headers": {"X-Test": "{param.tag}"}, "body": "{\"a\":\"{step.local.stdout}\"}", "json": true}},
	   {"id": "tell", "if": {"step": "start", "when": "ok"}, "continueOnError": true,
	    "notify": {"title": "Updated {param.dir}", "body": "{step.start.status} {job} {instance}", "level": "success"}}
	  ]}]`, true)))
	if err != nil {
		t.Fatal(err)
	}
	j := m.Job("poll")
	if j == nil || len(j.Steps) != 5 {
		t.Fatalf("job not parsed: %+v", j)
	}
	if m.JobNeedsAdmin(j, false, nil) {
		t.Fatal("a user-level job must not need admin")
	}
	got, err := j.ResolveParams(map[string]string{"dir": "/opt/stacks/web"})
	if err != nil || got["tag"] != "latest" {
		t.Fatalf("defaults: %v %v", got, err)
	}
}

func TestJobsManifestInvalid(t *testing.T) {
	cases := []struct {
		name, jobs, want string
		noNotify         bool
	}{
		{"no steps", `[{"name":"a","steps":[]}]`, "between 1 and", false},
		{"unknown command", `[{"name":"a","steps":[{"id":"x","command":"nope"}]}]`, "not declared", false},
		{"pty command", `[{"name":"a","steps":[{"id":"x","command":"term"}]}]`, "terminal command", false},
		{"arg count", `[{"name":"a","steps":[{"id":"x","command":"git-fetch","args":[]}]}]`, "takes 1 argument", false},
		{"fixed arg breaks pattern", `[{"name":"a","steps":[{"id":"x","command":"git-fetch","args":["/etc"]}]}]`, "not allowed", false},
		{"unknown param", `[{"name":"a","steps":[{"id":"x","command":"git-fetch","args":["{param.dir}"]}]}]`, "not a param", false},
		{"typo placeholder", `[{"name":"a","params":[{"name":"dir","pattern":".*"}],"steps":[{"id":"x","command":"git-fetch","args":["{param.Dir}"]}]}]`, "not a placeholder", false},
		{"later step", `[{"name":"a","steps":[{"id":"x","command":"root-cmd","args":[]},{"id":"y","if":{"step":"z","when":"ok"},"command":"root-cmd","args":[]}]}]`, "not an earlier step", false},
		{"bad when", `[{"name":"a","steps":[{"id":"x","command":"root-cmd","args":[]},{"id":"y","if":{"step":"x","when":"maybe"},"command":"root-cmd","args":[]}]}]`, "if.when", false},
		{"differs needs other", `[{"name":"a","steps":[{"id":"x","command":"root-cmd","args":[]},{"id":"y","if":{"step":"x","when":"differs"},"command":"root-cmd","args":[]}]}]`, "if.other", false},
		{"duplicate id", `[{"name":"a","steps":[{"id":"x","command":"root-cmd","args":[]},{"id":"x","command":"root-cmd","args":[]}]}]`, "used twice", false},
		{"two kinds", `[{"name":"a","steps":[{"id":"x","command":"root-cmd","args":[],"http":{"api":"docker","method":"GET","path":"/containers/json"}}]}]`, "exactly one", false},
		{"http rule", `[{"name":"a","steps":[{"id":"x","http":{"api":"docker","method":"DELETE","path":"/containers/json"}}]}]`, "not allowed by the rules", false},
		{"http api", `[{"name":"a","steps":[{"id":"x","http":{"api":"nope","method":"GET","path":"/"}}]}]`, "not declared", false},
		{"http header", `[{"name":"a","steps":[{"id":"x","http":{"api":"docker","method":"GET","path":"/containers/json","headers":{"X-Other":"1"}}}]}]`, "not in the api's headers", false},
		{"step output in path", `[{"name":"a","steps":[{"id":"x","command":"root-cmd","args":[]},{"id":"y","http":{"api":"docker","method":"GET","path":"/containers/{step.x.stdout}/start"}}]}]`, "cannot be used here", false},
		{"wrong field", `[{"name":"a","steps":[{"id":"x","command":"root-cmd","args":[]},{"id":"y","http":{"api":"docker","method":"POST","path":"/containers/a/start","body":"{step.x.status}"}}]}]`, "has no status", false},
		{"notify without capability", `[{"name":"a","steps":[{"id":"x","notify":{"title":"t"}}]}]`, "capabilities.notify", true},
		{"bad level", `[{"name":"a","steps":[{"id":"x","notify":{"title":"t","level":"loud"}}]}]`, "notify.level", false},
		{"webhook param", `[{"name":"a","webhook":{"params":["x"]},"steps":[{"id":"x","command":"root-cmd","args":[]}]}]`, "not a param", false},
		{"default off pattern", `[{"name":"a","params":[{"name":"p","pattern":"[0-9]+","default":"x"}],"steps":[{"id":"x","command":"root-cmd","args":[]}]}]`, "default", false},
		{"bad pattern", `[{"name":"a","params":[{"name":"p","pattern":"("}],"steps":[{"id":"x","command":"root-cmd","args":[]}]}]`, "pattern", false},
		{"timeout", `[{"name":"a","timeoutSec":99999,"steps":[{"id":"x","command":"root-cmd","args":[]}]}]`, "timeoutSec", false},
		{"twice", `[{"name":"a","steps":[{"id":"x","command":"root-cmd","args":[]}]},{"name":"a","steps":[{"id":"x","command":"root-cmd","args":[]}]}]`, "declared twice", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := ParseManifest([]byte(jobsManifest(c.jobs, !c.noNotify)))
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("want an error containing %q, got %v", c.want, err)
			}
		})
	}
}

func TestJobNeedsAdminInheritedFromSteps(t *testing.T) {
	m, err := ParseManifest([]byte(jobsManifest(`[{"name":"a","steps":[{"id":"x","command":"root-cmd","args":[]}]}]`, false)))
	if err != nil {
		t.Fatal(err)
	}
	j := m.Job("a")
	if !m.JobNeedsAdmin(j, false, nil) {
		t.Fatal("a job with an admin command needs admin")
	}
	if m.JobNeedsAdmin(j, true, nil) {
		t.Fatal("root never needs the unlock")
	}
}

func TestJobParamsRefuseWhatThePatternRefuses(t *testing.T) {
	m, err := ParseManifest([]byte(jobsManifest(`[{"name":"a","params":[{"name":"tag","pattern":"[a-z0-9.]+","maxLen":8}],"steps":[{"id":"x","command":"root-cmd","args":[]}]}]`, false)))
	if err != nil {
		t.Fatal(err)
	}
	j := m.Job("a")
	for _, bad := range []string{"UPPER", "a b", "", "toolongvalue", "a\nb"} {
		if _, err := j.ResolveParams(map[string]string{"tag": bad}); err == nil {
			t.Errorf("%q must be refused", bad)
		}
	}
	if _, err := j.ResolveParams(map[string]string{"tag": "v1.2"}); err != nil {
		t.Error(err)
	}
	if _, err := j.ResolveParams(map[string]string{"tag": "v1", "other": "x"}); err == nil {
		t.Error("an unknown param must be refused")
	}
	if _, err := j.ResolveParams(nil); err == nil {
		t.Error("a missing required param must be refused")
	}
}

func TestRenderTemplate(t *testing.T) {
	val := func(r Ref) (string, error) {
		if r.Kind == "param" {
			return `a"b`, nil
		}
		return "x", nil
	}
	got, err := RenderTemplate(`{"k":"{param.p}","n":{0}, "u":"{u}"}`, val, nil)
	if err != nil || got != `{"k":"a"b","n":{0}, "u":"{u}"}` {
		t.Fatalf("%q %v", got, err)
	}
	if _, err := RenderTemplate(`{param.}`, val, nil); err == nil {
		t.Fatal("a malformed placeholder must be an error")
	}
}

func TestJobStepTimeout(t *testing.T) {
	job := func(jobSec, stepSec string, step string) string {
		return `[{"name": "backup", ` + jobSec + ` "steps": [{"id": "s", "command": "git-fetch", "args": ["/opt/stacks/a"], ` + stepSec + ` "continueOnError": false}` + step + `]}]`
	}
	m, err := ParseManifest([]byte(jobsManifest(job(`"timeoutSec": 21600,`, `"timeoutSec": 14400,`, ""), false)))
	if err != nil {
		t.Fatal(err)
	}
	if !m.jobStepAllows("git-fetch", 14400) || !m.jobStepAllows("git-fetch", 600) {
		t.Fatal("the declared step timeout was not allowed")
	}
	if m.jobStepAllows("git-fetch", 14401) || m.jobStepAllows("git-rev", 60) || m.jobStepAllows("git-fetch", -1) {
		t.Fatal("a longer timeout or another command was allowed")
	}
	// Default unchanged: a step without timeoutSec asks for nothing.
	m2, _ := ParseManifest([]byte(jobsManifest(job("", "", ""), false)))
	if m2.jobStepAllows("git-fetch", 31) || m2.Job("backup").Timeout() != 300 {
		t.Fatal("the default changed")
	}
	for what, doc := range map[string]string{
		"over 6 h":                job(`"timeoutSec": 21600,`, `"timeoutSec": 21601,`, ""),
		"longer than the job":     job(`"timeoutSec": 600,`, `"timeoutSec": 900,`, ""),
		"longer than the default": job("", `"timeoutSec": 400,`, ""),
		"negative":                job(`"timeoutSec": 600,`, `"timeoutSec": -5,`, ""),
		"job over 6 h":            job(`"timeoutSec": 21601,`, "", ""),
		"on a notify step":        job(`"timeoutSec": 600,`, "", `, {"id": "n", "timeoutSec": 60, "notify": {"title": "x"}}`),
	} {
		if _, err := ParseManifest([]byte(jobsManifest(doc, true))); err == nil {
			t.Errorf("%s: accepted", what)
		}
	}
}

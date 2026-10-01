package services

// Plain-language hints for well-known failures. Keep the table in one place;
// the web app translates by HintID and falls back to Hint.
type hintInput struct {
	Name     string
	Result   string // systemd Result: exit-code, timeout, start-limit-hit, ...
	ExitCode int
	Active   map[string]bool // names of active units
}

type hint struct{ ID, Text string }

func hintFor(in hintInput) *hint {
	switch in.Name {
	case "systemd-networkd-wait-online.service":
		if in.Active["NetworkManager.service"] {
			return &hint{"networkd-wait-online", "You use NetworkManager, so you probably don't need this unit. Disabling it is safe."}
		}
	case "NetworkManager-wait-online.service":
		return &hint{"nm-wait-online", "This only delays boot until a network is up. It is usually harmless if no network is connected at startup."}
	case "systemd-resolved.service":
		if in.Result == "exit-code" {
			return &hint{"resolved", "DNS lookups may fail. Check that no other DNS service is using port 53."}
		}
	}
	switch in.Result {
	case "start-limit-hit":
		return &hint{"start-limit", "It restarted too often and systemd gave up. Fix the cause in the log, then start it again."}
	case "oom-kill":
		return &hint{"oom", "It was stopped because the machine ran out of memory."}
	case "timeout":
		return &hint{"timeout", "It took too long to start or stop. Look at the log for what it was waiting for."}
	case "exit-code":
		switch in.ExitCode {
		case 203:
			return &hint{"exec-missing", "The program could not be started. It may be missing or not executable; check the ExecStart line in the unit file."}
		case 217:
			return &hint{"user-missing", "The user or group the service should run as does not exist."}
		case 200, 201, 202, 205, 206, 226:
			return &hint{"env-setup", "systemd could not prepare the environment for the service. The log has the details."}
		case 127:
			return &hint{"cmd-not-found", "A command it tried to run was not found. Check that its package is installed."}
		}
	}
	return nil
}

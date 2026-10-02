# Changes from the `envs` work (for CHANGELOG.md)

* Environments: add remote Docker hosts in Settings > Environments (Docker over TLS with client certificates, SSH with a
  key, a Portainer agent, or another Ervisio server) and choose who may use each. Plugins can send HTTP calls and declared
  commands to them (`remote: "docker"`, the `{env}` argv placeholder, the `env` option of the SDK calls, `sdk.envs.list()`).
* Certificates and SSH host keys are pinned on first connect after you confirm the fingerprint. Keys, passphrases and
  agent secrets are stored sealed on the server and never shown again.
* Pair two Ervisio servers with a one-time token (15 minutes). Calls from one run on the other, as a Linux user chosen at
  pairing, under that server's rules; either side can revoke at any time.
* Plugins that declare `capabilities.network.userHosts` can ask for a host outside their list. An administrator approves
  one exact host and port in a dialog; approvals are listed and revocable in Settings > Plugin policy.
* New daemon flags `--envs-dir` and `--tunnel-dir`.

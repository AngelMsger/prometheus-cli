# @angelmsger/prometheus-cli

npm distribution of [`prometheus-cli`](https://github.com/AngelMsger/prometheus-cli)
— a command-line tool that inspects [Prometheus](https://www.prometheus.io/) jobs,
builds, Pipeline stages and test results from the terminal — and can trigger
builds — built for coding agents (Claude Code and others) and humans alike.

```bash
npm install -g @angelmsger/prometheus-cli
prometheus-cli config init --pretty             # interactive TUI: server URL + credentials
prometheus-cli skill install                    # deploy the companion agent Skill
prometheus-cli build get my-app lastFailed      # why is the latest build red?
```

Installing this package downloads the prebuilt binary for your platform from the
matching GitHub Release and verifies its SHA-256 checksum. If your npm setup
disables install scripts, the binary is fetched on first run instead.

The companion `prometheus` Skill for coding agents is embedded in the binary.
After installing or upgrading the package, run `prometheus-cli skill install` and
reload the agent context. `prometheus-cli skill status` reports version alignment.

See the [project README](https://github.com/AngelMsger/prometheus-cli) and the
[installation guide](https://github.com/AngelMsger/prometheus-cli/blob/main/docs/installation.md)
for full documentation.

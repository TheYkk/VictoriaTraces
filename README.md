🚧 This project is currently a work in progress. Therefore, the following issues may exist:

- On-disk data structures and API endpoints may change and may not be backward compatible.
- Some links point to incorrect locations.
- Some static files are unavailable.

Please feel free to raise an issue to report these problems. We will continuously fix them during the development until the first release.

# VictoriaTraces

[![Latest Release](https://img.shields.io/github/v/release/VictoriaMetrics/VictoriaTraces?sort=semver&label=&logo=github&labelColor=gray&color=gray&link=https%3A%2F%2Fgithub.com%2FVictoriaMetrics%2FVictoriaTraces%2Freleases%2Flatest)](https://github.com/VictoriaMetrics/VictoriaTraces/releases)
![Docker Pulls](https://img.shields.io/docker/pulls/victoriametrics/victoria-traces?label=&logo=docker&logoColor=white&labelColor=2496ED&color=2496ED&link=https%3A%2F%2Fhub.docker.com%2Fr%2Fvictoriametrics%2Fvictoria-traces)
[![Build Status](https://github.com/VictoriaMetrics/VictoriaTraces/actions/workflows/main.yml/badge.svg?branch=master&link=https%3A%2F%2Fgithub.com%2FVictoriaMetrics%2FVictoriaTraces%2Factions)](https://github.com/VictoriaMetrics/VictoriaTraces/actions/workflows/main.yml)
[![License](https://img.shields.io/github/license/VictoriaMetrics/VictoriaTraces?labelColor=green&label=&link=https%3A%2F%2Fgithub.com%2FVictoriaMetrics%2FVictoriaTraces%2Fblob%2Fmaster%2FLICENSE)](https://github.com/VictoriaMetrics/VictoriaTraces/blob/master/LICENSE)
![Slack](https://img.shields.io/badge/Join-4A154B?logo=slack&link=https%3A%2F%2Fslack.victoriametrics.com)
[![X](https://img.shields.io/twitter/follow/VictoriaMetrics?style=flat&label=Follow&color=black&logo=x&labelColor=black&link=https%3A%2F%2Fx.com%2FVictoriaMetrics)](https://x.com/VictoriaMetrics/)
[![Reddit](https://img.shields.io/reddit/subreddit-subscribers/VictoriaMetrics?style=flat&label=Join&labelColor=red&logoColor=white&logo=reddit&link=https%3A%2F%2Fwww.reddit.com%2Fr%2FVictoriaMetrics)](https://www.reddit.com/r/VictoriaMetrics/)

VictoriaTraces is a fast easy to use database for traces.

Here are some resources and information about VictoriaTraces:

- [Documentation](https://docs.victoriametrics.com/victoriatraces/)
- Available: [Binary releases](https://github.com/VictoriaMetrics/VictoriaTraces/releases/latest), docker images [Docker Hub](https://hub.docker.com/r/victoriametrics/victoria-traces/) and [Quay](https://quay.io/repository/victoriametrics/victoria-traces), [Source code](https://github.com/VictoriaMetrics/VictoriaTraces)
- Deployment types: [Single-node version](https://docs.victoriametrics.com/victoriatraces/), [Cluster version](https://docs.victoriametrics.com/victoriatraces/cluster/)
- Changelog: [CHANGELOG](https://docs.victoriametrics.com/victoriatraces/changelog/), and [How to upgrade](https://docs.victoriametrics.com/victoriatraces/#upgrading)
- Community: [Slack](https://slack.victoriametrics.com/), [X (Twitter)](https://x.com/VictoriaMetrics), [LinkedIn](https://www.linkedin.com/company/victoriametrics/), [YouTube](https://www.youtube.com/@VictoriaMetrics)

Both the single-node and the cluster versions of VictoriaTraces are open source and free to use.

## Local Docker builds

Both local Docker build paths use a Debian 13 slim runtime and CGO-enabled,
statically linked binaries, following the production build's `netgo osusergo` tags.

```sh
# Build from source inside Docker; no local Go compiler is required.
./build-docker.sh source victoria-traces:local

# Compile with the local Linux Go/C toolchain, then package only the binary.
./build-docker.sh local victoria-traces:local
```

The mode defaults to `source` and the image tag defaults to `victoria-traces:local`.
The script builds an image locally; it does not push it to a registry.

- `Dockerfile` builds the vendored source with `golang:1.26.6-trixie` and
  `CGO_ENABLED=1`, then copies the binary into `debian:13-slim`.
  `Dockerfile.dockerignore` includes the vendored dependencies while excluding
  local binaries, storage data, and build caches.
- `ADockerfile` packages an executable named `victoria-traces` from its build
  context without compiling it. The script's `local` mode supplies a temporary
  context containing only this Dockerfile and the freshly compiled binary.
  It does not overwrite an existing binary in the repository.

Both modes require Docker. Local mode additionally requires a Linux host with
Go 1.26.6 or newer, a C compiler, and static glibc development libraries
(`gcc` and `libc6-dev` on Debian/Ubuntu). It builds for the host architecture;
source mode builds for Docker's default platform.


## Community and contributions

Feel free asking any questions regarding VictoriaTraces:

- [Slack Inviter](https://slack.victoriametrics.com/) and [Slack channel](https://victoriametrics.slack.com/)
- [X (Twitter)](https://x.com/VictoriaMetrics/)
- [Linkedin](https://www.linkedin.com/company/victoriametrics/)
- [Reddit](https://www.reddit.com/r/VictoriaMetrics/)
- [Telegram](https://t.me/VictoriaTraces)
- [Mastodon](https://mastodon.social/@victoriametrics/)

If you like VictoriaTraces and want to contribute, then please [read these docs](https://docs.victoriametrics.com/victoriametrics/contributing/).

Thank you for your cooperation!

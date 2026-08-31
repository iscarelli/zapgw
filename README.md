# zapgw

*[Leia em português](README.pt-BR.md)*

A gateway for Meta's messaging APIs (WhatsApp Cloud API and Instagram DM) — multi-tenant, static
binary, no runtime dependencies.

A consumer sends `POST /v1/messages` and gets back a signed `POST` to its own `callback_url` when
something arrives. The gateway is what talks to Meta; **what** to send and **when** stays with the
consumer.

## Status

**In production since July 2026, serving two consumers.** The code landed here on 2026-08-30 from
the private repository where the project was born; this is now the working repository, and the old
one is frozen as history.

> **Why the history starts on 2026-08-30 and not in July:** the origin repository carries real
> people's phone numbers and customer names across hundreds of commits. **There is no unpublishing.**
> So the project was born again here, with the first commit already sanitized, instead of rewriting
> seven hundred commits and hoping nothing was missed. The cost of that choice is a short history;
> the gain is that no third party's data has to be removed later.

## What it does

- **Sending** — text, media, templates, interactive, reactions, location and contacts. **One Meta
  action per call:** looping, ordering and retry policy belong to the consumer, which has the
  context to run them.
- **Receiving** — Meta's webhook arrives here, its signature is verified, it is isolated per tenant,
  and it is redelivered **signed** to the consumer's `callback_url`.
- **Real multi-tenancy** — every instance has its own token, secret and destination, encrypted at
  rest. Every route declares how it isolates tenants, and a test fails any new route that doesn't.
- **Observability** — per-instance, per-day counters, a transit log, and a `/v1/estado` that
  distinguishes *"I measured it and it's bad"* from *"I could not measure it"*.

## Getting started

Requirements: Go 1.22+ and a Meta App with WhatsApp Business.

    git clone https://github.com/iscarelli/zapgw.git && cd zapgw
    CGO_ENABLED=0 go build ./cmd/zapgw
    cp .env.example /etc/zapgw/env      # fill it in; NEVER commit values

The Meta side — App, phone number, permanent token, webhook — is walked through in
[`docs/ONBOARDING-META.md`](docs/ONBOARDING-META.md), including what each step costs and where it
stalls.

## Deploying

The binary is static and has no runtime dependencies. Two ways to get one:

    # from the release (Linux; zapgw-linux-arm64 is also published)
    curl -fsSLo zapgw https://github.com/iscarelli/zapgw/releases/download/v0.60.1/zapgw-linux-amd64
    chmod +x zapgw

    # or build it
    CGO_ENABLED=0 go build ./cmd/zapgw

⚠️ **`zapgw-linux-arm64` is published but has never been executed** — it only compiled. *Compiling
is not running*, so it ships marked untested rather than announced as supported.

Three things have to be in place on the target host:

| what | where | from |
|---|---|---|
| the binary | `/usr/local/bin/zapgw` | the release, or the build above |
| the systemd unit | `/etc/systemd/system/zapgw.service` | [`implanta/zapgw.service`](implanta/zapgw.service) |
| the variables | `/etc/zapgw/env`, mode `0600` | a filled-in copy of [`.env.example`](.env.example) |

`/etc/zapgw/env` is the only place secrets live — `ZAPGW_CHAVE_CIFRA` included. It is not version
controlled, the deploy script does not copy it, and none of it ever travels on a command line.

### The deploy script

[`implanta/deploy.sh`](implanta/deploy.sh) does the whole path for a Proxmox container: build, ship,
snapshot, keep the previous binary, swap, restart, and **wait for `/v1/health` to answer**. If it
doesn't answer in time, the script **rolls back on its own** and exits non-zero. It assumes `pct` on
the target node; for any other topology it is worth more as a script to read than as a tool to run.

**Three variables are required and have no defaults.** Missing any one of them stops the script
before it touches the network, naming the variable and the expected format:

    ZAPGW_DEPLOY_VMID=100 \
    ZAPGW_DEPLOY_HOST=deploy@proxmox-node.example.internal \
    ZAPGW_DEPLOY_SAUDE=http://<gateway-internal-ip>:8080/v1/health \
    implanta/deploy.sh

*They have no defaults on purpose.* Until this repository was opened, the script carried a real
installation's node, container and IP baked in. In a public repository a default like that is not
just a leaked address: it is a script that, run by someone who didn't read it, tries to deploy to a
host that isn't theirs.

## Documentation

- [`docs/CONTRATO-CONSUMIDOR.md`](docs/CONTRATO-CONSUMIDOR.md) — **the API reference.** Every route,
  every field, every error code, and what each response promises and what it explicitly does **not**.
- [`docs/MANUAL-DO-INTEGRADOR.md`](docs/MANUAL-DO-INTEGRADOR.md) — zero to first message.
- [`docs/MODELO-DE-USO.md`](docs/MODELO-DE-USO.md) — what is the gateway's responsibility and what is
  the consumer's, and why the line sits where it does.
- [`docs/META-CAMPOS-DE-WEBHOOK.md`](docs/META-CAMPOS-DE-WEBHOOK.md) — Meta's fields that pass
  through, and the ones that are never translated.
- [`docs/ARMADILHAS.md`](docs/ARMADILHAS.md) — *pitfalls*. **Read it before touching anything.** One
  line per trap, each with the real cost it charged. None of them is hypothetical.

## Rules that hold from the first commit

- **Nothing that identifies a real person or customer gets in** — phone numbers, tenant names,
  third-party Meta identifiers, internal network addresses. Examples and fixtures use synthetic
  values, **and a test fails anything else** — it even decodes the base64 hidden inside a `wamid`,
  because that identifier carries the recipient's phone number inside it and a grep for the number
  as a human writes it walks straight past.
- **TLS has no off switch**, in either direction, under no flag — not even "just for development".
  A test fails whoever introduces one.
- Why both, and what makes each one fail, is in [`CLAUDE.md`](CLAUDE.md).

## License

[AGPL-3.0-or-later](LICENSE). The product is a server: run a modified version as a service and you
publish its source.

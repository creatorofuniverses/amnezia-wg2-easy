# AmneziaWG Easy

Fork of the archived [amnezia-wg-easy](https://github.com/spcfox/amnezia-wg-easy) with **AmneziaWG 2.0** support: S1-S4 padding, H1-H4 header ranges, and I1-I5 CPS (Custom Protocol Signature) packets for DPI evasion. Built on [`amneziavpn/amneziawg-go`](https://hub.docker.com/r/amneziavpn/amneziawg-go) base image with AWG 2.0 userspace tools.

> **Note:** Most of the AWG 2.0 upgrade code in this fork was written by [Claude Code](https://claude.ai/code) (Anthropic's AI coding agent). Human-reviewed and tested.

<p align="center">
  <img src="./assets/screenshot.png" width="802" />
</p>

## Features

* All-in-one: AmneziaWG + Web UI.
* Easy installation, simple to use.
* List, create, edit, delete, enable & disable clients.
* Download a client's configuration file.
* Statistics for which clients are connected.
* Tx/Rx charts for each connected client.
* Gravatar support.
* Automatic Light / Dark Mode
* Multilanguage Support
* UI_TRAFFIC_STATS (default off)
* **AmneziaWG 2.0**: S3/S4 padding, H1-H4 ranges, I1-I5 CPS signatures

## Requirements

* A host with Docker installed.

## Installation

### 1. Install Docker

If you haven't installed Docker yet, install it by running:

```bash
curl -sSL https://get.docker.com | sh
sudo usermod -aG docker $(whoami)
exit
```

And log in again.

### 2. Enable IP forwarding

Run these on the **host** before starting the container:

```bash
sudo sysctl -w net.ipv4.ip_forward=1
sudo sysctl -w net.ipv4.conf.all.src_valid_mark=1
```

To make them persistent across reboots:

```bash
echo 'net.ipv4.ip_forward=1' | sudo tee -a /etc/sysctl.conf
echo 'net.ipv4.conf.all.src_valid_mark=1' | sudo tee -a /etc/sysctl.conf
```

### 3. Run AmneziaWG Easy

```
  docker run -d \
  --name=amnezia-wg-easy \
  -e LANGUAGE=en \
  -e WG_HOST=<🚨YOUR_SERVER_IP> \
  -e PASSWORD=<🚨YOUR_ADMIN_PASSWORD> \
  -e PORT=51821 \
  -e WG_PORT=51820 \
  -v ~/.amnezia-wg-easy:/etc/amnezia/amneziawg \
  -p 51820:51820/udp \
  -p 51821:51821/tcp \
  --cap-add=NET_ADMIN \
  --cap-add=SYS_MODULE \
  --device=/dev/net/tun:/dev/net/tun \
  --restart unless-stopped \
  ghcr.io/creatorofuniverses/amnezia-wg-easy
```

> Replace `YOUR_SERVER_IP` with your WAN IP, or a Dynamic DNS hostname.
>
> Replace `YOUR_ADMIN_PASSWORD` with a password to log in on the Web UI.

The Web UI will now be available on `http://0.0.0.0:51821`.

> Your configuration files will be saved in `~/.amnezia-wg-easy`

### 4. Or use Docker Compose

Copy [`docker-compose.yml`](docker-compose.yml), set `WG_HOST` and `PASSWORD`, then:

```bash
docker compose up --detach
```

For local development, build the image from source:

```bash
docker compose up --detach --build
```

All environment variables are documented as comments inside the compose file.

## Options

These options can be configured by setting environment variables using `-e KEY="VALUE"` in the `docker run` command.

| Env | Default | Example | Description |
| - | - | - | - |
| `LANGUAGE` | `en` | `de` | Web UI language (Supports: en, ru, tr, no, pl, fr, de, ca, es). |
| `CHECK_UPDATE` | `true` | `false` | Check for a new version and display a notification about its availability |
| `PORT` | `51821` | `6789` | TCP port for Web UI. |
| `WEBUI_HOST` | `0.0.0.0` | `localhost` | IP address web UI binds to. |
| `PASSWORD` | - | `foobar123` | When set, requires a password when logging in to the Web UI. |
| `WG_HOST` | - | `vpn.myserver.com` | The public hostname of your VPN server. |
| `WG_DEVICE` | `eth0` | `ens6f0` | Ethernet device the AmneziaWG traffic should be forwarded through. |
| `WG_PORT` | `51820` | `12345` | The public UDP port of your VPN server. AmneziaWG will listen on that (otherwise default) inside the Docker container. |
| `WG_MTU` | `null` | `1420` | The MTU the clients will use. Server uses default WG MTU. |
| `WG_PERSISTENT_KEEPALIVE` | `0` | `25` | Value in seconds to keep the "connection" open. If this value is 0, then connections won't be kept alive. |
| `WG_DEFAULT_ADDRESS` | `10.8.0.x` | `10.6.0.x` | Clients IP address range. |
| `WG_DEFAULT_DNS` | `1.1.1.1` | `8.8.8.8, 8.8.4.4` | DNS server clients will use. If set to blank value, clients will not use any DNS. |
| `WG_ALLOWED_IPS` | `0.0.0.0/0, ::/0` | `192.168.15.0/24, 10.0.1.0/24` | Allowed IPs clients will use. |
| `WG_PRE_UP` | `...` | - | See [config.js](/src/config.js#L21) for the default value. |
| `WG_POST_UP` | `...` | `iptables ...` | See [config.js](/src/config.js#L22) for the default value. |
| `WG_PRE_DOWN` | `...` | - | See [config.js](/src/config.js#L29) for the default value. |
| `WG_POST_DOWN` | `...` | `iptables ...` | See [config.js](/src/config.js#L30) for the default value. |
| `UI_TRAFFIC_STATS` | `false` | `true` | Enable detailed RX / TX client stats in Web UI |
| `UI_CHART_TYPE` | `0` | `1` | UI_CHART_TYPE=0 # Charts disabled, UI_CHART_TYPE=1 # Line chart, UI_CHART_TYPE=2 # Area chart, UI_CHART_TYPE=3 # Bar chart |
| `JC` | `random` | `5` | Junk packet count — number of packets with random data that are sent before the start of the session. |
| `JMIN` | `50` | `25` | Junk packet minimum size — minimum packet size for Junk packet. That is, all randomly generated packets will have a size no smaller than Jmin. |
| `JMAX` | `1000` | `250` | Junk packet maximum size — maximum size for Junk packets. |
| `S1` | `random` | `75` | Init packet junk size — the size of random data that will be added to the init packet (range 15-150). |
| `S2` | `random` | `75` | Response packet junk size — the size of random data that will be added to the response packet (range 15-150). |
| `S3` | `random` | `32` | Cookie reply padding size (range 0-64). AWG 2.0 parameter. |
| `S4` | `random` | `8` | Data packet padding size (range 0-32, keep low — adds per-packet overhead). AWG 2.0 parameter. |
| `H1` | `random` | `100000-500000000` | Init packet magic header. Supports range format `min-max` (AWG 2.0) or single value for backward compat. |
| `H2` | `random` | `600000000-900000000` | Response packet magic header. Same format as H1. Ranges must not overlap between H1-H4. |
| `H3` | `random` | `1000000000-1200000000` | Underload packet magic header. Same format as H1. |
| `H4` | `random` | `1300000000-1400000000` | Transport packet magic header. Same format as H1. |
| `I1` | - | `<r 2><b 0x8580...>` | CPS signature line 1 (client-only, AWG 2.0). Uses CPS syntax: `<b 0xHEX>`, `<r N>`, `<t>`, etc. |
| `I2` | - | `<b 0xc000000001><r 64><t>` | CPS signature line 2 (client-only). Only include lines that have values. |
| `I3` | - | - | CPS signature line 3 (client-only). |
| `I4` | - | - | CPS signature line 4 (client-only). |
| `I5` | - | - | CPS signature line 5 (client-only). |

> If you change `WG_PORT`, make sure to also change the exposed port.

## QR Codes & Client Configs

The QR codes and downloadable configs use the **classic AmneziaWG** format (compatible with the [AmneziaWG](https://github.com/amnezia-vpn/amneziawg-tools) client apps). They are **not** in the AmneziaVPN format — importing them into the AmneziaVPN app is not supported yet.

<!-- TODO: Add AmneziaVPN config format support (JSON-based, includes protocol selection and server metadata) -->

## Optional Obfuscation Proxy

You can run the VPN in two modes:

| Mode | Command | What runs |
|------|---------|-----------|
| Plain | `just up` | AmneziaWG 2.0 only (default) |
| Proxy | `just up-proxy` | AmneziaWG 2.0 + UDP obfuscation proxy sidecar |

In **proxy mode** an async UDP proxy sits in front of AmneziaWG and makes the
traffic positively resemble a real **QUIC / DNS / STUN / SIP** service to Deep
Packet Inspection — a second layer on top of AWG's own S1–S4 / H1–H4
randomization. Clients connect exactly as before (same `WG_HOST:WG_PORT`); the
proxy is transparent. Standard AmneziaWG clients get server→client obfuscation;
bidirectional imitation additionally requires WireSock Secure Connect 3.5+ on
the client.

### Setup

```bash
cp .env.example .env       # then edit WG_HOST, PASSWORD, etc.
just up-proxy              # builds the proxy image and starts both containers
```

The client-facing port is `WG_PORT` (e.g. set `WG_PORT=443` with
`PROXY_PROTOCOL=quic`, or `WG_PORT=53` with `PROXY_PROTOCOL=dns`).

### Proxy configuration (`.env`)

| Variable | Default | Purpose |
|----------|---------|---------|
| `PROXY_PROTOCOL` | `quic` | Protocol to imitate: `quic` / `dns` / `stun` / `sip` / `auto` |
| `PROXY_QUIC_HANDSHAKE` | `true` | Complete a real QUIC/TLS handshake to probes (stronger; `false` = stateless Version Negotiation) |
| `PROXY_QUIC_DOMAIN` | `cloudflare.com` | SNI domain for the QUIC handshake cert |
| `PROXY_DNS_FORWARD` | `false` | Answer real DNS queries upstream. **Requires** `PROXY_PROTOCOL=dns` or `auto` — the proxy refuses to start otherwise |
| `PROXY_DNS_UPSTREAM` | `1.1.1.1:53` | Upstream resolver when forwarding |

### Switching modes

```bash
just down        # or: just down-proxy
just up-proxy    # or: just up
```

Client data persists across modes (shared `~/.amnezia-wg-easy` volume).

### Credits / vendoring

The proxy under [`proxy/`](proxy/) is vendored from
[wiresock/amneziawg-install](https://github.com/wiresock/amneziawg-install)
(`amneziawg-proxy`), MIT-licensed, at commit `549bba8`. Thanks to its authors.
To update it, re-copy the upstream crate over `proxy/` and bump this note.

## Updating

To update to the latest version, simply run:

```bash
docker stop amnezia-wg-easy
docker rm amnezia-wg-easy
docker pull ghcr.io/creatorofuniverses/amnezia-wg-easy
```

And then run the `docker run -d \ ...` command above again.

With Docker Compose AmneziaWG Easy can be updated with a single command:
`docker compose up --detach --pull always`

### Upgrading from AWG 1.x (spcfox/amnezia-wg-easy)

The config path has changed from `/etc/wireguard` to `/etc/amnezia/amneziawg`. Update your volume mount accordingly. Your existing `wg0.json` config will be automatically migrated — legacy single-value H1-H4 parameters are converted to the new range format on first load.

## Thanks

Based on [wg-easy](https://github.com/wg-easy/wg-easy) by Emile Nijssen and [amnezia-wg-easy](https://github.com/spcfox/amnezia-wg-easy) by spcfox.

AWG 2.0 support co-authored by [Claude Code](https://claude.ai/code).

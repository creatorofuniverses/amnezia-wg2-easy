# ── Web UI node_modules (node 18: node 20 hangs on armv6/v7 builds) ──
FROM docker.io/library/node:18-alpine AS build_node_modules
COPY src /app
WORKDIR /app
RUN npm ci --omit=dev && mv node_modules /node_modules

# ── amneziawg-go-proxy (userspace datapath fallback) ──
FROM golang:1.24-alpine AS build_awg_go
ARG AWG_GO_REF=master
RUN apk add --no-cache git make
RUN git clone https://github.com/creatorofuniverses/amneziawg-go-proxy.git /src \
 && cd /src && git checkout "${AWG_GO_REF}" && make
# binary: /src/amneziawg-go

# ── amneziawg-tools-proxy (awg + awg-quick) ──
FROM alpine:3.20 AS build_awg_tools
ARG AWG_TOOLS_REF=master
RUN apk add --no-cache git build-base linux-headers bash
RUN git clone https://github.com/creatorofuniverses/amneziawg-tools-proxy.git /src \
 && cd /src && git checkout "${AWG_TOOLS_REF}" \
 && make -C src WITH_WGQUICK=yes WITH_BASHCOMPLETION=no WITH_SYSTEMDUNITS=no \
 && make -C src install DESTDIR=/out PREFIX=/usr WITH_WGQUICK=yes WITH_BASHCOMPLETION=no WITH_SYSTEMDUNITS=no
# installs: /out/usr/bin/awg, /out/usr/bin/awg-quick

# ── Go probe-responder ──
FROM golang:1.25-alpine AS build_responder
RUN apk add --no-cache linux-headers git
COPY responder /src
WORKDIR /src
RUN go build -o /awg-responder .
# binary: /awg-responder

# ── Runtime ──
FROM alpine:3.20
RUN apk add --no-cache nodejs npm bash iproute2 iptables dumb-init
COPY --from=build_awg_go    /src/amneziawg-go      /usr/bin/amneziawg-go
COPY --from=build_awg_tools /out/usr/bin/awg       /usr/bin/awg
COPY --from=build_awg_tools /out/usr/bin/awg-quick /usr/bin/awg-quick
RUN ln -sf /usr/bin/awg /usr/bin/wg \
 && ln -sf /usr/bin/awg-quick /usr/bin/wg-quick \
 && chmod +x /usr/bin/awg-quick
COPY --from=build_node_modules /app /app
COPY --from=build_node_modules /node_modules /node_modules
COPY --from=build_responder /awg-responder /usr/bin/awg-responder
COPY docker-entrypoint.sh /usr/bin/docker-entrypoint.sh
RUN chmod +x /usr/bin/awg-responder /usr/bin/docker-entrypoint.sh
ENV DEBUG=Server,WireGuard
# awg-quick uses the host kernel module if present, else this userspace impl:
ENV WG_QUICK_USERSPACE_IMPLEMENTATION=amneziawg-go
WORKDIR /app
HEALTHCHECK --interval=1m --timeout=5s --retries=3 CMD /usr/bin/timeout 5s /bin/sh -c "/usr/bin/wg show | /bin/grep -q interface || exit 1"
CMD ["/usr/bin/dumb-init", "/usr/bin/docker-entrypoint.sh"]

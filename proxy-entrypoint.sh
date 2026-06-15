#!/bin/sh
# Renders proxy.toml from environment, then runs amneziawg-proxy.
# Booleans (PROXY_DNS_FORWARD, PROXY_QUIC_HANDSHAKE) MUST be literal
# `true`/`false` (unquoted TOML booleans).
set -eu

: "${WG_PORT:=51820}"
: "${PROXY_PROTOCOL:=quic}"
: "${PROXY_BACKEND_HOST:=amnezia-wg2-easy}"
: "${PROXY_DNS_FORWARD:=false}"
: "${PROXY_DNS_UPSTREAM:=1.1.1.1:53}"
: "${PROXY_QUIC_HANDSHAKE:=true}"
: "${PROXY_QUIC_DOMAIN:=cloudflare.com}"
: "${AWG_CONFIG:=/etc/amnezia/amneziawg/wg0.conf}"

# Validate TOML booleans early — a bad value would otherwise produce a
# confusing parse error from the proxy at startup.
case "${PROXY_DNS_FORWARD}" in true|false) ;; *) echo "ERROR: PROXY_DNS_FORWARD must be 'true' or 'false' (got '${PROXY_DNS_FORWARD}')" >&2; exit 1 ;; esac
case "${PROXY_QUIC_HANDSHAKE}" in true|false) ;; *) echo "ERROR: PROXY_QUIC_HANDSHAKE must be 'true' or 'false' (got '${PROXY_QUIC_HANDSHAKE}')" >&2; exit 1 ;; esac

CONFIG_DIR=/etc/amneziawg-proxy
CONFIG_FILE="${CONFIG_DIR}/proxy.toml"
mkdir -p "${CONFIG_DIR}" /var/lib/amneziawg-proxy

cat > "${CONFIG_FILE}" <<EOF
listen = "0.0.0.0:${WG_PORT}"
backend = "${PROXY_BACKEND_HOST}:${WG_PORT}"
imitate_protocol = "${PROXY_PROTOCOL}"
awg_config = "${AWG_CONFIG}"
dns_forward_enabled = ${PROXY_DNS_FORWARD}
dns_upstream = "${PROXY_DNS_UPSTREAM}"
quic_handshake_enabled = ${PROXY_QUIC_HANDSHAKE}
quic_certificate_domain = "${PROXY_QUIC_DOMAIN}"
EOF

echo "amneziawg-proxy: rendered ${CONFIG_FILE}:"
cat "${CONFIG_FILE}"

exec amneziawg-proxy "${CONFIG_FILE}"

#!/bin/sh
# Supervises the Node UI/tunnel and the optional Go probe-responder.
# Node is the primary process (its exit ends the container); the responder is a
# crash-isolated side filter whose death never affects connectivity.
set -e

WG_PORT="${WG_PORT:-51820}"
QUEUE_NUM="${RESPONDER_QUEUE:-0}"

# The connmark claim rule is only needed for the multi-RTT QUIC handshake.
QUIC_HS="${QUIC_HANDSHAKE:-true}"
CLAIM_RULE=false
if [ "${IMITATE_PROTOCOL:-none}" = "quic" ] && [ "${QUIC_HS}" != "false" ]; then
  CLAIM_RULE=true
fi

insert_nfqueue_rule() {
  # NEW-only first-contact rule (established flows bypass userspace).
  iptables -I INPUT 1 -p udp --dport "${WG_PORT}" -m conntrack --ctstate NEW \
    -m limit --limit 50/sec --limit-burst 100 \
    -j NFQUEUE --queue-num "${QUEUE_NUM}" --queue-bypass
  if [ "${CLAIM_RULE}" = "true" ]; then
    # Claimed QUIC probe flows: keep the WHOLE flow queued to the responder
    # across RTTs. Inserted at position 1 so it precedes the NEW rule.
    iptables -I INPUT 1 -p udp --dport "${WG_PORT}" -m connmark --mark 0x1/0x1 \
      -j NFQUEUE --queue-num "${QUEUE_NUM}" --queue-bypass
  fi
}

flush_nfqueue_rules() {
  # Loop-delete every copy of both rules (idempotent; cleans leftovers from a
  # prior crash/kill that skipped graceful teardown). Unconditional so a stale
  # connmark rule is removed even when CLAIM_RULE is now false.
  while iptables -D INPUT -p udp --dport "${WG_PORT}" -m connmark --mark 0x1/0x1 \
    -j NFQUEUE --queue-num "${QUEUE_NUM}" --queue-bypass 2>/dev/null; do :; done
  while iptables -D INPUT -p udp --dport "${WG_PORT}" -m conntrack --ctstate NEW \
    -m limit --limit 50/sec --limit-burst 100 \
    -j NFQUEUE --queue-num "${QUEUE_NUM}" --queue-bypass 2>/dev/null; do :; done
}

run_responder() {
  # Wait for the datapath to come up (wg0 present) before touching netfilter.
  i=0
  while [ "$i" -lt 30 ]; do
    if ip link show wg0 >/dev/null 2>&1; then
      break
    fi
    i=$((i + 1))
    sleep 1
  done

  flush_nfqueue_rules
  insert_nfqueue_rule
  echo "responder: NFQUEUE rule installed on udp/${WG_PORT} (queue ${QUEUE_NUM})"
  /usr/bin/awg-responder &
  RESP_PID=$!
  # On responder exit, flush rules so traffic falls through to awg0.
  wait "${RESP_PID}" || true
  echo "responder: exited; flushing NFQUEUE rules (active-probe defense off, tunnel unaffected)"
  flush_nfqueue_rules
}

if [ "${RESPONDER:-false}" = "true" ]; then
  # Validate synchronously (fail-fast) BEFORE launching anything.
  case "${IMITATE_PROTOCOL:-none}" in
    none)
      echo "ERROR: RESPONDER=true requires IMITATE_PROTOCOL != none" >&2
      exit 1
      ;;
    sip)
      echo "WARN: IMITATE_PROTOCOL=sip is shaping-only; SIP probes are NOT answered." >&2
      echo "WARN: sip + RESPONDER=true is the least-protected setting (silence is a fingerprint)." >&2
      ;;
  esac
  run_responder &
fi

# Node brings up the tunnel and serves the UI; it is the primary process.
exec node server.js

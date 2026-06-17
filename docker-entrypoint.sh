#!/bin/sh
# Supervises the Node UI/tunnel and the optional Go probe-responder.
# Node is the primary process (its exit ends the container); the responder is a
# crash-isolated side filter whose death never affects connectivity.
set -e

WG_PORT="${WG_PORT:-51820}"
QUEUE_NUM="${RESPONDER_QUEUE:-0}"

insert_nfqueue_rule() {
  # Insert at the HEAD of INPUT so it precedes the PostUp ACCEPT that the Node
  # app appends (src/config.js). NEW-only: established flows bypass userspace.
  # --queue-bypass: if no process is attached to the queue, ACCEPT (fail open).
  iptables -I INPUT 1 -p udp --dport "${WG_PORT}" -m conntrack --ctstate NEW \
    -m limit --limit 50/sec --limit-burst 100 \
    -j NFQUEUE --queue-num "${QUEUE_NUM}" --queue-bypass
}

remove_nfqueue_rule() {
  iptables -D INPUT -p udp --dport "${WG_PORT}" -m conntrack --ctstate NEW \
    -m limit --limit 50/sec --limit-burst 100 \
    -j NFQUEUE --queue-num "${QUEUE_NUM}" --queue-bypass 2>/dev/null || true
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

  insert_nfqueue_rule
  echo "responder: NFQUEUE rule installed on udp/${WG_PORT} (queue ${QUEUE_NUM})"
  /usr/bin/awg-responder &
  RESP_PID=$!
  # On responder exit, remove the rule so traffic falls through to awg0.
  wait "${RESP_PID}" || true
  echo "responder: exited; removing NFQUEUE rule (active-probe defense off, tunnel unaffected)"
  remove_nfqueue_rule
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

# Responder hardening backlog

Living note for hardening the NFQUEUE probe-responder. Seeded by a field
observation on 2026-06-21 (entry node `ufo-ru-en`, `IMITATE_PROTOCOL=quic`,
`RESPONDER=true`, `QUIC_HANDSHAKE=true`). Start here when picking up hardening.

## Field observation (2026-06-21)

During a live debug session, the container logged a **single**:

```
responder: nfqueue error: netlink receive: i/o timeout
$ wg-quick down wg0
... (Node re-listens on :51821) ...
$ wg-quick down wg0
$ wg-quick up wg0
responder: NFQUEUE rule installed on udp/443 (queue 0)
```

i.e. the responder netlink error **coincided** with a `wg-quick down/up` cycle
(all clients dropped for ~2s). Container `RestartCount` stayed `0` (Docker did not
restart the container), and after the cycle everything was stable. Also seen once
at responder start, benign:

```
quic-go: connection doesn't allow setting of receive buffer size. Not a *net.UDPConn?
```

## Why this is suspicious (the responder *should* be isolated)

Reading the code, a responder netlink error should **not** be able to flap the
tunnel:

- `nfqueue.go:86-89` — `errFn` already **logs and returns 0**, so per-receive
  errors (incl. `netlink receive: i/o timeout`) do **not** stop the read loop.
- `main.go:74-76` — the process only `log.Fatal`s if `runQueue` *returns* an
  error. `runQueue` returns only on `nfqueue.Open` / `newQUICManager` /
  `RegisterWithErrorFunc` failure; otherwise it blocks on `<-ctx.Done()`. The
  `errFn`-return-0 path keeps the read goroutine alive, so the i/o timeout above
  should not have exited the process.
- `docker-entrypoint.sh:41-61` — `run_responder` is fully isolated: on
  `awg-responder` exit it only `flush_nfqueue_rules`; it **never** touches
  `wg-quick` or Node. Node (`exec node server.js`) is the primary process and the
  sole owner of the tunnel.

**Conclusion: by design the responder cannot issue `wg-quick down/up`.** So the
observed flap is either:

- **(a) Node-driven and coincidental** — Node cycled the tunnel for its own reason
  (config write / reconnect / `wg syncconf`) and the timing just lined up; or
- **(b) reversed causality** — Node tore `wg0` down first, and the responder's
  nfnetlink read timed out *as a symptom* of the datapath churn, logging the
  error after the fact.

The `NFQUEUE rule installed` line reappearing means `run_responder` ran again,
which (given RestartCount=0) implies the **entrypoint re-executed** — i.e. Node
exited and was relaunched inside the container, OR the whole `run_responder`
subshell was restarted. That points the investigation at **Node's tunnel
lifecycle**, not the responder.

## Investigation / hardening backlog

- **P1 — pin the causality.** Reproduce with precise timestamps and determine
  ordering: did `wg-quick down` (Node) fire *before* the `netlink i/o timeout`, or
  after? Add timestamped logging on both sides. Find out **why Node cycles the
  tunnel** (which event in `lib/WireGuard.js` calls `wg-quick down/up` /
  `wg syncconf`). If Node is the flapper, the responder is innocent and the real
  fix is Node-side (don't bounce the whole tunnel on a benign event).
- **P2 — survive datapath churn.** Confirm whether a `wg0` down/up invalidates the
  responder's nfnetlink socket. If a fatal netlink error *can* occur, prefer
  **re-opening the queue** over `log.Fatal` in `main.go:74` (today a return =
  process death = `run_responder` flushes rules = probe defense silently off until
  the next entrypoint run).
- **P3 — netlink receive robustness.** `netlink receive: i/o timeout` under load
  is commonly **receive-buffer overflow**. `nfqueue.go:14-19` sets
  `MaxQueueLen: 0xff` (255) — low. Consider raising it and the netlink socket
  rcvbuf, and have `errFn` distinguish transient (retry) from fatal (re-open)
  rather than a blanket `return 0`.
- **P4 — assert the isolation invariant.** Add an integration check that killing
  `awg-responder` only flushes NFQUEUE rules and never perturbs Node/the tunnel
  (the entrypoint already intends this; lock it down with a test so a future
  refactor can't regress it).
- **P5 — silence/annotate the benign quic-go warning.** `connection doesn't allow
  setting of receive buffer size` is expected: the responder feeds `quic-go` over
  a custom NFQUEUE-backed `net.PacketConn`, not a real `*net.UDPConn`. Suppress or
  document inline so it isn't chased as a bug during hardening.

## P1 instrumentation — LANDED (2026-06-25)

The "timestamped logging on both sides" P1 asks for now ships (no behaviour
change — evidence only). A **static trace settled the easy half**: the Node
backend has **no periodic/automatic tunnel bounce** (zero `setInterval`; the 1 s
UI poll only runs read-only `wg show … dump`). Every `wg-quick down/up` comes
from exactly one caller — startup bootstrap, `updateServerSettings` (restart
field), `regenerateKeypair`, a site-peer `__applyWithBounce`, or `Shutdown()` on
**SIGTERM**. And a transient `netlink i/o timeout` does **not** kill the responder
(`nfqueue.go` `errFn` returns 0). So the field log's `down … re-listen on :51821
… down … up … NFQUEUE rule installed` is a **full Node/entrypoint restart**, and
**P1 reduces to: what restarted the Node process?** (`run_responder` doesn't loop,
so the reappearing install line ⇒ the entrypoint re-ran.)

What was added to capture the answer on the next occurrence / a deliberate repro:

- **Node — `src/lib/WireGuard.js`:** all tunnel commands now go through
  `__tunnelExec(op, reason)`, which logs `tunnel evt=begin|ok|err op=<down|up|sync>
  reason=<caller> ts=<ISO> …` around each `wg-quick`/`syncconf`. `reason` names the
  caller (`bootstrap`, `settings`, `regen-key`, `sitepeer-*`, `shutdown`,
  `saveConfig`, …) so a flap is attributable.
- **Node — `src/server.js`:** `lifecycle evt=boot|signal|exit|unhandledRejection|
  uncaughtException` with ISO ts + pid. The two crash handlers are the key fix: a
  crash used to terminate Node **silently** (no handler) — the prime suspect for a
  restart with `RestartCount=0`. They log a stack first, then preserve exit-1.
- **Responder — `main.go` / `nfqueue.go`:** `log.SetFlags(… | Lmicroseconds |
  LUTC)` so every responder line is UTC-µs and orderable against Node's ISO lines
  in the same `docker logs`; plus a clean-exit line on ctx cancel.
- **`docker-entrypoint.sh`:** `log_ts` UTC-stamps the NFQUEUE install/exit echoes,
  pinning the entrypoint re-run.

**Read the timeline** (single stream, both UTC):
`docker logs --since 2h <ctr> 2>&1 | grep -nE 'lifecycle|tunnel evt|netlink|NFQUEUE|context cancelled'`
The order to look for: does `lifecycle evt=unhandledRejection/uncaughtException` or
`evt=signal sig=SIGTERM` appear **before** the `tunnel evt=… op=down`? If a crash/
signal precedes the down, Node is the flapper and P1's fix is Node-side. If the
`netlink i/o timeout` precedes everything, re-open P2/P3. If neither (only OOM-kill
in `dmesg`), the fix is a memory/limits issue, not code.

## Workaround (until hardened)

`RESPONDER=false` — keeps passive QUIC imitation (`IMITATE_PROTOCOL=quic` still
shapes traffic); only the active-probe answering is lost. Tunnel unaffected.

## Cross-reference

External field write-up (symptom-level, for VPN operators) lives in the wiki:
`vpn-setup-wiki/triple-awg-xray/imitation-stack-guide.md` → "Грабли нового
сервера" → gotcha #4.

'use strict';

// Round 3 / P1 instrumentation: timestamped process-lifecycle logging so a Node
// restart — the real trigger behind the field "tunnel flap" (the responder is
// isolated by design and cannot bounce wg0) — leaves a greppable fingerprint in
// `docker logs`, orderable against the responder's netlink events. A crash
// (unhandled rejection / uncaught exception) previously terminated the process
// SILENTLY (no handler), which is exactly why the flap's cause was undeterminable.
// These handlers add the fingerprint WITHOUT changing terminate-on-crash
// behaviour (Node's default is already to exit 1 on both).

const lifeLog = (evt, extra) => {
  const line = `lifecycle evt=${evt} ts=${new Date().toISOString()} pid=${process.pid}${extra ? ` ${extra}` : ''}`;
  // eslint-disable-next-line no-console
  console.log(line);
};

lifeLog('boot', `node=${process.version}`);

require('./services/Server');

const WireGuard = require('./services/WireGuard');

WireGuard.getConfig()
  .catch((err) => {
  // eslint-disable-next-line no-console
    console.error(err);
    lifeLog('exit-getconfig-failed', 'code=1');

    // eslint-disable-next-line no-process-exit
    process.exit(1);
  });

// A previously-unhandled rejection terminated the process with no trace. Log the
// reason + stack first, then preserve the terminate behaviour (exit 1).
process.on('unhandledRejection', (reason) => {
  const msg = reason instanceof Error ? (reason.stack || reason.message) : String(reason);
  // eslint-disable-next-line no-console
  console.error(`lifecycle evt=unhandledRejection ts=${new Date().toISOString()} pid=${process.pid}\n${msg}`);

  // eslint-disable-next-line no-process-exit
  process.exit(1);
});

process.on('uncaughtException', (err) => {
  // eslint-disable-next-line no-console
  console.error(`lifecycle evt=uncaughtException ts=${new Date().toISOString()} pid=${process.pid}\n${(err && err.stack) || err}`);

  // eslint-disable-next-line no-process-exit
  process.exit(1);
});

// Handle terminate signal (`docker stop` -> SIGTERM). This is the ONLY signal
// path that intentionally tears the tunnel down (Shutdown -> `wg-quick down`).
process.on('SIGTERM', async () => {
  lifeLog('signal', 'sig=SIGTERM action=shutdown');
  await WireGuard.Shutdown();
  // eslint-disable-next-line no-process-exit
  process.exit(0);
});

// Handle interrupt signal. NOTE (P1 follow-up candidate): this logs but neither
// shuts down nor exits — the process keeps running on SIGINT, which a supervisor
// may then escalate to SIGKILL. Left as-is for instrumentation; flagged.
process.on('SIGINT', () => {
  lifeLog('signal', 'sig=SIGINT action=log-only');
});

// Last-gasp marker: fires on any process exit (sync handlers only). Pins the
// exact code of every Node death, so a restart shows boot(new pid) right after.
process.on('exit', (code) => {
  lifeLog('exit', `code=${code}`);
});

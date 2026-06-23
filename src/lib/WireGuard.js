'use strict';

const fs = require('node:fs/promises');
const path = require('path');
const debug = require('debug')('WireGuard');
const crypto = require('node:crypto');
const QRCode = require('qrcode');

const Util = require('./Util');
const ServerError = require('./ServerError');
const ShareString = require('./awgShareString');
const { stripImitationKeys } = require('./stripImitationKeys');
const ServerSettings = require('./serverSettings');
const ConfigRender = require('./configRender');
const ClientValidation = require('./clientValidation');

const {
  WG_PATH,
  WG_DEVICE,
  WG_HOST,
  WG_PORT,
  WG_MTU,
  WG_DEFAULT_DNS,
  WG_DEFAULT_ADDRESS,
  WG_PERSISTENT_KEEPALIVE,
  WG_ALLOWED_IPS,
  WG_PRE_UP,
  WG_POST_UP,
  WG_PRE_DOWN,
  WG_POST_DOWN,
  JC,
  JMIN,
  JMAX,
  S1,
  S2,
  S3,
  S4,
  H1,
  H2,
  H3,
  H4,
  I1,
  I2,
  I3,
  I4,
  I5,
  IMITATE_PROTOCOL,
} = require('../config');

module.exports = class WireGuard {

  async getConfig() {
    if (!this.__configPromise) {
      this.__configPromise = Promise.resolve().then(async () => {
        if (!WG_HOST) {
          throw new Error('WG_HOST Environment Variable Not Set!');
        }

        debug('Loading configuration...');
        let config;
        try {
          config = await fs.readFile(path.join(WG_PATH, 'wg0.json'), 'utf8');
          config = JSON.parse(config);

          // Migrate AWG 1.x config to 2.0
          if (typeof config.server.h1 === 'number') {
            config.server.h1 = { min: config.server.h1, max: config.server.h1 };
            config.server.h2 = { min: config.server.h2, max: config.server.h2 };
            config.server.h3 = { min: config.server.h3, max: config.server.h3 };
            config.server.h4 = { min: config.server.h4, max: config.server.h4 };
          }
          if (config.server.s3 === undefined) config.server.s3 = S3;
          if (config.server.s4 === undefined) config.server.s4 = S4;

          debug('Configuration loaded.');
        } catch (err) {
          const privateKey = await Util.exec('wg genkey');
          const publicKey = await Util.exec(`echo ${privateKey} | wg pubkey`, {
            log: 'echo ***hidden*** | wg pubkey',
          });

          const address = WG_DEFAULT_ADDRESS.replace('x', '1');

          config = {
            server: {
              privateKey,
              publicKey,
              address,
              jc: JC,
              jmin: JMIN,
              jmax: JMAX,
              s1: S1,
              s2: S2,
              s3: S3,
              s4: S4,
              h1: H1,
              h2: H2,
              h3: H3,
              h4: H4,
            },
            clients: {},
          };

          debug('Configuration generated.');
        }

        ServerSettings.seedServerDefaults(config.server, {
          host: WG_HOST,
          port: WG_PORT,
          mtu: WG_MTU,
          dns: WG_DEFAULT_DNS,
          defaultAddress: WG_DEFAULT_ADDRESS,
          allowedIPs: WG_ALLOWED_IPS,
          persistentKeepalive: WG_PERSISTENT_KEEPALIVE,
          i1: I1,
          i2: I2,
          i3: I3,
          i4: I4,
          i5: I5,
        });

        await this.__saveConfig(config);
        await Util.exec('wg-quick down wg0').catch(() => { });
        await Util.exec('wg-quick up wg0').catch((err) => {
          if (err && err.message && err.message.includes('Cannot find device "wg0"')) {
            throw new Error('WireGuard exited with the error: Cannot find device "wg0"\nThis usually means that your host\'s kernel does not support WireGuard!');
          }

          throw err;
        });
        await this.__logDatapathImpl();
        // await Util.exec(`iptables -t nat -A POSTROUTING -s ${WG_DEFAULT_ADDRESS.replace('x', '0')}/24 -o ' + WG_DEVICE + ' -j MASQUERADE`);
        // await Util.exec('iptables -A INPUT -p udp -m udp --dport 51820 -j ACCEPT');
        // await Util.exec('iptables -A FORWARD -i wg0 -j ACCEPT');
        // await Util.exec('iptables -A FORWARD -o wg0 -j ACCEPT');
        await this.__syncConfig();

        return config;
      });
    }

    return this.__configPromise;
  }

  // Report (to stdout, so it lands in `docker logs`) whether awg-quick brought
  // wg0 up on the host kernel module or fell back to the amneziawg-go userspace
  // datapath. A userspace impl is a TUN device, so /sys/class/net/wg0/tun_flags
  // exists; the kernel module's device has no such file. Best-effort: never throws.
  async __logDatapathImpl() {
    try {
      await fs.access('/sys/class/net/wg0/tun_flags');
      // eslint-disable-next-line no-console
      console.log('datapath: amneziawg kernel module not found — using amneziawg-go userspace datapath (wg0 is a TUN)');
    } catch (err) {
      if (err && err.code === 'ENOENT') {
        // eslint-disable-next-line no-console
        console.log('datapath: using the amneziawg kernel module (host DKMS)');
      } else {
        // eslint-disable-next-line no-console
        console.log(`datapath: could not determine kernel vs userspace for wg0 (${err.message})`);
      }
    }
  }

  async saveConfig() {
    const config = await this.getConfig();
    await this.__saveConfig(config);
    await this.__syncConfig();
  }

  async __saveConfig(config) {
    const hooks = ConfigRender.renderDefaultHooks(config.server, {
      device: WG_DEVICE,
      preUp: WG_PRE_UP,
      postUp: WG_POST_UP,
      preDown: WG_PRE_DOWN,
      postDown: WG_POST_DOWN,
    }, config.clients);
    const result = ConfigRender.renderServerConf(config.server, config.clients, hooks, IMITATE_PROTOCOL);

    debug('Config saving...');
    await fs.writeFile(path.join(WG_PATH, 'wg0.json'), JSON.stringify(config, false, 2), {
      mode: 0o660,
    });
    await fs.writeFile(path.join(WG_PATH, 'wg0.conf'), result, {
      mode: 0o600,
    });
    debug('Config saved.');
  }

  async __syncConfig() {
    debug('Config syncing...');
    await Util.exec('wg syncconf wg0 <(wg-quick strip wg0)');
    debug('Config synced.');
  }

  // Apply a config mutation with a full tunnel bounce + rollback.
  // `mutate(config)` changes the shared config in place and returns a rollback fn.
  // Order: down (tears down OLD on-disk firewall rules) -> mutate+save -> up.
  async __applyWithBounce(mutate) {
    const config = await this.getConfig();
    await Util.exec('wg-quick down wg0').catch(() => { });
    const rollback = mutate(config);
    await this.__saveConfig(config);
    try {
      await Util.exec('wg-quick up wg0');
    } catch (err) {
      rollback();
      await this.__saveConfig(config);
      await Util.exec('wg-quick up wg0').catch(() => { });
      throw Object.assign(new Error(`Failed to apply site-peer change: ${err.message}`), { statusCode: 500 });
    }
  }

  async getServerSettings() {
    const config = await this.getConfig();
    const s = config.server;
    return {
      host: s.host,
      port: s.port,
      mtu: s.mtu,
      dns: s.dns,
      defaultAddress: s.defaultAddress,
      allowedIPs: s.allowedIPs,
      persistentKeepalive: s.persistentKeepalive,
      jc: s.jc,
      jmin: s.jmin,
      jmax: s.jmax,
      s1: s.s1,
      s2: s.s2,
      s3: s.s3,
      s4: s.s4,
      h1: s.h1,
      h2: s.h2,
      h3: s.h3,
      h4: s.h4,
      i1: s.i1,
      i2: s.i2,
      i3: s.i3,
      i4: s.i4,
      i5: s.i5,
      publicKey: s.publicKey,
      // Read-only: env-only (kept in sync with the responder at container start).
      imitateProtocol: IMITATE_PROTOCOL,
    };
  }

  async updateServerSettings(patch) {
    const config = await this.getConfig();
    // imitateProtocol is read-only/env-only; the UI echoes it back in the draft,
    // so drop it before validate/classify/apply (never persisted to config.server).
    delete patch.imitateProtocol;
    const errors = ServerSettings.validateServerSettings(patch, config.server);
    if (Object.keys(errors).length > 0) {
      throw Object.assign(new Error('Invalid server settings'), { statusCode: 400, errors });
    }

    const prev = { ...config.server };
    const diff = ServerSettings.classify(prev, patch);

    if (diff.needsRestart) {
      // Tear down using the CURRENTLY on-disk conf (live firewall rules) BEFORE writing.
      await Util.exec('wg-quick down wg0').catch(() => { });
    }

    Object.assign(config.server, patch);
    await this.__saveConfig(config);

    if (diff.needsRestart) {
      try {
        await Util.exec('wg-quick up wg0');
      } catch (err) {
        // Roll back so a bad value never strands the server offline.
        Object.assign(config.server, prev);
        await this.__saveConfig(config);
        await Util.exec('wg-quick up wg0').catch(() => { });
        throw Object.assign(new Error(`Failed to apply settings: ${err.message}`), { statusCode: 500 });
      }
    } else {
      await this.__syncConfig();
    }

    return { settings: await this.getServerSettings(), restarted: diff.needsRestart, mustReimport: diff.mustReimport };
  }

  async regenerateKeypair() {
    const config = await this.getConfig();
    const prevPrivateKey = config.server.privateKey;
    const prevPublicKey = config.server.publicKey;
    const privateKey = await Util.exec('wg genkey');
    const publicKey = await Util.exec(`echo ${privateKey} | wg pubkey`, {
      log: 'echo ***hidden*** | wg pubkey',
    });
    await Util.exec('wg-quick down wg0').catch(() => { });
    config.server.privateKey = privateKey;
    config.server.publicKey = publicKey;
    await this.__saveConfig(config);
    try {
      await Util.exec('wg-quick up wg0');
    } catch (err) {
      config.server.privateKey = prevPrivateKey;
      config.server.publicKey = prevPublicKey;
      await this.__saveConfig(config);
      await Util.exec('wg-quick up wg0').catch(() => { });
      throw Object.assign(new Error(`Failed to regenerate keypair: ${err.message}`), { statusCode: 500 });
    }
    return { publicKey, mustReimport: true };
  }

  async getClients() {
    const config = await this.getConfig();
    const clients = Object.entries(config.clients).map(([clientId, client]) => ({
      id: clientId,
      name: client.name,
      enabled: client.enabled,
      legacy: client.legacy === true,
      address: client.address,
      publicKey: client.publicKey,
      createdAt: new Date(client.createdAt),
      updatedAt: new Date(client.updatedAt),
      allowedIPs: client.allowedIPs,
      siteMasquerade: client.siteMasquerade === true,
      downloadableConfig: 'privateKey' in client,
      persistentKeepalive: null,
      latestHandshakeAt: null,
      transferRx: null,
      transferTx: null,
    }));

    // Loop WireGuard status
    const dump = await Util.exec('wg show wg0 dump', {
      log: false,
    });
    dump
      .trim()
      .split('\n')
      .slice(1)
      .forEach((line) => {
        const [
          publicKey,
          preSharedKey, // eslint-disable-line no-unused-vars
          endpoint, // eslint-disable-line no-unused-vars
          allowedIps, // eslint-disable-line no-unused-vars
          latestHandshakeAt,
          transferRx,
          transferTx,
          persistentKeepalive,
        ] = line.split('\t');

        const client = clients.find((client) => client.publicKey === publicKey);
        if (!client) return;

        client.latestHandshakeAt = latestHandshakeAt === '0'
          ? null
          : new Date(Number(`${latestHandshakeAt}000`));
        client.transferRx = Number(transferRx);
        client.transferTx = Number(transferTx);
        client.persistentKeepalive = persistentKeepalive;
      });

    return clients;
  }

  async getClient({ clientId }) {
    const config = await this.getConfig();
    // Own-property check: reject inherited keys (`__proto__`, `constructor`, …)
    // so they 404 like any unknown id instead of resolving to a prototype object.
    if (!Object.prototype.hasOwnProperty.call(config.clients, clientId)) {
      throw new ServerError(`Client Not Found: ${clientId}`, 404);
    }
    const client = config.clients[clientId];

    return client;
  }

  async getClientConfiguration({ clientId }) {
    const config = await this.getConfig();
    const client = await this.getClient({ clientId });

    const conf = ConfigRender.renderClientConf(config.server, client, IMITATE_PROTOCOL);
    return client.legacy ? stripImitationKeys(conf) : conf;
  }

  async getClientShareString({ clientId }) {
    const client = await this.getClient({ clientId });
    const config = await this.getClientConfiguration({ clientId });
    const name = String(client.name || '').replace(/[\r\n]+/g, ' ').trim();
    return ShareString.encode(`# Name = ${name}\n${config}`);
  }

  async getClientQRCodeSVG({ clientId }) {
    const config = await this.getClientConfiguration({ clientId });
    return QRCode.toString(config, {
      type: 'svg',
      width: 512,
    });
  }

  async createClient({ name }) {
    if (!name) {
      throw new Error('Missing: Name');
    }

    const config = await this.getConfig();

    const privateKey = await Util.exec('wg genkey');
    const publicKey = await Util.exec(`echo ${privateKey} | wg pubkey`);
    const preSharedKey = await Util.exec('wg genpsk');

    // Calculate next IP (skip taken addresses and any that fall inside a site peer's subnet)
    let address;
    const siteOthers = Object.entries(config.clients)
      .filter(([, c]) => ClientValidation.isSitePeer(c))
      .map(([id, c]) => ({ clientId: id, name: c.name, cidrs: ClientValidation.effectiveCidrs(c) }));
    for (let i = 2; i < 255; i++) {
      const candidate = WG_DEFAULT_ADDRESS.replace('x', i);
      if (Object.values(config.clients).some((c) => c.address === candidate)) continue;
      if (ClientValidation.findOverlap([`${candidate}/32`], siteOthers)) continue;
      address = candidate;
      break;
    }

    if (!address) {
      throw new Error('Maximum number of clients reached.');
    }

    // Create Client
    const id = crypto.randomUUID();
    const client = {
      id,
      name,
      address,
      privateKey,
      publicKey,
      preSharedKey,

      createdAt: new Date(),
      updatedAt: new Date(),

      enabled: true,
      legacy: false,
      allowedIPs: null,
      siteMasquerade: false,
    };

    config.clients[id] = client;

    await this.saveConfig();

    return client;
  }

  async enableClient({ clientId }) {
    const client = await this.getClient({ clientId });
    if (ClientValidation.isSitePeer(client)) {
      await this.__applyWithBounce(() => {
        const prev = client.enabled;
        client.enabled = true;
        client.updatedAt = new Date();
        return () => {
          client.enabled = prev;
        };
      });
      return;
    }
    client.enabled = true;
    client.updatedAt = new Date();
    await this.saveConfig();
  }

  async disableClient({ clientId }) {
    const client = await this.getClient({ clientId });
    if (ClientValidation.isSitePeer(client)) {
      await this.__applyWithBounce(() => {
        const prev = client.enabled;
        client.enabled = false;
        client.updatedAt = new Date();
        return () => {
          client.enabled = prev;
        };
      });
      return;
    }
    client.enabled = false;
    client.updatedAt = new Date();
    await this.saveConfig();
  }

  async deleteClient({ clientId }) {
    const config = await this.getConfig();
    const client = config.clients[clientId];
    if (!client) return;
    if (ClientValidation.isSitePeer(client)) {
      await this.__applyWithBounce((cfg) => {
        const removed = cfg.clients[clientId];
        delete cfg.clients[clientId];
        return () => {
          cfg.clients[clientId] = removed;
        };
      });
      return;
    }
    delete config.clients[clientId];
    await this.saveConfig();
  }

  async setClientLegacy({ clientId, legacy }) {
    const client = await this.getClient({ clientId });

    client.legacy = !!legacy;
    client.updatedAt = new Date();

    await this.saveConfig();
  }

  async setClientSitePeer({ clientId, allowedIPs, siteMasquerade }) {
    const config = await this.getConfig();
    const client = await this.getClient({ clientId });

    const norm = (typeof allowedIPs === 'string' && allowedIPs.trim()) ? allowedIPs.trim() : null;
    const masq = norm ? !!siteMasquerade : false;

    const others = Object.entries(config.clients)
      .filter(([id]) => id !== clientId)
      .map(([id, c]) => ({ clientId: id, name: c.name, cidrs: ClientValidation.effectiveCidrs(c) }));
    const errors = ClientValidation.validateClientAllowedIPs(norm, others);
    if (Object.keys(errors).length > 0) {
      throw Object.assign(new Error('Invalid AllowedIPs'), { statusCode: 400, errors });
    }

    const prevAllowed = client.allowedIPs;
    const prevMasq = client.siteMasquerade;
    await this.__applyWithBounce(() => {
      client.allowedIPs = norm;
      client.siteMasquerade = masq;
      client.updatedAt = new Date();
      return () => {
        client.allowedIPs = prevAllowed;
        client.siteMasquerade = prevMasq;
      };
    });

    return { client };
  }

  async updateClientName({ clientId, name }) {
    const client = await this.getClient({ clientId });

    client.name = name;
    client.updatedAt = new Date();

    await this.saveConfig();
  }

  async updateClientAddress({ clientId, address }) {
    const config = await this.getConfig();
    const client = await this.getClient({ clientId });

    if (!Util.isValidIPv4(address)) {
      throw new ServerError(`Invalid Address: ${address}`, 400);
    }
    const others = Object.entries(config.clients)
      .filter(([id]) => id !== clientId)
      .map(([id, c]) => ({ clientId: id, name: c.name, cidrs: ClientValidation.effectiveCidrs(c) }));
    const conflict = ClientValidation.findOverlap([`${address}/32`], others);
    if (conflict) {
      throw new ServerError(`Address ${address} overlaps ${conflict.with}`, 400);
    }

    client.address = address;
    client.updatedAt = new Date();
    await this.saveConfig();
  }

  // Shutdown wireguard
  async Shutdown() {
    await Util.exec('wg-quick down wg0').catch(() => { });
  }

};

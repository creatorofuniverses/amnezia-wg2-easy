/* eslint-disable no-unused-vars */
/* eslint-disable no-undef */

'use strict';

class API {

  async call({ method, path, body }) {
    const res = await fetch(`./api${path}`, {
      method,
      headers: {
        'Content-Type': 'application/json',
      },
      body: body
        ? JSON.stringify(body)
        : undefined,
    });

    if (res.status === 204) {
      return undefined;
    }

    const json = await res.json();

    if (!res.ok) {
      // H3's default error handler drops `message` from the body; the reason
      // rides in data.message (lossless) or statusMessage (sanitized).
      throw new Error(
        (json.data && json.data.message)
        || json.statusMessage
        || json.message
        || json.error
        || res.statusText,
      );
    }

    return json;
  }

  async getCheckUpdate() {
    return this.call({
      method: 'get',
      path: '/check-update',
    });
  }

  async getRelease() {
    return this.call({
      method: 'get',
      path: '/release',
    });
  }

  async getLang() {
    return this.call({
      method: 'get',
      path: '/lang',
    });
  }

  async getuiTrafficStats() {
    return this.call({
      method: 'get',
      path: '/ui-traffic-stats',
    });
  }

  async getChartType() {
    return this.call({
      method: 'get',
      path: '/ui-chart-type',
    });
  }

  async getSession() {
    return this.call({
      method: 'get',
      path: '/session',
    });
  }

  async createSession({ password }) {
    return this.call({
      method: 'post',
      path: '/session',
      body: { password },
    });
  }

  async deleteSession() {
    return this.call({
      method: 'delete',
      path: '/session',
    });
  }

  async getClients() {
    return this.call({
      method: 'get',
      path: '/wireguard/client',
    }).then((clients) => clients.map((client) => ({
      ...client,
      createdAt: new Date(client.createdAt),
      updatedAt: new Date(client.updatedAt),
      latestHandshakeAt: client.latestHandshakeAt !== null
        ? new Date(client.latestHandshakeAt)
        : null,
    })));
  }

  async createClient({ name }) {
    return this.call({
      method: 'post',
      path: '/wireguard/client',
      body: { name },
    });
  }

  async deleteClient({ clientId }) {
    return this.call({
      method: 'delete',
      path: `/wireguard/client/${clientId}`,
    });
  }

  async enableClient({ clientId }) {
    return this.call({
      method: 'post',
      path: `/wireguard/client/${clientId}/enable`,
    });
  }

  async disableClient({ clientId }) {
    return this.call({
      method: 'post',
      path: `/wireguard/client/${clientId}/disable`,
    });
  }

  async enableClientLegacy({ clientId }) {
    return this.call({
      method: 'post',
      path: `/wireguard/client/${clientId}/legacy/enable`,
    });
  }

  async disableClientLegacy({ clientId }) {
    return this.call({
      method: 'post',
      path: `/wireguard/client/${clientId}/legacy/disable`,
    });
  }

  async updateClientName({ clientId, name }) {
    return this.call({
      method: 'put',
      path: `/wireguard/client/${clientId}/name/`,
      body: { name },
    });
  }

  async updateClientAddress({ clientId, address }) {
    return this.call({
      method: 'put',
      path: `/wireguard/client/${clientId}/address/`,
      body: { address },
    });
  }

  async setClientSitePeer({ clientId, allowedIPs, siteMasquerade }) {
    const res = await fetch(`./api/wireguard/client/${clientId}/allowedips`, {
      method: 'put',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ allowedIPs, siteMasquerade }),
    });
    let body = {};
    try {
      body = await res.json();
    } catch (e) {
      // no/non-json body
    }
    if (!res.ok) {
      if (res.status === 400) {
        const err = new Error(body.statusMessage || body.message || 'Validation failed');
        err.fieldErrors = (body.data && body.data.errors) || {};
        throw err;
      }
      throw new Error(body.message || body.error || res.statusText);
    }
    return body;
  }

  async getClientShareString({ clientId }) {
    const res = await fetch(`./api/wireguard/client/${clientId}/share-string`);
    if (!res.ok) {
      let message = res.statusText;
      try {
        message = (await res.json()).error || message;
      } catch (e) {
        // body is not JSON
      }
      throw new Error(message);
    }
    return res.text();
  }

  async getServerSettings() {
    return this.call({
      method: 'get',
      path: '/server-settings',
    });
  }

  async updateServerSettings(patch) {
    const res = await fetch('./api/server-settings', {
      method: 'post',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(patch),
    });
    let body = {};
    try {
      body = await res.json();
    } catch (e) {
      // no/!json body
    }
    if (!res.ok) {
      if (res.status === 400) {
        const err = new Error(body.statusMessage || body.message || 'Validation failed');
        err.fieldErrors = (body.data && body.data.errors) || {};
        throw err;
      }
      throw new Error(body.message || body.error || res.statusText);
    }
    return body;
  }

  async regenerateKeypair() {
    return this.call({
      method: 'post',
      path: '/server-settings/regenerate-keypair',
    });
  }

}

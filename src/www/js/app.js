/* eslint-disable no-console */
/* eslint-disable no-alert */
/* eslint-disable no-undef */
/* eslint-disable no-new */

'use strict';

const CHANGELOG_URL = 'https://raw.githubusercontent.com/creatorofuniverses/amnezia-wg-easy/production/docs/changelog.json';

function bytes(bytes, decimals, kib, maxunit) {
  kib = kib || false;
  if (bytes === 0) return '0 B';
  if (Number.isNaN(parseFloat(bytes)) && !Number.isFinite(bytes)) return 'NaN';
  const k = kib ? 1024 : 1000;
  const dm = decimals != null && !Number.isNaN(decimals) && decimals >= 0 ? decimals : 2;
  const sizes = kib
    ? ['B', 'KiB', 'MiB', 'GiB', 'TiB', 'PiB', 'EiB', 'ZiB', 'YiB', 'BiB']
    : ['B', 'KB', 'MB', 'GB', 'TB', 'PB', 'EB', 'ZB', 'YB', 'BB'];
  let i = Math.floor(Math.log(bytes) / Math.log(k));
  if (maxunit !== undefined) {
    const index = sizes.indexOf(maxunit);
    if (index !== -1) i = index;
  }
  // eslint-disable-next-line no-restricted-properties
  return `${parseFloat((bytes / Math.pow(k, i)).toFixed(dm))} ${sizes[i]}`;
}

function svIsIPv4(s) {
  const m = /^(\d{1,3})\.(\d{1,3})\.(\d{1,3})\.(\d{1,3})$/.exec(String(s).trim());
  return !!m && m.slice(1).every((o) => Number(o) >= 0 && Number(o) <= 255);
}
function svIsIPv6(s) {
  const t = String(s).trim();
  return /^[0-9a-fA-F:]+$/.test(t) && t.includes(':') && !/:::/.test(t) && (t.match(/:/g) || []).length <= 7;
}
function svIsIP(s) {
  return svIsIPv4(s) || svIsIPv6(s);
}
function svIsCIDR(s) {
  const parts = String(s).trim().split('/');
  if (parts.length !== 2) return false;
  const p = Number(parts[1]);
  if (!Number.isInteger(p) || p < 0) return false;
  if (svIsIPv4(parts[0])) return p <= 32;
  if (svIsIPv6(parts[0])) return p <= 128;
  return false;
}
function svInt(v, lo, hi) {
  if (String(v).trim() === '') return false;
  const n = Number(v);
  return Number.isInteger(n) && n >= lo && n <= hi;
}
// Mirrors src/lib/serverSettings.js for instant inline UX; the backend stays authoritative.
function validateServerDraft(d) {
  const e = {};
  if (!d.host || String(d.host).trim() === '') e.host = 'Required';
  if (!svInt(d.port, 1, 65535)) e.port = 'Port 1–65535';
  if (!(d.mtu === null || d.mtu === '' || svInt(d.mtu, 576, 1500))) e.mtu = 'MTU 576–1500 or empty';
  if (!String(d.dns).split(',').every((x) => svIsIP(x.trim()))) e.dns = 'Comma-separated IPs';
  if (!String(d.allowedIPs).split(',').every((x) => svIsCIDR(x.trim()))) e.allowedIPs = 'Comma-separated CIDRs';
  if (!/^(\d{1,3})\.(\d{1,3})\.(\d{1,3})\.x$/.test(String(d.defaultAddress))) {
    e.defaultAddress = 'Use a template like 10.8.0.x';
  }
  if (!svInt(d.persistentKeepalive, 0, 65535)) e.persistentKeepalive = 'Keepalive 0–65535';
  if (!svInt(d.jc, 1, 128)) e.jc = 'Jc 1–128';
  ['jmin', 'jmax', 's1', 's2', 's3', 's4'].forEach((k) => {
    if (!svInt(d[k], 0, 1280)) e[k] = `${k} 0–1280`;
  });
  if (!e.jmin && !e.jmax && Number(d.jmin) > Number(d.jmax)) e.jmax = 'Jmax ≥ Jmin';
  ['h1', 'h2', 'h3', 'h4'].forEach((k) => {
    const h = d[k];
    if (!h || typeof h !== 'object' || !svInt(h.min, 5, 2147483647) || !svInt(h.max, 5, 2147483647) || Number(h.min) > Number(h.max)) {
      e[k] = `${k} min ≤ max`;
    }
  });
  return e;
}

const i18n = new VueI18n({
  locale: localStorage.getItem('lang') || 'en',
  fallbackLocale: 'en',
  messages,
});

const UI_CHART_TYPES = [
  { type: false, strokeWidth: 0 },
  { type: 'line', strokeWidth: 3 },
  { type: 'area', strokeWidth: 0 },
  { type: 'bar', strokeWidth: 0 },
];

const CHART_COLORS = {
  rx: { light: 'rgba(13,148,136,0.55)', dark: 'rgba(45,212,191,0.6)' },
  tx: { light: 'rgba(13,148,136,0.85)', dark: 'rgba(45,212,191,0.9)' },
  gradient: { light: ['rgba(0,0,0,1.0)', 'rgba(0,0,0,1.0)'], dark: ['rgba(128,128,128,0)', 'rgba(128,128,128,0)'] },
};

new Vue({
  el: '#app',
  components: {
    apexchart: VueApexCharts,
  },
  i18n,
  data: {
    authenticated: null,
    authenticating: false,
    password: null,
    requiresPassword: null,

    clients: null,
    clientsPersist: {},
    clientDelete: null,
    clientCreate: null,
    clientCreateName: '',
    clientEditName: null,
    clientEditNameId: null,
    clientEditAddress: null,
    clientEditAddressId: null,
    qrcode: null,
    copiedClientId: null,

    view: 'clients',
    serverSettings: null,
    serverDraft: null,
    serverErrors: {},
    serverLoading: false,
    serverSaving: false,
    serverSaveResult: null,
    regenerateConfirm: false,
    obfExpanded: false,

    currentRelease: null,
    latestRelease: null,

    uiTrafficStats: false,

    uiChartType: 0,
    uiShowCharts: localStorage.getItem('uiShowCharts') === '1',
    uiTheme: localStorage.theme || 'auto',
    prefersDarkScheme: window.matchMedia('(prefers-color-scheme: dark)'),

    chartOptions: {
      chart: {
        background: 'transparent',
        stacked: false,
        toolbar: {
          show: false,
        },
        animations: {
          enabled: false,
        },
        parentHeightOffset: 0,
        sparkline: {
          enabled: true,
        },
      },
      colors: [],
      stroke: {
        curve: 'smooth',
      },
      fill: {
        type: 'gradient',
        gradient: {
          shade: 'dark',
          type: 'vertical',
          shadeIntensity: 0,
          gradientToColors: CHART_COLORS.gradient[this.theme],
          inverseColors: false,
          opacityTo: 0,
          stops: [0, 100],
        },
      },
      dataLabels: {
        enabled: false,
      },
      plotOptions: {
        bar: {
          horizontal: false,
        },
      },
      xaxis: {
        labels: {
          show: false,
        },
        axisTicks: {
          show: false,
        },
        axisBorder: {
          show: false,
        },
      },
      yaxis: {
        labels: {
          show: false,
        },
        min: 0,
      },
      tooltip: {
        enabled: false,
      },
      legend: {
        show: false,
      },
      grid: {
        show: false,
        padding: {
          left: -10,
          right: 0,
          bottom: -15,
          top: -15,
        },
        column: {
          opacity: 0,
        },
        xaxis: {
          lines: {
            show: false,
          },
        },
      },
    },
  },
  methods: {
    isConnected(client) {
      return client.latestHandshakeAt && ((new Date() - new Date(client.latestHandshakeAt)) < 1000 * 60 * 10);
    },
    dateTime: (value) => {
      return new Intl.DateTimeFormat(undefined, {
        year: 'numeric',
        month: 'short',
        day: 'numeric',
        hour: 'numeric',
        minute: 'numeric',
      }).format(value);
    },
    async refresh({
      updateCharts = false,
    } = {}) {
      if (!this.authenticated) return;

      const clients = await this.api.getClients();
      const prevById = {};
      (this.clients || []).forEach((c) => {
        prevById[c.id] = c;
      });
      this.clients = clients.map((client) => {
        // Preserve in-progress site-peer edits across the 1s refresh. Defining
        // these drafts here (before the assignment below) is what makes Vue
        // observe them, so the expander can v-model them instead of fighting
        // the refresh that replaces every client object each tick.
        const prev = prevById[client.id];
        client._allowedIPsDraft = prev && prev._allowedIPsDraft !== undefined
          ? prev._allowedIPsDraft
          : (client.allowedIPs || '');
        client._masqDraft = prev && prev._masqDraft !== undefined
          ? prev._masqDraft
          : !!client.siteMasquerade;

        if (client.name.includes('@') && client.name.includes('.')) {
          client.avatar = `https://gravatar.com/avatar/${sha256(client.name.toLowerCase().trim())}.jpg`;
        }

        if (!this.clientsPersist[client.id]) {
          this.clientsPersist[client.id] = {};
          this.clientsPersist[client.id].transferRxHistory = Array(50).fill(0);
          this.clientsPersist[client.id].transferRxPrevious = client.transferRx;
          this.clientsPersist[client.id].transferTxHistory = Array(50).fill(0);
          this.clientsPersist[client.id].transferTxPrevious = client.transferTx;
        }

        // Debug
        // client.transferRx = this.clientsPersist[client.id].transferRxPrevious + Math.random() * 1000;
        // client.transferTx = this.clientsPersist[client.id].transferTxPrevious + Math.random() * 1000;
        // client.latestHandshakeAt = new Date();
        // this.requiresPassword = true;

        this.clientsPersist[client.id].transferRxCurrent = client.transferRx - this.clientsPersist[client.id].transferRxPrevious;
        this.clientsPersist[client.id].transferRxPrevious = client.transferRx;
        this.clientsPersist[client.id].transferTxCurrent = client.transferTx - this.clientsPersist[client.id].transferTxPrevious;
        this.clientsPersist[client.id].transferTxPrevious = client.transferTx;

        if (updateCharts) {
          this.clientsPersist[client.id].transferRxHistory.push(this.clientsPersist[client.id].transferRxCurrent);
          this.clientsPersist[client.id].transferRxHistory.shift();

          this.clientsPersist[client.id].transferTxHistory.push(this.clientsPersist[client.id].transferTxCurrent);
          this.clientsPersist[client.id].transferTxHistory.shift();

          this.clientsPersist[client.id].transferTxSeries = [{
            name: 'Tx',
            data: this.clientsPersist[client.id].transferTxHistory,
          }];

          this.clientsPersist[client.id].transferRxSeries = [{
            name: 'Rx',
            data: this.clientsPersist[client.id].transferRxHistory,
          }];

          client.transferTxHistory = this.clientsPersist[client.id].transferTxHistory;
          client.transferRxHistory = this.clientsPersist[client.id].transferRxHistory;
          client.transferMax = Math.max(...client.transferTxHistory, ...client.transferRxHistory);

          client.transferTxSeries = this.clientsPersist[client.id].transferTxSeries;
          client.transferRxSeries = this.clientsPersist[client.id].transferRxSeries;
        }

        client.transferTxCurrent = this.clientsPersist[client.id].transferTxCurrent;
        client.transferRxCurrent = this.clientsPersist[client.id].transferRxCurrent;

        client.hoverTx = this.clientsPersist[client.id].hoverTx;
        client.hoverRx = this.clientsPersist[client.id].hoverRx;

        return client;
      });
    },
    login(e) {
      e.preventDefault();

      if (!this.password) return;
      if (this.authenticating) return;

      this.authenticating = true;
      this.api.createSession({
        password: this.password,
      })
        .then(async () => {
          const session = await this.api.getSession();
          this.authenticated = session.authenticated;
          this.requiresPassword = session.requiresPassword;
          return this.refresh();
        })
        .catch((err) => {
          alert(err.message || err.toString());
        })
        .finally(() => {
          this.authenticating = false;
          this.password = null;
        });
    },
    logout(e) {
      e.preventDefault();

      this.api.deleteSession()
        .then(() => {
          this.authenticated = false;
          this.clients = null;
        })
        .catch((err) => {
          alert(err.message || err.toString());
        });
    },
    createClient() {
      const name = this.clientCreateName;
      if (!name) return;

      this.api.createClient({ name })
        .catch((err) => alert(err.message || err.toString()))
        .finally(() => this.refresh().catch(console.error));
    },
    copyShareLink(client) {
      this.api.getClientShareString({ clientId: client.id })
        .then((link) => this.copyToClipboard(link))
        .then(() => {
          this.copiedClientId = client.id;
          setTimeout(() => {
            if (this.copiedClientId === client.id) this.copiedClientId = null;
          }, 1500);
        })
        .catch((err) => alert(err.message || err.toString()));
    },
    async copyToClipboard(text) {
      if (navigator.clipboard && window.isSecureContext) {
        await navigator.clipboard.writeText(text);
        return;
      }
      // Fallback for non-secure contexts (wg-easy is often served over plain HTTP).
      const ta = document.createElement('textarea');
      ta.value = text;
      ta.style.position = 'fixed';
      ta.style.top = '-1000px';
      ta.style.opacity = '0';
      document.body.appendChild(ta);
      ta.focus();
      ta.select();
      let ok = false;
      try {
        ok = document.execCommand('copy');
      } finally {
        document.body.removeChild(ta);
      }
      if (!ok) {
        // Last resort: show the string so the user can copy it manually.
        window.prompt('Copy this share link:', text);
      }
    },
    deleteClient(client) {
      this.api.deleteClient({ clientId: client.id })
        .catch((err) => alert(err.message || err.toString()))
        .finally(() => this.refresh().catch(console.error));
    },
    enableClient(client) {
      this.api.enableClient({ clientId: client.id })
        .catch((err) => alert(err.message || err.toString()))
        .finally(() => this.refresh().catch(console.error));
    },
    disableClient(client) {
      this.api.disableClient({ clientId: client.id })
        .catch((err) => alert(err.message || err.toString()))
        .finally(() => this.refresh().catch(console.error));
    },
    toggleClientLegacy(client) {
      const req = client.legacy
        ? this.api.disableClientLegacy({ clientId: client.id })
        : this.api.enableClientLegacy({ clientId: client.id });
      req
        .catch((err) => alert(err.message || err.toString()))
        .finally(() => this.refresh().catch(console.error));
    },
    updateClientName(client, name) {
      this.api.updateClientName({ clientId: client.id, name })
        .catch((err) => alert(err.message || err.toString()))
        .finally(() => this.refresh().catch(console.error));
    },
    updateClientAddress(client, address) {
      this.api.updateClientAddress({ clientId: client.id, address })
        .catch((err) => alert(err.message || err.toString()))
        .finally(() => this.refresh().catch(console.error));
    },
    saveClientSitePeer(client, allowedIPs, siteMasquerade) {
      // client-side guard mirrors the server (svIsCIDR already exists)
      const list = String(allowedIPs || '').split(',').map((s) => s.trim()).filter(Boolean);
      if (list.length && !list.every(svIsCIDR)) {
        alert(this.$t('allowedIPsInvalid'));
        return;
      }
      this.api.setClientSitePeer({ clientId: client.id, allowedIPs, siteMasquerade })
        .catch((err) => alert((err.fieldErrors && err.fieldErrors.allowedIPs) || err.message || err.toString()))
        .finally(() => this.refresh().catch(console.error));
    },
    toggleTheme() {
      const themes = ['light', 'dark', 'auto'];
      const currentIndex = themes.indexOf(this.uiTheme);
      const newIndex = (currentIndex + 1) % themes.length;
      this.uiTheme = themes[newIndex];
      localStorage.theme = this.uiTheme;
      this.setTheme(this.uiTheme);
    },
    setTheme(theme) {
      const { classList } = document.documentElement;
      const shouldAddDarkClass = theme === 'dark' || (theme === 'auto' && this.prefersDarkScheme.matches);
      classList.toggle('dark', shouldAddDarkClass);
    },
    handlePrefersChange(e) {
      if (localStorage.theme === 'auto') {
        this.setTheme(e.matches ? 'dark' : 'light');
      }
    },
    toggleCharts() {
      localStorage.setItem('uiShowCharts', this.uiShowCharts ? 1 : 0);
    },
    deepCopySettings(s) {
      return JSON.parse(JSON.stringify(s));
    },
    openServerSettings() {
      this.view = 'server-settings';
      this.serverErrors = {};
      this.serverSaveResult = null;
      this.serverLoading = true;
      this.api.getServerSettings()
        .then((s) => {
          this.serverSettings = s;
          this.serverDraft = this.deepCopySettings(s);
        })
        .catch((err) => alert(err.message || err.toString()))
        .finally(() => {
          this.serverLoading = false;
        });
    },
    closeServerSettings() {
      this.view = 'clients';
    },
    fieldErr(field) {
      return this.serverErrors[field] || this.serverClientErrors[field] || '';
    },
    saveServerSettings() {
      if (!this.serverCanSave) return;
      this.serverSaving = true;
      this.serverErrors = {};
      this.api.updateServerSettings(this.serverDraft)
        .then((res) => {
          this.serverSettings = res.settings;
          this.serverDraft = this.deepCopySettings(res.settings);
          this.serverSaveResult = { restarted: res.restarted, mustReimport: res.mustReimport };
        })
        .catch((err) => {
          if (err.fieldErrors) this.serverErrors = err.fieldErrors;
          else alert(err.message || err.toString());
        })
        .finally(() => {
          this.serverSaving = false;
        });
    },
    confirmRegenerateKeypair() {
      this.serverSaving = true;
      this.api.regenerateKeypair()
        .then((res) => {
          if (this.serverSettings) this.serverSettings.publicKey = res.publicKey;
          if (this.serverDraft) this.serverDraft.publicKey = res.publicKey;
          this.serverSaveResult = { restarted: true, mustReimport: true };
        })
        .catch((err) => alert(err.message || err.toString()))
        .finally(() => {
          this.serverSaving = false;
          this.regenerateConfirm = false;
        });
    },
  },
  filters: {
    bytes,
    timeago: (value) => {
      return timeago.format(value, i18n.locale);
    },
  },
  mounted() {
    this.prefersDarkScheme.addListener(this.handlePrefersChange);
    this.setTheme(this.uiTheme);

    this.api = new API();
    this.api.getSession()
      .then((session) => {
        this.authenticated = session.authenticated;
        this.requiresPassword = session.requiresPassword;
        this.refresh({
          updateCharts: this.updateCharts,
        }).catch((err) => {
          alert(err.message || err.toString());
        });
      })
      .catch((err) => {
        alert(err.message || err.toString());
      });

    setInterval(() => {
      this.refresh({
        updateCharts: this.updateCharts,
      }).catch(console.error);
    }, 1000);

    this.api.getuiTrafficStats()
      .then((res) => {
        this.uiTrafficStats = res;
      })
      .catch(() => {
        this.uiTrafficStats = false;
      });

    this.api.getChartType()
      .then((res) => {
        this.uiChartType = parseInt(res, 10);
      })
      .catch(() => {
        this.uiChartType = 0;
      });

    Promise.resolve().then(async () => {
      const lang = await this.api.getLang();
      if (lang !== localStorage.getItem('lang') && i18n.availableLocales.includes(lang)) {
        localStorage.setItem('lang', lang);
        i18n.locale = lang;
      }

      const checkUpdate = await this.api.getCheckUpdate();
      if (!checkUpdate) return;

      const currentRelease = await this.api.getRelease();
      const latestRelease = await fetch(CHANGELOG_URL)
        .then((res) => res.json())
        .then((releases) => {
          const releasesArray = Object.entries(releases).map(([version, changelog]) => ({
            version: parseInt(version, 10),
            changelog,
          }));
          releasesArray.sort((a, b) => {
            return b.version - a.version;
          });

          return releasesArray[0];
        });

      if (currentRelease >= latestRelease.version) return;

      this.currentRelease = currentRelease;
      this.latestRelease = latestRelease;
    }).catch((err) => console.error(err));
  },
  watch: {
    serverDraft: {
      deep: true,
      handler() {
        if (Object.keys(this.serverErrors).length) this.serverErrors = {};
        this.serverSaveResult = null;
      },
    },
  },
  computed: {
    chartOptionsTX() {
      const opts = {
        ...this.chartOptions,
        colors: [CHART_COLORS.tx[this.theme]],
      };
      opts.chart.type = UI_CHART_TYPES[this.uiChartType].type || false;
      opts.stroke.width = UI_CHART_TYPES[this.uiChartType].strokeWidth;
      return opts;
    },
    chartOptionsRX() {
      const opts = {
        ...this.chartOptions,
        colors: [CHART_COLORS.rx[this.theme]],
      };
      opts.chart.type = UI_CHART_TYPES[this.uiChartType].type || false;
      opts.stroke.width = UI_CHART_TYPES[this.uiChartType].strokeWidth;
      return opts;
    },
    updateCharts() {
      return this.uiChartType > 0 && this.uiShowCharts;
    },
    theme() {
      if (this.uiTheme === 'auto') {
        return this.prefersDarkScheme.matches ? 'dark' : 'light';
      }
      return this.uiTheme;
    },
    serverDirty() {
      return !!(this.serverSettings && this.serverDraft
        && JSON.stringify(this.serverSettings) !== JSON.stringify(this.serverDraft));
    },
    serverClientErrors() {
      if (!this.serverDraft) return {};
      return validateServerDraft(this.serverDraft);
    },
    serverValid() {
      return Object.keys(this.serverClientErrors).length === 0;
    },
    serverCanSave() {
      return this.serverDirty && this.serverValid && !this.serverSaving;
    },
  },
});

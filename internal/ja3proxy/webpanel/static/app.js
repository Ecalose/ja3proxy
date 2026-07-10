const elements = Object.fromEntries([
  "connection", "proxy-listen", "proxy-protocol", "fingerprint", "upstream", "upload-rate",
  "download-rate", "active-sessions", "total-sessions", "total-upload",
  "total-download", "uptime", "sessions", "events", "error-toast",
  "config-form", "tls-fingerprint", "proxy-protocol-choice", "traffic-page", "settings-page",
  "upstream-choice", "upstream-field", "upstream-input", "config-note", "proxy-port",
  "proxy-auth-choice", "proxy-username-field", "proxy-username", "proxy-password-field", "proxy-password"
].map(id => [id, document.getElementById(id)]));

let previous = null;
let filter = "all";
let configInitialized = false;
let configuredProxyAuthEnabled = false;

const escapeHTML = value => String(value ?? "")
  .replaceAll("&", "&amp;").replaceAll("<", "&lt;").replaceAll(">", "&gt;")
  .replaceAll('"', "&quot;").replaceAll("'", "&#039;");

function formatBytes(value, suffix = "") {
  const bytes = Number(value) || 0;
  const units = ["B", "KB", "MB", "GB", "TB"];
  let amount = bytes;
  let unit = 0;
  while (Math.abs(amount) >= 1000 && unit < units.length - 1) { amount /= 1000; unit += 1; }
  const digits = amount >= 100 || unit === 0 ? 0 : amount >= 10 ? 1 : 2;
  return `${amount.toFixed(digits)} ${units[unit]}${suffix}`;
}

function elapsed(from, to = Date.now()) {
  const seconds = Math.max(0, Math.floor((to - new Date(from).getTime()) / 1000));
  if (seconds < 60) return `${seconds}s`;
  if (seconds < 3600) return `${Math.floor(seconds / 60)}m ${seconds % 60}s`;
  return `${Math.floor(seconds / 3600)}h ${Math.floor((seconds % 3600) / 60)}m`;
}

function uptime(from) {
  const seconds = Math.max(0, Math.floor((Date.now() - new Date(from).getTime()) / 1000));
  const hours = Math.floor(seconds / 3600);
  const minutes = Math.floor((seconds % 3600) / 60);
  return `${String(hours).padStart(2, "0")}:${String(minutes).padStart(2, "0")}`;
}

function renderSessions(sessions) {
  const visible = (sessions || []).filter(session => filter === "all" || session.state === filter).slice(0, 40);
  if (!visible.length) {
    elements.sessions.innerHTML = `<tr><td colspan="6" class="empty">${filter === "all" ? "Waiting for the first connection." : `No ${escapeHTML(filter)} sessions.`}</td></tr>`;
    return;
  }
  elements.sessions.innerHTML = visible.map(session => {
    const state = escapeHTML(session.state || "closed");
    const traffic = `${formatBytes(session.uploadBytes)} ↑ · ${formatBytes(session.downloadBytes)} ↓`;
    const detail = session.sni && session.sni !== session.target ? session.sni : (session.error || "");
    return `<tr title="${escapeHTML(session.error)}">
      <td><span class="state state-${state}">${state}</span></td>
      <td>${escapeHTML(session.protocol || "—")}</td>
      <td class="target-cell"><strong>${escapeHTML(session.target || "—")}</strong>${detail ? `<small>${escapeHTML(detail)}</small>` : ""}</td>
      <td>${escapeHTML(session.clientAddr || "—")}</td>
      <td>${traffic}</td>
      <td>${elapsed(session.startedAt)}</td>
    </tr>`;
  }).join("");
}

function renderEvents(events) {
  const visible = [...(events || [])].reverse().slice(0, 10);
  if (!visible.length) { elements.events.innerHTML = '<li class="empty">No events recorded.</li>'; return; }
  elements.events.innerHTML = visible.map(event => `<li class="${event.level === "warn" ? "warn" : ""}">
    <time>${new Date(event.time).toLocaleTimeString([], { hour12: false })}</time>
    <strong>${escapeHTML(event.message)}</strong>
    <p>${escapeHTML(event.target || event.error || event.protocol || "Runtime event")}</p>
  </li>`).join("");
}

function setSelectOptions(select, options, selected) {
  select.innerHTML = options.map(option => {
    const value = typeof option === "string" ? option : option.value;
    const label = typeof option === "string" ? option : option.label;
    return `<option value="${escapeHTML(value)}"${value === selected ? " selected" : ""}>${escapeHTML(label)}</option>`;
  }).join("");
}

function syncConfig(runtime, force = false) {
  if (configInitialized && !force) return;

  const currentFingerprint = `${runtime.tlsClient || "Golang"}@${runtime.tlsVersion || "0"}`;
  const fingerprints = [...new Set([currentFingerprint, ...(runtime.tlsFingerprints || [])])];
  setSelectOptions(elements["tls-fingerprint"], fingerprints, currentFingerprint);
  elements["tls-fingerprint"].disabled = runtime.configurationMode === "fingerprint-file";

  elements["proxy-port"].value = runtime.proxyPort || "";
  elements["proxy-protocol-choice"].value = runtime.proxyProtocol || "mixed";
  elements["proxy-auth-choice"].value = runtime.proxyAuthEnabled ? "enabled" : "disabled";
  configuredProxyAuthEnabled = Boolean(runtime.proxyAuthEnabled);
  elements["proxy-username"].value = runtime.proxyUsername || "";
  elements["proxy-password"].value = "";
  syncProxyAuthFields();

  const upstreamOptions = [{ value: "direct", label: "Direct connection" }];
  if (runtime.upstreamEnabled) upstreamOptions.push({ value: "current", label: runtime.upstream });
  upstreamOptions.push({ value: "custom", label: "Custom proxy…" });
  setSelectOptions(elements["upstream-choice"], upstreamOptions, runtime.upstreamEnabled ? "current" : "direct");
  elements["upstream-field"].hidden = true;
  elements["upstream-input"].value = "";

  elements["config-note"].textContent = runtime.configurationMode === "fingerprint-file"
    ? "TLS fingerprint is file-managed; upstream changes remain available."
    : "Changes apply to new connections without interrupting active sessions.";
  elements["config-note"].className = "config-note";
  configInitialized = true;
}

function render(data) {
  const traffic = data.traffic;
  const runtime = data.runtime;
  const now = new Date(traffic.capturedAt);
  let uploadRate = 0;
  let downloadRate = 0;
  if (previous) {
    const interval = Math.max(.1, (now - new Date(previous.capturedAt)) / 1000);
    uploadRate = Math.max(0, (traffic.totalUploadBytes - previous.totalUploadBytes) / interval);
    downloadRate = Math.max(0, (traffic.totalDownloadBytes - previous.totalDownloadBytes) / interval);
  }

  elements["proxy-listen"].textContent = runtime.proxyListen || "—";
  const protocolLabels = { mixed: "HTTP + SOCKS5", http: "HTTP", socks5: "SOCKS5" };
  elements["proxy-protocol"].textContent = protocolLabels[runtime.proxyProtocol] || "HTTP + SOCKS5";
  elements.fingerprint.textContent = `${runtime.tlsClient || "—"}@${runtime.tlsVersion || "—"}`;
  elements.upstream.textContent = runtime.upstream || "DIRECT";
  elements["upload-rate"].textContent = formatBytes(uploadRate, "/s");
  elements["download-rate"].textContent = formatBytes(downloadRate, "/s");
  elements["active-sessions"].textContent = traffic.activeSessions.toLocaleString();
  elements["total-sessions"].textContent = traffic.totalSessions.toLocaleString();
  elements["total-upload"].textContent = formatBytes(traffic.totalUploadBytes);
  elements["total-download"].textContent = formatBytes(traffic.totalDownloadBytes);
  elements.uptime.textContent = uptime(traffic.startedAt);
  renderSessions(traffic.sessions);
  renderEvents(traffic.events);
  syncConfig(runtime);
  previous = traffic;
}

async function refresh() {
  try {
    const response = await fetch("/api/state", { cache: "no-store" });
    if (!response.ok) throw new Error(`HTTP ${response.status}`);
    render(await response.json());
    elements.connection.className = "connection live";
    elements.connection.innerHTML = "<i></i>Live";
    elements["error-toast"].classList.remove("show");
  } catch (_) {
    elements.connection.className = "connection lost";
    elements.connection.innerHTML = "<i></i>Signal lost";
    elements["error-toast"].classList.add("show");
  }
}

document.querySelectorAll("[data-filter]").forEach(button => button.addEventListener("click", () => {
  filter = button.dataset.filter;
  document.querySelectorAll("[data-filter]").forEach(item => item.classList.toggle("active", item === button));
  if (previous) renderSessions(previous.sessions);
}));

function activateTab(name, updateHash = true) {
  document.querySelectorAll("[data-tab]").forEach(tab => {
    const active = tab.dataset.tab === name;
    tab.classList.toggle("active", active);
    tab.setAttribute("aria-selected", String(active));
    tab.tabIndex = active ? 0 : -1;
  });
  elements["traffic-page"].hidden = name !== "traffic";
  elements["settings-page"].hidden = name !== "settings";
  if (updateHash) history.replaceState(null, "", name === "settings" ? "#settings" : location.pathname);
}

document.querySelectorAll("[data-tab]").forEach(tab => {
  tab.addEventListener("click", () => activateTab(tab.dataset.tab));
  tab.addEventListener("keydown", event => {
    if (event.key !== "ArrowLeft" && event.key !== "ArrowRight") return;
    const target = tab.dataset.tab === "traffic" ? "settings" : "traffic";
    activateTab(target);
    document.querySelector(`[data-tab="${target}"]`).focus();
  });
});

elements["upstream-choice"].addEventListener("change", () => {
  const custom = elements["upstream-choice"].value === "custom";
  elements["upstream-field"].hidden = !custom;
  if (custom) elements["upstream-input"].focus();
});

function syncProxyAuthFields() {
  const enabled = elements["proxy-auth-choice"].value === "enabled";
  elements["proxy-username-field"].hidden = !enabled;
  elements["proxy-password-field"].hidden = !enabled;
}

elements["proxy-auth-choice"].addEventListener("change", () => {
  syncProxyAuthFields();
  if (elements["proxy-auth-choice"].value === "enabled") elements["proxy-username"].focus();
});

elements["config-form"].addEventListener("submit", async event => {
  event.preventDefault();
  const submit = event.submitter;
  const payload = {};
  const proxyPort = Number(elements["proxy-port"].value);
  if (!Number.isInteger(proxyPort) || proxyPort < 1 || proxyPort > 65535) {
    elements["config-note"].textContent = "Enter a proxy port between 1 and 65535.";
    elements["config-note"].className = "config-note error";
    elements["proxy-port"].focus();
    return;
  }
  payload.proxyPort = proxyPort;
  payload.proxyProtocol = elements["proxy-protocol-choice"].value;

  const proxyAuthEnabled = elements["proxy-auth-choice"].value === "enabled";
  payload.proxyAuthEnabled = proxyAuthEnabled;
  if (proxyAuthEnabled) {
    const username = elements["proxy-username"].value.trim();
    if (!username) {
      elements["config-note"].textContent = "Enter a client authentication username.";
      elements["config-note"].className = "config-note error";
      elements["proxy-username"].focus();
      return;
    }
    payload.proxyUsername = username;
    const password = elements["proxy-password"].value;
    if (!configuredProxyAuthEnabled && !password) {
      elements["config-note"].textContent = "Enter a client authentication password.";
      elements["config-note"].className = "config-note error";
      elements["proxy-password"].focus();
      return;
    }
    if (password) payload.proxyPassword = password;
  }
  if (!elements["tls-fingerprint"].disabled) payload.tlsFingerprint = elements["tls-fingerprint"].value;

  const upstreamChoice = elements["upstream-choice"].value;
  if (upstreamChoice === "direct") payload.upstream = "";
  if (upstreamChoice === "custom") {
    const upstream = elements["upstream-input"].value.trim();
    if (!upstream) {
      elements["config-note"].textContent = "Enter a SOCKS5 or HTTP proxy URL.";
      elements["config-note"].className = "config-note error";
      elements["upstream-input"].focus();
      return;
    }
    payload.upstream = upstream;
  }

  if (!Object.keys(payload).length) {
    elements["config-note"].textContent = "No configuration change selected.";
    elements["config-note"].className = "config-note";
    return;
  }

  submit.disabled = true;
  elements["config-note"].className = "config-note";
  elements["config-note"].textContent = "Applying…";
  try {
    const response = await fetch("/api/config", {
      method: "PUT",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(payload)
    });
    const result = await response.json();
    if (!response.ok) throw new Error(result.error || `HTTP ${response.status}`);
    configInitialized = false;
    syncConfig(result.runtime, true);
    elements["config-note"].textContent = "Applied to new connections.";
    elements["config-note"].className = "config-note success";
    await refresh();
  } catch (error) {
    elements["config-note"].textContent = error.message || "Configuration update failed.";
    elements["config-note"].className = "config-note error";
  } finally {
    submit.disabled = false;
  }
});

activateTab(location.hash === "#settings" ? "settings" : "traffic", false);
refresh();
setInterval(refresh, 1000);

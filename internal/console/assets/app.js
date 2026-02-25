const page = document.body.dataset.page || "stats";
const msgEl = document.getElementById("msg");

const statsEls = {
  inFlight: document.getElementById("st-inflight"),
  total: document.getElementById("st-total"),
  errors: document.getElementById("st-errors"),
  inBytes: document.getElementById("st-in"),
  outBytes: document.getElementById("st-out"),
};

const activityBody = document.getElementById("activity-body");
const rulesBody = document.getElementById("rules-body");
const upstreamsBody = document.getElementById("upstreams-body");

let rulesCache = [];

function showMsg(text, isError = false) {
  if (!msgEl) return;
  msgEl.textContent = text;
  msgEl.style.color = isError ? "#9c2f1f" : "#5c6b70";
}

async function api(path, options = {}) {
  const resp = await fetch(path, {
    headers: { "Content-Type": "application/json", ...(options.headers || {}) },
    ...options,
  });
  if (!resp.ok) {
    const body = await resp.text();
    throw new Error(body || `请求失败: ${resp.status}`);
  }
  const contentType = resp.headers.get("Content-Type") || "";
  if (contentType.includes("application/json")) {
    return resp.json();
  }
  return null;
}

function fmtBytes(n) {
  const units = ["B", "KB", "MB", "GB"];
  let value = Number(n || 0);
  let idx = 0;
  while (value >= 1024 && idx < units.length - 1) {
    value /= 1024;
    idx += 1;
  }
  return `${value.toFixed(value < 10 && idx > 0 ? 1 : 0)} ${units[idx]}`;
}

function fmtTime(ts) {
  const d = new Date(ts);
  if (Number.isNaN(d.getTime())) return "-";
  return d.toLocaleTimeString();
}

function appendActivity(ev) {
  if (!activityBody) return;
  const tr = document.createElement("tr");
  tr.innerHTML = `
    <td>${fmtTime(ev.time)}</td>
    <td>${ev.client || "-"}</td>
    <td>${ev.method || "-"}</td>
    <td>${ev.host || "-"}</td>
    <td>${ev.action || "-"}</td>
    <td>${ev.status ?? "-"}</td>
    <td>${ev.duration_ms ?? "-"}</td>
  `;
  activityBody.prepend(tr);
  while (activityBody.children.length > 200) {
    activityBody.removeChild(activityBody.lastElementChild);
  }
}

function renderRules() {
  if (!rulesBody) return;
  rulesBody.innerHTML = "";
  rulesCache.forEach((rule) => {
    const tr = document.createElement("tr");
    tr.innerHTML = `
      <td>${fmtTime(rule.created_at)}</td>
      <td>${rule.id}</td>
      <td>${rule.pattern}</td>
      <td>${rule.action}${rule.upstream_id ? `(${rule.upstream_id})` : ""}${rule.block_status ? `:${rule.block_status}` : ""}</td>
      <td>${rule.enabled ? "是" : "否"}</td>
      <td>
        <button class="danger" data-op="del" data-id="${rule.id}">删除</button>
      </td>
    `;
    rulesBody.appendChild(tr);
  });
}

function renderUpstreams(items) {
  if (!upstreamsBody) return;
  upstreamsBody.innerHTML = "";
  items.forEach((up) => {
    const tr = document.createElement("tr");
    tr.innerHTML = `
      <td>${up.id}</td>
      <td>${up.url}</td>
      <td>${up.enabled ? "是" : "否"}</td>
      <td><button class="danger" data-up-del="${up.id}">删除</button></td>
    `;
    upstreamsBody.appendChild(tr);
  });
}

async function loadStats() {
  const st = await api("/api/stats");
  if (statsEls.inFlight) statsEls.inFlight.textContent = st.in_flight;
  if (statsEls.total) statsEls.total.textContent = st.total_requests;
  if (statsEls.errors) statsEls.errors.textContent = st.total_errors;
  if (statsEls.inBytes) statsEls.inBytes.textContent = fmtBytes(st.bytes_in);
  if (statsEls.outBytes) statsEls.outBytes.textContent = fmtBytes(st.bytes_out);
}

async function loadActivities() {
  if (!activityBody) return;
  const events = await api("/api/activities?limit=50");
  activityBody.innerHTML = "";
  events.reverse().forEach(appendActivity);
}

async function loadRules() {
  rulesCache = await api("/api/rules");
  renderRules();
}

async function loadUpstreams() {
  const items = await api("/api/upstreams");
  renderUpstreams(items);
}

function startSSE() {
  if (!activityBody) return;
  const es = new EventSource("/api/activities/stream");
  es.onmessage = (evt) => {
    try {
      appendActivity(JSON.parse(evt.data));
    } catch (_) {
      return;
    }
  };
  es.onerror = () => {
    showMsg("实时活动流连接中断，正在等待浏览器重连...", true);
  };
}

function bindRulesEvents() {
  const ruleForm = document.getElementById("rule-form");
  if (!ruleForm || !rulesBody) return;

  ruleForm.addEventListener("submit", async (e) => {
    e.preventDefault();
    const form = new FormData(e.currentTarget);
    const payload = {
      id: form.get("id").trim(),
      enabled: form.get("enabled") === "on",
      pattern: form.get("pattern").trim(),
      action: form.get("action"),
    };
    const upstreamId = form.get("upstream_id").trim();
    const blockStatus = form.get("block_status").trim();
    if (upstreamId) payload.upstream_id = upstreamId;
    if (blockStatus) payload.block_status = Number(blockStatus);

    try {
      await api("/api/rules", { method: "POST", body: JSON.stringify(payload) });
      e.currentTarget.reset();
      await loadRules();
      showMsg("规则新增成功");
    } catch (err) {
      showMsg(`规则新增失败: ${err.message}`, true);
    }
  });

  rulesBody.addEventListener("click", async (e) => {
    const btn = e.target.closest("button");
    if (!btn) return;
    const id = btn.dataset.id;
    const op = btn.dataset.op;
    if (!id || !op) return;

    try {
      if (op === "del") {
        await api(`/api/rules/${encodeURIComponent(id)}`, { method: "DELETE" });
        await loadRules();
        showMsg("规则已删除");
        return;
      }
    } catch (err) {
      showMsg(`规则操作失败: ${err.message}`, true);
    }
  });
}

function bindUpstreamEvents() {
  const upstreamForm = document.getElementById("upstream-form");
  if (!upstreamForm || !upstreamsBody) return;

  upstreamForm.addEventListener("submit", async (e) => {
    e.preventDefault();
    const form = new FormData(e.currentTarget);
    const payload = {
      id: form.get("id").trim(),
      url: form.get("url").trim(),
      enabled: form.get("enabled") === "on",
    };

    try {
      await api("/api/upstreams", { method: "POST", body: JSON.stringify(payload) });
      e.currentTarget.reset();
      await loadUpstreams();
      showMsg("上游新增成功");
    } catch (err) {
      showMsg(`上游新增失败: ${err.message}`, true);
    }
  });

  upstreamsBody.addEventListener("click", async (e) => {
    const btn = e.target.closest("button[data-up-del]");
    if (!btn) return;
    const id = btn.dataset.upDel;
    try {
      await api(`/api/upstreams/${encodeURIComponent(id)}`, { method: "DELETE" });
      await loadUpstreams();
      showMsg("上游已删除");
    } catch (err) {
      showMsg(`上游删除失败: ${err.message}`, true);
    }
  });
}

async function init() {
  try {
    if (page === "stats") {
      await loadStats();
      setInterval(() => {
        loadStats().catch((err) => showMsg(`统计刷新失败: ${err.message}`, true));
      }, 2000);
      showMsg("统计页已就绪");
      return;
    }

    if (page === "activities") {
      await loadActivities();
      startSSE();
      showMsg("活动页已就绪");
      return;
    }

    if (page === "rules") {
      bindRulesEvents();
      await loadRules();
      showMsg("规则页已就绪");
      return;
    }

    if (page === "upstreams") {
      bindUpstreamEvents();
      await loadUpstreams();
      showMsg("上游页已就绪");
      return;
    }

    showMsg("未知页面", true);
  } catch (err) {
    showMsg(`初始化失败: ${err.message}`, true);
  }
}

init();

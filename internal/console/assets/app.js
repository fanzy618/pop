const statsEls = {
  inFlight: document.getElementById("st-inflight"),
  total: document.getElementById("st-total"),
  errors: document.getElementById("st-errors"),
  inBytes: document.getElementById("st-in"),
  outBytes: document.getElementById("st-out"),
};

const msgEl = document.getElementById("msg");
const activityBody = document.getElementById("activity-body");
const rulesBody = document.getElementById("rules-body");
const upstreamsBody = document.getElementById("upstreams-body");

let rulesCache = [];

function showMsg(text, isError = false) {
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
  rulesBody.innerHTML = "";
  const sorted = [...rulesCache].sort((a, b) => a.order - b.order);
  sorted.forEach((rule) => {
    const tr = document.createElement("tr");
    tr.innerHTML = `
      <td>${rule.order}</td>
      <td>${rule.id}</td>
      <td>${rule.pattern}</td>
      <td>${rule.action}${rule.upstream_id ? `(${rule.upstream_id})` : ""}${rule.block_status ? `:${rule.block_status}` : ""}</td>
      <td>${rule.enabled ? "是" : "否"}</td>
      <td>
        <button class="secondary" data-op="up" data-id="${rule.id}">上移</button>
        <button class="secondary" data-op="down" data-id="${rule.id}">下移</button>
        <button class="danger" data-op="del" data-id="${rule.id}">删除</button>
      </td>
    `;
    rulesBody.appendChild(tr);
  });
}

function renderUpstreams(items) {
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
  statsEls.inFlight.textContent = st.in_flight;
  statsEls.total.textContent = st.total_requests;
  statsEls.errors.textContent = st.total_errors;
  statsEls.inBytes.textContent = fmtBytes(st.bytes_in);
  statsEls.outBytes.textContent = fmtBytes(st.bytes_out);
}

async function loadActivities() {
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
  const es = new EventSource("/api/activities/stream");
  es.onmessage = (evt) => {
    try {
      appendActivity(JSON.parse(evt.data));
    } catch (_) {
      // ignore malformed event
    }
  };
  es.onerror = () => {
    showMsg("实时活动流连接中断，正在等待浏览器重连...", true);
  };
}

document.getElementById("rule-form").addEventListener("submit", async (e) => {
  e.preventDefault();
  const form = new FormData(e.currentTarget);
  const payload = {
    id: form.get("id").trim(),
    enabled: form.get("enabled") === "on",
    order: Number(form.get("order")),
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

    const ordered = [...rulesCache].sort((a, b) => a.order - b.order).map((r) => r.id);
    const idx = ordered.indexOf(id);
    if (idx === -1) return;
    if (op === "up" && idx > 0) {
      [ordered[idx - 1], ordered[idx]] = [ordered[idx], ordered[idx - 1]];
    }
    if (op === "down" && idx < ordered.length - 1) {
      [ordered[idx + 1], ordered[idx]] = [ordered[idx], ordered[idx + 1]];
    }
    await api("/api/rules/reorder", { method: "POST", body: JSON.stringify({ ids: ordered }) });
    await loadRules();
    showMsg("规则顺序已更新");
  } catch (err) {
    showMsg(`规则操作失败: ${err.message}`, true);
  }
});

document.getElementById("upstream-form").addEventListener("submit", async (e) => {
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

async function init() {
  try {
    await Promise.all([loadStats(), loadActivities(), loadRules(), loadUpstreams()]);
    setInterval(() => {
      loadStats().catch((err) => showMsg(`统计刷新失败: ${err.message}`, true));
    }, 2000);
    startSSE();
    showMsg("控制台已就绪");
  } catch (err) {
    showMsg(`初始化失败: ${err.message}`, true);
  }
}

init();

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
let upstreamsCache = [];
let editingRuleID = 0;
let editingUpstreamID = 0;

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

function fmtDateTime(ts) {
  const d = new Date(ts);
  if (Number.isNaN(d.getTime())) return "-";
  return d.toLocaleString();
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

function upstreamLabel(upstream) {
  if (!upstream) return "-";
  const name = (upstream.name || "").trim();
  if (name) return name;
  return upstream.url || `#${upstream.id}`;
}

function findUpstreamByID(id) {
  return upstreamsCache.find((up) => Number(up.id) === Number(id));
}

function routeOptionLabel(upstream) {
  return `上游: ${upstreamLabel(upstream)} (#${upstream.id})`;
}

function ruleRouteValue(rule) {
  if (!rule) return "DIRECT";
  if (rule.action === "DIRECT") return "DIRECT";
  if (rule.action === "BLOCK") return "BLOCK";
  if (rule.action === "PROXY" && rule.upstream_id) {
    return `UPSTREAM:${rule.upstream_id}`;
  }
  return "DIRECT";
}

function renderRouteOptions(selectEl, selectedValue = "DIRECT") {
  if (!selectEl) return;
  selectEl.innerHTML = "";

  const base = [
    { value: "DIRECT", text: "直连 (DIRECT)" },
    { value: "BLOCK", text: "阻断 (BLOCK 404)" },
  ];
  base.forEach((item) => {
    const opt = document.createElement("option");
    opt.value = item.value;
    opt.textContent = item.text;
    selectEl.appendChild(opt);
  });

  upstreamsCache.forEach((up) => {
    const opt = document.createElement("option");
    opt.value = `UPSTREAM:${up.id}`;
    opt.textContent = routeOptionLabel(up);
    selectEl.appendChild(opt);
  });

  if (selectedValue.startsWith("UPSTREAM:")) {
    const refID = Number(selectedValue.replace("UPSTREAM:", ""));
    if (!Number.isNaN(refID) && !findUpstreamByID(refID)) {
      const opt = document.createElement("option");
      opt.value = selectedValue;
      opt.textContent = `上游: #${refID} (不存在)`;
      selectEl.appendChild(opt);
    }
  }

  selectEl.value = selectedValue;
}

function parseRouteTarget(value) {
  if (value === "DIRECT") {
    return { action: "DIRECT" };
  }
  if (value === "BLOCK") {
    return { action: "BLOCK", block_status: 404 };
  }
  if (value.startsWith("UPSTREAM:")) {
    const upstreamID = Number(value.replace("UPSTREAM:", ""));
    if (!Number.isInteger(upstreamID) || upstreamID <= 0) {
      throw new Error("上游选择无效");
    }
    return { action: "PROXY", upstream_id: upstreamID };
  }
  throw new Error("动作选择无效");
}

function renderRules() {
  if (!rulesBody) return;
  rulesBody.innerHTML = "";
  const items = Array.isArray(rulesCache) ? rulesCache : [];
  items.forEach((rule) => {
    const tr = document.createElement("tr");
    let actionText = rule.action;
    if (rule.action === "BLOCK") {
      actionText = "BLOCK:404";
    } else if (rule.action === "PROXY") {
      const up = findUpstreamByID(rule.upstream_id);
      actionText = `PROXY(${upstreamLabel(up)})`;
    }

    tr.innerHTML = `
      <td>${fmtDateTime(rule.created_at)}</td>
      <td>${rule.id}</td>
      <td>${rule.pattern}</td>
      <td>${actionText}</td>
      <td>${rule.enabled ? "是" : "否"}</td>
      <td>
        <button class="secondary" data-op="edit" data-id="${rule.id}">编辑</button>
        <button class="danger" data-op="del" data-id="${rule.id}">删除</button>
      </td>
    `;
    rulesBody.appendChild(tr);
  });
}

function renderUpstreams() {
  if (!upstreamsBody) return;
  upstreamsBody.innerHTML = "";
  upstreamsCache.forEach((up) => {
    const tr = document.createElement("tr");
    tr.innerHTML = `
      <td>${up.id}</td>
      <td>${up.name || "-"}</td>
      <td>${up.url}</td>
      <td>${up.enabled ? "是" : "否"}</td>
      <td>
        <button class="secondary" data-up-op="edit" data-up-id="${up.id}">编辑</button>
        <button class="danger" data-up-op="del" data-up-id="${up.id}">删除</button>
      </td>
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
  const items = await api("/api/rules");
  rulesCache = Array.isArray(items) ? items : [];
  renderRules();
}

async function loadUpstreams() {
  const items = await api("/api/upstreams");
  upstreamsCache = Array.isArray(items) ? items : [];
  renderUpstreams();
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
  const ruleCancel = document.getElementById("rule-cancel");
  if (!ruleForm || !rulesBody || !ruleCancel) return;

  const patternInput = ruleForm.elements.namedItem("pattern");
  const routeTargetInput = ruleForm.elements.namedItem("route_target");
  const enabledInput = ruleForm.elements.namedItem("enabled");
  const submitBtn = ruleForm.querySelector("button[type='submit']");

  function resetRuleForm() {
    editingRuleID = 0;
    ruleForm.reset();
    if (enabledInput) enabledInput.checked = true;
    renderRouteOptions(routeTargetInput, "DIRECT");
    if (submitBtn) submitBtn.textContent = "新增规则";
    ruleCancel.classList.add("hidden");
  }

  function enterRuleEdit(rule) {
    editingRuleID = Number(rule.id);
    if (patternInput) patternInput.value = rule.pattern || "";
    renderRouteOptions(routeTargetInput, ruleRouteValue(rule));
    if (enabledInput) enabledInput.checked = !!rule.enabled;
    if (submitBtn) submitBtn.textContent = "更新规则";
    ruleCancel.classList.remove("hidden");
  }

  ruleCancel.addEventListener("click", () => {
    resetRuleForm();
    showMsg("已取消规则编辑");
  });

  ruleForm.addEventListener("submit", async (e) => {
    e.preventDefault();
    const form = new FormData(e.currentTarget);
    const payload = {
      enabled: form.get("enabled") === "on",
      pattern: String(form.get("pattern") || "").trim(),
      ...parseRouteTarget(String(form.get("route_target") || "DIRECT")),
    };

    try {
      if (editingRuleID > 0) {
        await api(`/api/rules/${editingRuleID}`, { method: "PUT", body: JSON.stringify(payload) });
      } else {
        await api("/api/rules", { method: "POST", body: JSON.stringify(payload) });
      }
      await loadRules();
      showMsg(editingRuleID > 0 ? "规则更新成功" : "规则新增成功");
      resetRuleForm();
    } catch (err) {
      showMsg(`${editingRuleID > 0 ? "规则更新" : "规则新增"}失败: ${err.message}`, true);
    }
  });

  rulesBody.addEventListener("click", async (e) => {
    const btn = e.target.closest("button");
    if (!btn) return;
    const id = Number(btn.dataset.id || 0);
    const op = btn.dataset.op;
    if (id <= 0 || !op) return;

    try {
      if (op === "edit") {
        const rule = rulesCache.find((item) => Number(item.id) === id);
        if (!rule) return;
        enterRuleEdit(rule);
        showMsg(`正在编辑规则: ${id}`);
        return;
      }

      if (op === "del") {
        await api(`/api/rules/${id}`, { method: "DELETE" });
        await loadRules();
        if (id === editingRuleID) {
          resetRuleForm();
        }
        showMsg("规则已删除");
      }
    } catch (err) {
      showMsg(`规则操作失败: ${err.message}`, true);
    }
  });

  resetRuleForm();
}

function bindUpstreamEvents() {
  const upstreamForm = document.getElementById("upstream-form");
  const upstreamCancel = document.getElementById("upstream-cancel");
  if (!upstreamForm || !upstreamsBody || !upstreamCancel) return;

  const nameInput = upstreamForm.elements.namedItem("name");
  const urlInput = upstreamForm.elements.namedItem("url");
  const enabledInput = upstreamForm.elements.namedItem("enabled");
  const submitBtn = upstreamForm.querySelector("button[type='submit']");

  function resetUpstreamForm() {
    editingUpstreamID = 0;
    upstreamForm.reset();
    if (enabledInput) enabledInput.checked = true;
    if (submitBtn) submitBtn.textContent = "新增上游";
    upstreamCancel.classList.add("hidden");
  }

  function enterUpstreamEdit(item) {
    editingUpstreamID = Number(item.id);
    if (nameInput) nameInput.value = item.name || "";
    if (urlInput) urlInput.value = item.url || "";
    if (enabledInput) enabledInput.checked = !!item.enabled;
    if (submitBtn) submitBtn.textContent = "更新上游";
    upstreamCancel.classList.remove("hidden");
  }

  upstreamCancel.addEventListener("click", () => {
    resetUpstreamForm();
    showMsg("已取消上游编辑");
  });

  upstreamForm.addEventListener("submit", async (e) => {
    e.preventDefault();
    const form = new FormData(e.currentTarget);
    const payload = {
      name: String(form.get("name") || "").trim(),
      url: String(form.get("url") || "").trim(),
      enabled: form.get("enabled") === "on",
    };

    try {
      if (editingUpstreamID > 0) {
        await api(`/api/upstreams/${editingUpstreamID}`, { method: "PUT", body: JSON.stringify(payload) });
      } else {
        await api("/api/upstreams", { method: "POST", body: JSON.stringify(payload) });
      }
      await loadUpstreams();
      showMsg(editingUpstreamID > 0 ? "上游更新成功" : "上游新增成功");
      resetUpstreamForm();
    } catch (err) {
      showMsg(`${editingUpstreamID > 0 ? "上游更新" : "上游新增"}失败: ${err.message}`, true);
    }
  });

  upstreamsBody.addEventListener("click", async (e) => {
    const btn = e.target.closest("button[data-up-op]");
    if (!btn) return;
    const op = btn.dataset.upOp;
    const id = Number(btn.dataset.upId || 0);
    if (id <= 0 || !op) return;

    try {
      if (op === "edit") {
        const item = upstreamsCache.find((up) => Number(up.id) === id);
        if (!item) return;
        enterUpstreamEdit(item);
        showMsg(`正在编辑上游: ${id}`);
        return;
      }

      if (op === "del") {
        await api(`/api/upstreams/${id}`, { method: "DELETE" });
        await loadUpstreams();
        if (id === editingUpstreamID) {
          resetUpstreamForm();
        }
        showMsg("上游已删除");
      }
    } catch (err) {
      showMsg(`上游删除失败: ${err.message}`, true);
    }
  });

  resetUpstreamForm();
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
      await loadUpstreams();
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

    if (page === "data") {
      const backupBtn = document.getElementById("backup-download");
      const restoreBtn = document.getElementById("restore-submit");
      const restoreFile = document.getElementById("restore-file");

      if (backupBtn) {
        backupBtn.addEventListener("click", async () => {
          try {
            const payload = await api("/api/data/backup");
            const stamp = new Date().toISOString().replace(/[:]/g, "-");
            const filename = `pop-backup-${stamp}.json`;
            const blob = new Blob([JSON.stringify(payload, null, 2)], { type: "application/json" });
            const url = URL.createObjectURL(blob);
            const a = document.createElement("a");
            a.href = url;
            a.download = filename;
            document.body.appendChild(a);
            a.click();
            a.remove();
            URL.revokeObjectURL(url);
            showMsg("备份下载成功");
          } catch (err) {
            showMsg(`备份失败: ${err.message}`, true);
          }
        });
      }

      if (restoreBtn && restoreFile) {
        restoreBtn.addEventListener("click", async () => {
          try {
            const file = restoreFile.files && restoreFile.files[0];
            if (!file) {
              throw new Error("请先选择备份文件");
            }
            const text = await file.text();
            const payload = JSON.parse(text);
            await api("/api/data/restore", {
              method: "POST",
              body: JSON.stringify(payload),
            });
            showMsg("恢复成功，规则与上游已热更新");
          } catch (err) {
            showMsg(`恢复失败: ${err.message}`, true);
          }
        });
      }

      showMsg("数据管理页已就绪");
      return;
    }

    showMsg("未知页面", true);
  } catch (err) {
    showMsg(`初始化失败: ${err.message}`, true);
  }
}

init();

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
const ruleSearchInput = document.getElementById("rule-search");
const rulesSummaryEl = document.getElementById("rules-summary");
const rulesPageInfoEl = document.getElementById("rules-page-info");
const rulesPrevPageBtn = document.getElementById("rules-prev-page");
const rulesNextPageBtn = document.getElementById("rules-next-page");

let rulesCache = [];
let upstreamsCache = [];
let editingRuleID = 0;
let editingUpstreamID = 0;
let rulesPage = 1;
let rulesTotal = 0;
let rulesKeyword = "";

const RULES_PAGE_SIZE = 20;

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

let versionLoadPromise = null;
async function loadVersion(force = false) {
  if (versionLoadPromise && !force) return versionLoadPromise;
  const el = document.getElementById("app-version");
  if (!el) return null;
  if (versionLoadPromise && force) {
    return versionLoadPromise;
  }
  versionLoadPromise = (async () => {
    try {
      const data = await api("/api/version", { cache: "no-store" });
      if (data && data.version) {
        el.textContent = data.version;
      }
    } catch (_) {
      return;
    } finally {
      versionLoadPromise = null;
    }
  })();
  return versionLoadPromise;
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
  return d.toLocaleTimeString("zh-CN", { hour12: false });
}

function fmtTimeShort(ts) {
  const d = new Date(ts);
  if (Number.isNaN(d.getTime())) return "-";
  return d.toLocaleTimeString("zh-CN", { hour12: false, hour: "2-digit", minute: "2-digit" });
}

function fmtDateTime(ts) {
  const d = new Date(ts);
  if (Number.isNaN(d.getTime())) return "-";
  return d.toLocaleString("zh-CN", { hour12: false });
}

function appendActivity(ev) {
  if (!activityBody) return;
  const host = (ev.host || "").trim();
  const tr = document.createElement("tr");

  // Status color class
  let statusClass = "";
  if (ev.status >= 200 && ev.status < 300) statusClass = "st-2xx";
  else if (ev.status >= 300 && ev.status < 400) statusClass = "st-3xx";
  else if (ev.status >= 400 && ev.status < 500) statusClass = "st-4xx";
  else if (ev.status >= 500) statusClass = "st-5xx";

  // Action color class
  const actionClass = `act-${ev.action || ""}`;

  tr.innerHTML = `
    <td>${fmtTime(ev.time)}</td>
    <td>${ev.client || "-"}</td>
    <td>${ev.method || "-"}</td>
    <td>${host || "-"}</td>
    <td class="${actionClass}">${ev.action || "-"}</td>
    <td class="${statusClass}">${ev.status ?? "-"}</td>
    <td>${ev.duration_ms ?? "-"}</td>
    <td>
      <button class="secondary" data-act-op="add-rule" data-host="${host}">加入规则</button>
    </td>
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

function renderRouteOptions(selectEl, selectedValue = "DIRECT", opts = {}) {
  if (!selectEl) return;
  const includeBlock = opts.includeBlock !== false;
  selectEl.innerHTML = "";

  const base = [{ value: "DIRECT", text: "直连 (DIRECT)" }];
  if (includeBlock) {
    base.push({ value: "BLOCK", text: "阻断 (BLOCK 404)" });
  }
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

function bindActivityRuleEvents() {
  const ruleForm = document.getElementById("activity-rule-form");
  const patternInput = document.getElementById("activity-rule-pattern");
  const routeTargetInput = document.getElementById("activity-route-target");
  const cancelBtn = document.getElementById("activity-rule-cancel");
  if (!ruleForm || !patternInput || !routeTargetInput || !cancelBtn || !activityBody) return;

  function hideForm() {
    ruleForm.classList.add("hidden");
    patternInput.value = "";
    renderRouteOptions(routeTargetInput, "DIRECT", { includeBlock: false });
  }

  function openFormWithHost(host) {
    patternInput.value = host;
    renderRouteOptions(routeTargetInput, "DIRECT", { includeBlock: false });
    ruleForm.classList.remove("hidden");
  }

  cancelBtn.addEventListener("click", () => {
    hideForm();
    showMsg("已取消添加规则");
  });

  ruleForm.addEventListener("submit", async (e) => {
    e.preventDefault();
    const pattern = String(patternInput.value || "").trim();
    const target = String(routeTargetInput.value || "DIRECT");
    if (!pattern) {
      showMsg("规则模式不能为空", true);
      return;
    }

    const payload = {
      enabled: true,
      pattern,
      ...parseRouteTarget(target),
    };

    try {
      const created = await api("/api/rules", { method: "POST", body: JSON.stringify(payload) });
      const newID = created && created.id ? String(created.id) : "";
      const targetURL = newID ? `/rules?highlight_rule_id=${encodeURIComponent(newID)}` : "/rules";
      window.location.href = targetURL;
    } catch (err) {
      showMsg(`添加规则失败: ${err.message}`, true);
    }
  });

  activityBody.addEventListener("click", (e) => {
    const btn = e.target.closest("button[data-act-op='add-rule']");
    if (!btn) return;
    const host = String(btn.dataset.host || "").trim();
    if (!host) {
      showMsg("该记录目标为空，无法创建规则", true);
      return;
    }
    openFormWithHost(host);
    showMsg(`准备添加规则: ${host}`);
  });

  hideForm();
}

function renderRules() {
  if (!rulesBody) return;
  rulesBody.innerHTML = "";
  const items = Array.isArray(rulesCache) ? rulesCache : [];
  items.forEach((rule) => {
    const tr = document.createElement("tr");
    tr.dataset.ruleId = String(rule.id);
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
  renderRulesPager();
}

function getRuleSearchKeyword() {
  return String(ruleSearchInput?.value || "").trim();
}

function getRulesPageCount(total) {
  return Math.max(1, Math.ceil(total / RULES_PAGE_SIZE));
}

function clampRulesPage(total = rulesTotal) {
  const pageCount = getRulesPageCount(total);
  if (rulesPage > pageCount) rulesPage = pageCount;
  if (rulesPage < 1) rulesPage = 1;
  return pageCount;
}

function renderRulesPager() {
  const pageCount = clampRulesPage();

  if (rulesSummaryEl) {
    rulesSummaryEl.textContent = rulesKeyword ? `关键词“${rulesKeyword}”匹配 ${rulesTotal} 条` : `共 ${rulesTotal} 条`;
  }
  if (rulesPageInfoEl) {
    rulesPageInfoEl.textContent = `第 ${rulesPage} / ${pageCount} 页`;
  }
  if (rulesPrevPageBtn) {
    rulesPrevPageBtn.disabled = rulesPage <= 1;
  }
  if (rulesNextPageBtn) {
    rulesNextPageBtn.disabled = rulesPage >= pageCount;
  }
}

function highlightRuleByID(ruleID) {
  if (!rulesBody || !ruleID) return;
  const row = rulesBody.querySelector(`tr[data-rule-id='${String(ruleID)}']`);
  if (!row) return;
  row.classList.add("flash-row");
  row.scrollIntoView({ behavior: "smooth", block: "center" });
  window.setTimeout(() => {
    row.classList.remove("flash-row");
  }, 2200);
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

let cpuChart, memoryChart, bytesChart, connectionsChart;

async function loadStatsHistory() {
  const history = await api("/api/stats/history");
  if (!history || !Array.isArray(history) || history.length < 2) return;

  const finalLabels = [];
  const finalCpu = [];
  const finalMemTotal = [];
  const finalMemHeap = [];
  const finalBytesIn = [];
  const finalBytesOut = [];
  const finalConn = [];

  for (let i = 1; i < history.length; i++) {
    const s = history[i];
    const prev = history[i - 1];
    const d = new Date(s.time);
    finalLabels.push(fmtTimeShort(s.time));
    finalCpu.push(s.cpu_percent);
    finalMemTotal.push(s.memory_bytes);
    finalMemHeap.push(s.heap_alloc_bytes);
    finalConn.push(s.connections);

    const dt = (d.getTime() - new Date(prev.time).getTime()) / 1000;
    if (dt > 0) {
      finalBytesIn.push(Math.max(0, (s.bytes_in - prev.bytes_in) / dt));
      finalBytesOut.push(Math.max(0, (s.bytes_out - prev.bytes_out) / dt));
    } else {
      finalBytesIn.push(0);
      finalBytesOut.push(0);
    }
  }

  if (cpuChart) {
    cpuChart.data.labels = finalLabels;
    cpuChart.data.datasets[0].data = finalCpu;
    cpuChart.update();
  }
  if (memoryChart) {
    memoryChart.data.labels = finalLabels;
    memoryChart.data.datasets[0].data = finalMemTotal;
    memoryChart.data.datasets[1].data = finalMemHeap;
    memoryChart.update();
  }
  if (bytesChart) {
    bytesChart.data.labels = finalLabels;
    bytesChart.data.datasets[0].data = finalBytesIn;
    bytesChart.data.datasets[1].data = finalBytesOut;
    bytesChart.update();
  }
  if (connectionsChart) {
    connectionsChart.data.labels = finalLabels;
    connectionsChart.data.datasets[0].data = finalConn;
    connectionsChart.update();
  }
}

function initCharts() {
  if (typeof Chart === "undefined") return;
  const commonOptions = {
    responsive: true,
    maintainAspectRatio: false,
    elements: {
      point: { radius: 0 },
      line: { tension: 0.3, fill: false }
    },
    interaction: {
      mode: 'index',
      intersect: false,
    },
  };

  const ctxCpu = document.getElementById('chart-cpu');
  if (ctxCpu) {
    cpuChart = new Chart(ctxCpu, {
      type: 'line',
      data: {
        labels: [],
        datasets: [{
          label: 'CPU 使用率 (%)',
          data: [],
          borderColor: '#1f6f66',
          backgroundColor: '#1f6f66',
        }]
      },
      options: {
        ...commonOptions,
        scales: {
          y: { min: 0, max: 100 }
        }
      }
    });
  }

  const ctxMem = document.getElementById('chart-memory');
  if (ctxMem) {
    memoryChart = new Chart(ctxMem, {
      type: 'line',
      data: {
        labels: [],
        datasets: [
          {
            label: '内存总量',
            data: [],
            borderColor: '#1f6f66',
            backgroundColor: '#1f6f66',
          },
          {
            label: '堆分配',
            data: [],
            borderColor: '#9c2f1f',
            backgroundColor: '#9c2f1f',
          }
        ]
      },
      options: {
        ...commonOptions,
        plugins: {
          tooltip: {
            callbacks: {
              label: function(context) {
                let label = context.dataset.label || '';
                if (label) label += ': ';
                if (context.parsed.y !== null) label += fmtBytes(context.parsed.y);
                return label;
              }
            }
          }
        },
        scales: {
          y: {
            ticks: {
              callback: function(value) {
                return fmtBytes(value);
              }
            }
          }
        }
      }
    });
  }

  const ctxBytes = document.getElementById('chart-bytes');
  if (ctxBytes) {
    bytesChart = new Chart(ctxBytes, {
      type: 'line',
      data: {
        labels: [],
        datasets: [
          {
            label: '接收',
            data: [],
            borderColor: '#2e7d32',
            backgroundColor: '#2e7d32',
          },
          {
            label: '发送',
            data: [],
            borderColor: '#1565c0',
            backgroundColor: '#1565c0',
          }
        ]
      },
      options: {
        ...commonOptions,
        plugins: {
          tooltip: {
            callbacks: {
              label: function(context) {
                let label = context.dataset.label || '';
                if (label) label += ': ';
                if (context.parsed.y !== null) label += fmtBytes(context.parsed.y) + '/s';
                return label;
              }
            }
          }
        },
        scales: {
          y: {
            ticks: {
              callback: function(value) {
                return fmtBytes(value) + '/s';
              }
            }
          }
        }
      }
    });
  }

  const ctxConn = document.getElementById('chart-connections');
  if (ctxConn) {
    connectionsChart = new Chart(ctxConn, {
      type: 'line',
      data: {
        labels: [],
        datasets: [{
          label: '并发连接数',
          data: [],
          borderColor: '#1f6f66',
          backgroundColor: '#1f6f66',
        }]
      },
      options: {
        ...commonOptions,
        scales: {
          y: { beginAtZero: true }
        }
      }
    });
  }
}

async function loadActivities() {
  if (!activityBody) return;
  const events = await api("/api/activities?limit=50");
  activityBody.innerHTML = "";
  events.reverse().forEach(appendActivity);
}

async function loadRules() {
  clampRulesPage();
  const params = new URLSearchParams();
  params.set("page", String(rulesPage));
  params.set("page_size", String(RULES_PAGE_SIZE));
  const keyword = getRuleSearchKeyword();
  if (keyword) {
    params.set("keyword", keyword);
  }
  const data = await api(`/api/rules?${params.toString()}`);
  rulesCache = Array.isArray(data?.items) ? data.items : [];
  rulesTotal = Number(data?.total || 0);
  rulesPage = Number(data?.page || rulesPage);
  rulesKeyword = String(data?.keyword || "");
  clampRulesPage();
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
    showMsg("访问记录流连接中断，正在等待浏览器重连...", true);
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

  if (ruleSearchInput) {
    ruleSearchInput.addEventListener("input", () => {
      rulesPage = 1;
      loadRules().catch((err) => showMsg(`规则加载失败: ${err.message}`, true));
    });
  }

  if (rulesPrevPageBtn) {
    rulesPrevPageBtn.addEventListener("click", () => {
      if (rulesPage <= 1) return;
      rulesPage -= 1;
      loadRules().catch((err) => showMsg(`规则加载失败: ${err.message}`, true));
    });
  }

  if (rulesNextPageBtn) {
    rulesNextPageBtn.addEventListener("click", () => {
      const pageCount = getRulesPageCount(rulesTotal);
      if (rulesPage >= pageCount) return;
      rulesPage += 1;
      loadRules().catch((err) => showMsg(`规则加载失败: ${err.message}`, true));
    });
  }

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

document.addEventListener("visibilitychange", () => {
  if (!document.hidden) {
    void loadVersion(true);
  }
});
window.addEventListener("focus", () => {
  void loadVersion(true);
});
window.addEventListener("pageshow", (ev) => {
  if (ev.persisted) {
    void loadVersion(true);
  }
});

async function init() {
  try {
    await loadVersion(true);
    if (page === "stats") {
      await loadStats();
      setInterval(() => {
        loadStats().catch((err) => showMsg(`统计刷新失败: ${err.message}`, true));
      }, 2000);
      initCharts();
      await loadStatsHistory();
      setInterval(() => {
        loadStatsHistory().catch(() => {});
      }, 10000);
      showMsg("统计页已就绪");
      return;
    }

    if (page === "activities") {
      await loadUpstreams();
      bindActivityRuleEvents();
      await loadActivities();
      startSSE();
      showMsg("访问记录页已就绪");
      return;
    }

    if (page === "rules") {
      await loadUpstreams();
      bindRulesEvents();
      await loadRules();
      const params = new URLSearchParams(window.location.search || "");
      const highlightID = params.get("highlight_rule_id");
      if (highlightID) {
        highlightRuleByID(highlightID);
        params.delete("highlight_rule_id");
        const nextQuery = params.toString();
        const nextURL = nextQuery ? `${window.location.pathname}?${nextQuery}` : window.location.pathname;
        window.history.replaceState(null, "", nextURL);
      }
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
      const abpFile = document.getElementById("abp-file");
      const abpRouteTarget = document.getElementById("abp-route-target");
      const abpEnabled = document.getElementById("abp-enabled");
      const abpImportBtn = document.getElementById("abp-import");

      await loadUpstreams();
      renderRouteOptions(abpRouteTarget, "DIRECT", { includeBlock: false });

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

      if (abpImportBtn && abpFile && abpRouteTarget && abpEnabled) {
        abpImportBtn.addEventListener("click", async () => {
          try {
            const file = abpFile.files && abpFile.files[0];
            if (!file) {
              throw new Error("请先选择 ABP 文件");
            }
            const form = new FormData();
            form.append("file", file);
            form.append("route_target", String(abpRouteTarget.value || "DIRECT"));
            form.append("enabled", abpEnabled.checked ? "true" : "false");

            const resp = await fetch("/api/data/import-abp", {
              method: "POST",
              body: form,
            });
            if (!resp.ok) {
              const body = await resp.text();
              throw new Error(body || `导入失败: ${resp.status}`);
            }
            const result = await resp.json();
            showMsg(`ABP 导入完成: 解析域名 ${result.parsed_domains}，创建规则 ${result.created_rules}，跳过已存在 ${result.skipped_existing}，跳过不支持 ${result.skipped_unsupported}`);
          } catch (err) {
            showMsg(`ABP 导入失败: ${err.message}`, true);
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

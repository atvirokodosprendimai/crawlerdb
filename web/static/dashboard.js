const API = "/api";
const EXCEPTION_PAGE_SIZE = 25;
const SITE_PAGE_SIZE = 50;

let overview = [];
let selectedJobId = null;
let selectedJobCounts = {};
let selectedExceptionsOffset = 0;
let selectedSiteOffset = 0;
let eventSource = null;
let refreshTimer = null;

function statusClass(status) {
    return `status-chip status-${status || "pending"}`;
}

function escapeHTML(value) {
    return String(value ?? "")
        .replaceAll("&", "&amp;")
        .replaceAll("<", "&lt;")
        .replaceAll(">", "&gt;")
        .replaceAll('"', "&quot;")
        .replaceAll("'", "&#39;");
}

function formatDate(value) {
    if (!value) return "—";
    const date = new Date(value);
    if (Number.isNaN(date.getTime())) return "—";
    return date.toLocaleString();
}

function shortId(value) {
    if (!value) return "—";
    return value.length > 10 ? `${value.slice(0, 10)}…` : value;
}

function formatBytes(value) {
    const size = Number(value || 0);
    if (!size) return "—";
    if (size < 1024) return `${size} B`;
    if (size < 1024 * 1024) return `${(size / 1024).toFixed(1)} KB`;
    if (size < 1024 * 1024 * 1024) return `${(size / (1024 * 1024)).toFixed(1)} MB`;
    return `${(size / (1024 * 1024 * 1024)).toFixed(1)} GB`;
}

function scheduleRefresh(delay = 400) {
    window.clearTimeout(refreshTimer);
    refreshTimer = window.setTimeout(async () => {
        try {
            await refreshOverview();
            if (selectedJobId) {
                await loadSelectedJob(selectedJobId, false);
            }
        } catch (_) {}
    }, delay);
}

function setConnectionState(isConnected) {
    document.getElementById("connection-dot").className = `dot ${isConnected ? "connected" : "disconnected"}`;
    document.getElementById("connection-text").textContent = isConnected ? "Live updates connected" : "Disconnected";
}

function connectSSE() {
    if (eventSource) {
        eventSource.close();
    }

    eventSource = new EventSource(`${API}/events`);
    eventSource.onopen = () => setConnectionState(true);
    eventSource.onerror = () => setConnectionState(false);
    eventSource.onmessage = (event) => {
        const payload = JSON.parse(event.data);
        addEvent(payload);
        scheduleRefresh();
    };
}

function addEvent(event) {
    const stream = document.getElementById("event-stream");
    const item = document.createElement("article");
    item.className = "stream-item";
    item.innerHTML = `
        <div class="stream-meta">
            <span>${escapeHTML(event.type || "event")}</span>
            <span>${new Date().toLocaleTimeString()}</span>
        </div>
        <pre class="stream-body">${escapeHTML(JSON.stringify(event.payload, null, 2))}</pre>
    `;
    stream.prepend(item);
    while (stream.children.length > 40) {
        stream.removeChild(stream.lastChild);
    }
}

async function fetchJSON(url, options) {
    const response = await fetch(url, options);
    if (!response.ok) {
        let message = `${response.status} ${response.statusText}`;
        try {
            const body = await response.json();
            if (body && body.error) {
                message = body.error;
            }
        } catch (_) {}
        throw new Error(message);
    }
    return response.json();
}

function sortOverview(items) {
    return [...items].sort((a, b) => {
        const byExceptions = (b.exception_count || 0) - (a.exception_count || 0);
        if (byExceptions !== 0) return byExceptions;

        const statusWeight = {
            running: 0,
            paused: 1,
            failed: 2,
            stopped: 3,
            pending: 4,
            completed: 5
        };
        const aWeight = statusWeight[a.job?.status] ?? 10;
        const bWeight = statusWeight[b.job?.status] ?? 10;
        if (aWeight !== bWeight) return aWeight - bWeight;

        return new Date(b.job?.updated_at || 0) - new Date(a.job?.updated_at || 0);
    });
}

function renderOverview() {
    const list = document.getElementById("job-list");
    list.innerHTML = "";

    if (!overview.length) {
        list.innerHTML = `<div class="empty">No crawl jobs yet. Start with a seed URL.</div>`;
        return;
    }

    for (const item of sortOverview(overview)) {
        const job = item.job;
        const counts = item.counts || {};
        const queue = (counts.pending || 0) + (counts.crawling || 0);
        const incidents = item.exception_count || 0;

        const card = document.createElement("button");
        card.type = "button";
        card.className = `job-item ${selectedJobId === job.id ? "active" : ""}`;
        card.addEventListener("click", () => selectJob(job.id));
        card.innerHTML = `
            <div class="job-top">
                <div>
                    <p class="job-url">${escapeHTML(job.seed_url)}</p>
                    <div class="job-id">${escapeHTML(shortId(job.id))}</div>
                </div>
                <span class="${statusClass(job.status)}">${escapeHTML(job.status)}</span>
            </div>
            <div class="metric-inline">
                <div><strong>${incidents}</strong><span>Incidents</span></div>
                <div><strong>${queue}</strong><span>In flight</span></div>
                <div><strong>${counts.done || 0}</strong><span>Done</span></div>
            </div>
            <div class="job-bottom">
                <span>${formatDate(job.updated_at || job.created_at)}</span>
                <span>${counts.blocked || 0} blocked / ${counts.error || 0} failed</span>
            </div>
        `;
        list.appendChild(card);
    }
}

function renderGlobalStats() {
    const incidents = overview.reduce((sum, item) => sum + (item.exception_count || 0), 0);
    const running = overview.filter((item) => item.job?.status === "running").length;
    const queue = overview.reduce((sum, item) => {
        const counts = item.counts || {};
        return sum + (counts.pending || 0) + (counts.crawling || 0);
    }, 0);

    document.getElementById("global-incidents").textContent = incidents.toLocaleString();
    document.getElementById("global-running").textContent = running.toLocaleString();
    document.getElementById("global-queue").textContent = queue.toLocaleString();

    if (!selectedJobId) {
        document.getElementById("global-selected").textContent = "None";
        document.getElementById("global-selected-note").textContent = "Choose a job to open its exception desk.";
    }
}

async function refreshOverview() {
    overview = await fetchJSON(`${API}/jobs/overview`);
    renderOverview();
    renderGlobalStats();

    if (!selectedJobId && overview.length) {
        const first = sortOverview(overview)[0];
        if (first) {
            await selectJob(first.job.id);
        }
    }
}

function selectedOverviewItem() {
    return overview.find((item) => item.job?.id === selectedJobId) || null;
}

function renderSelectedJob(job, counts) {
    document.getElementById("selected-job-empty").style.display = "none";
    document.getElementById("selected-job-content").style.display = "block";
    document.getElementById("exceptions-empty").style.display = "none";
    document.getElementById("exceptions-wrap").style.display = "block";

    document.getElementById("selected-job-status").innerHTML = `<span class="${statusClass(job.status)}">${escapeHTML(job.status)}</span>`;
    document.getElementById("selected-job-title").textContent = job.seed_url;
    document.getElementById("selected-job-id").textContent = `Job ${job.id}`;

    document.getElementById("global-selected").textContent = shortId(job.id);
    document.getElementById("global-selected-note").textContent = `${(counts.error || 0) + (counts.blocked || 0)} open incidents`;

    const signals = [
        {
            value: ((counts.error || 0) + (counts.blocked || 0)).toLocaleString(),
            label: "Open incidents",
            note: `${counts.blocked || 0} blocked / ${counts.error || 0} failed`,
            hot: true
        },
        {
            value: ((counts.pending || 0) + (counts.crawling || 0)).toLocaleString(),
            label: "Queue load",
            note: `${counts.pending || 0} pending, ${counts.crawling || 0} crawling`
        },
        {
            value: (counts.done || 0).toLocaleString(),
            label: "Completed",
            note: "URLs that finished successfully"
        },
        {
            value: job.status,
            label: "Job state",
            note: formatDate(job.updated_at || job.created_at)
        }
    ];

    document.getElementById("selected-signal-grid").innerHTML = signals.map((signal) => `
        <article class="signal-card ${signal.hot ? "hot" : ""}">
            <p class="signal-label">${escapeHTML(signal.label)}</p>
            <p class="signal-value">${escapeHTML(signal.value)}</p>
            <p class="signal-note">${escapeHTML(signal.note)}</p>
        </article>
    `).join("");

    const controls = [];
    if (job.status === "running") {
        controls.push(buttonHTML("Pause", "btn btn-warning", () => jobAction(job.id, "pause")));
        controls.push(buttonHTML("Stop", "btn btn-danger", () => jobAction(job.id, "stop")));
    }
    if (job.status === "paused") {
        controls.push(buttonHTML("Resume", "btn btn-primary", () => jobAction(job.id, "resume")));
        controls.push(buttonHTML("Stop", "btn btn-danger", () => jobAction(job.id, "stop")));
    }
    if (job.status === "failed" || job.status === "stopped") {
        controls.push(buttonHTML("Retry", "btn btn-primary", () => jobAction(job.id, "retry")));
    }
    controls.push(buttonHTML("Sitemap", "btn btn-secondary", () => exportSelected("sitemap")));
    const controlsRoot = document.getElementById("selected-job-controls");
    controlsRoot.innerHTML = "";
    controls.forEach((button) => controlsRoot.appendChild(button));

    document.getElementById("status-summary").innerHTML = `
        <div class="mini"><strong>${(counts.pending || 0).toLocaleString()}</strong><span>Pending</span></div>
        <div class="mini"><strong>${(counts.crawling || 0).toLocaleString()}</strong><span>Crawling</span></div>
        <div class="mini"><strong>${(counts.done || 0).toLocaleString()}</strong><span>Done</span></div>
        <div class="mini"><strong>${((counts.error || 0) + (counts.blocked || 0)).toLocaleString()}</strong><span>Incidents</span></div>
    `;
}

function buttonHTML(label, className, handler) {
    const button = document.createElement("button");
    button.type = "button";
    button.className = className;
    button.textContent = label;
    button.addEventListener("click", handler);
    return button;
}

async function selectJob(jobId) {
    selectedJobId = jobId;
    selectedExceptionsOffset = 0;
    selectedSiteOffset = 0;
    renderOverview();
    await loadSelectedJob(jobId, true);
}

async function loadSelectedJob(jobId, reloadExceptions) {
    const item = selectedOverviewItem();
    const job = item?.job || await fetchJSON(`${API}/jobs/${jobId}`);
    const counts = item?.counts || await fetchJSON(`${API}/jobs/${jobId}/urls`);
    selectedJobCounts = counts || {};
    renderSelectedJob(job, selectedJobCounts);
    if (reloadExceptions) {
        await loadExceptions();
    }
    await loadSiteExplorer();
    renderGlobalStats();
}

async function loadExceptions() {
    if (!selectedJobId) return;

    const data = await fetchJSON(`${API}/jobs/${selectedJobId}/exceptions?limit=${EXCEPTION_PAGE_SIZE}&offset=${selectedExceptionsOffset}`);
    const items = data.items || [];
    const body = document.getElementById("exceptions-body");
    body.innerHTML = "";

    if (!items.length) {
        document.getElementById("exceptions-empty").style.display = "block";
        document.getElementById("exceptions-empty").textContent = "No blocked or failed URLs in this job.";
        document.getElementById("exceptions-wrap").style.display = "none";
        return;
    }

    document.getElementById("exceptions-empty").style.display = "none";
    document.getElementById("exceptions-wrap").style.display = "block";

    for (const item of items) {
        const row = document.createElement("tr");
        row.innerHTML = `
            <td><span class="${statusClass(item.status)}">${escapeHTML(item.status)}</span></td>
            <td class="url-cell">
                <a class="url-line" href="${escapeHTML(item.normalized)}" target="_blank" rel="noreferrer">${escapeHTML(item.normalized)}</a>
                <span class="context-line">depth ${escapeHTML(item.depth)} · found on ${escapeHTML(item.found_on || "seed")}</span>
            </td>
            <td><div class="reason">${escapeHTML(item.last_error || "No error message recorded")}</div></td>
            <td>${formatDate(item.updated_at)}</td>
        `;
        body.appendChild(row);
    }

    const totalIncidents = (selectedJobCounts.error || 0) + (selectedJobCounts.blocked || 0);
    const page = Math.floor(selectedExceptionsOffset / EXCEPTION_PAGE_SIZE) + 1;
    document.getElementById("exceptions-page-note").textContent = `Page ${page} · showing ${items.length} of ${totalIncidents.toLocaleString()} incidents`;
    document.getElementById("exceptions-prev").disabled = selectedExceptionsOffset === 0;
    document.getElementById("exceptions-next").disabled = selectedExceptionsOffset + EXCEPTION_PAGE_SIZE >= totalIncidents;
}

async function reloadExceptions(resetOffset) {
    if (resetOffset) {
        selectedExceptionsOffset = 0;
    }
    await loadExceptions();
}

async function pageExceptions(direction) {
    const nextOffset = selectedExceptionsOffset + (direction * EXCEPTION_PAGE_SIZE);
    if (nextOffset < 0) return;
    selectedExceptionsOffset = nextOffset;
    await loadExceptions();
}

function currentSiteFilters() {
    return {
        q: document.getElementById("site-query").value.trim(),
        status: document.getElementById("site-status").value,
        content: document.getElementById("site-content").value,
        depth: document.getElementById("site-depth").value
    };
}

function resetSiteFilters() {
    document.getElementById("site-query").value = "";
    document.getElementById("site-status").value = "all";
    document.getElementById("site-content").value = "all";
    document.getElementById("site-depth").value = "all";
}

async function loadSiteExplorer(resetOffset = false) {
    const empty = document.getElementById("site-empty");
    const wrap = document.getElementById("site-wrap");

    if (!selectedJobId) {
        empty.style.display = "block";
        empty.textContent = "Select a job to browse its URLs and stored content.";
        wrap.style.display = "none";
        return;
    }

    if (resetOffset) {
        selectedSiteOffset = 0;
    }

    const filters = currentSiteFilters();
    const params = new URLSearchParams({
        limit: String(SITE_PAGE_SIZE),
        offset: String(selectedSiteOffset),
        status: filters.status,
        content: filters.content,
        depth: filters.depth,
        q: filters.q
    });

    const data = await fetchJSON(`${API}/jobs/${selectedJobId}/site?${params.toString()}`);
    const items = data.items || [];
    const total = Number(data.total || 0);
    const body = document.getElementById("site-body");
    body.innerHTML = "";

    if (!items.length) {
        empty.style.display = "block";
        empty.textContent = total === 0 ? "No URLs match the current explorer filters." : "No URLs on this page.";
        wrap.style.display = "none";
        return;
    }

    empty.style.display = "none";
    wrap.style.display = "block";

    for (const item of items) {
        const pageBits = [];
        if (item.title) {
            pageBits.push(`<strong class="page-title">${escapeHTML(item.title)}</strong>`);
        }
        if (item.http_status) {
            pageBits.push(`<span class="context-line">HTTP ${escapeHTML(item.http_status)}</span>`);
        }
        if (item.content_type) {
            pageBits.push(`<span class="context-line">${escapeHTML(item.content_type)}</span>`);
        }

        const saved = item.file_url
            ? `<a class="file-link" href="${escapeHTML(item.file_url)}" target="_blank" rel="noreferrer">Open file</a><span class="context-line">${escapeHTML(formatBytes(item.content_size))}</span>`
            : `<span class="muted-inline">—</span>`;

        const row = document.createElement("tr");
        row.innerHTML = `
            <td><span class="${statusClass(item.status)}">${escapeHTML(item.status)}</span></td>
            <td class="url-cell">
                <a class="url-line" href="${escapeHTML(item.normalized)}" target="_blank" rel="noreferrer">${escapeHTML(item.normalized)}</a>
                <span class="context-line">depth ${escapeHTML(item.depth)} · found on ${escapeHTML(item.found_on || "seed")}</span>
            </td>
            <td>${pageBits.join("") || `<span class="muted-inline">Not fetched yet</span>`}</td>
            <td>${escapeHTML(item.depth)}</td>
            <td>${saved}</td>
            <td>${formatDate(item.updated_at || item.fetched_at)}</td>
        `;
        body.appendChild(row);
    }

    const page = Math.floor(selectedSiteOffset / SITE_PAGE_SIZE) + 1;
    document.getElementById("site-page-note").textContent = `Page ${page} · showing ${items.length} of ${total.toLocaleString()} URLs`;
    document.getElementById("site-prev").disabled = selectedSiteOffset === 0;
    document.getElementById("site-next").disabled = selectedSiteOffset + SITE_PAGE_SIZE >= total;
}

async function pageSite(direction) {
    const nextOffset = selectedSiteOffset + (direction * SITE_PAGE_SIZE);
    if (nextOffset < 0) return;
    selectedSiteOffset = nextOffset;
    await loadSiteExplorer();
}

async function jobAction(jobId, action) {
    try {
        const response = await fetchJSON(`${API}/jobs/${jobId}/${action}`, { method: "POST" });
        await refreshOverview();
        if (action === "retry" && response?.job_id) {
            await selectJob(response.job_id);
            return;
        }
        if (selectedJobId === jobId) {
            await selectJob(jobId);
        }
    } catch (error) {
        window.alert(`Action failed: ${error.message}`);
    }
}

function exportSelected(format) {
    if (!selectedJobId) return;
    window.open(`${API}/jobs/${selectedJobId}/export?format=${format}`, "_blank", "noopener");
}

function registerActions() {
    document.getElementById("refresh-overview").addEventListener("click", () => refreshOverview().catch(() => {}));
    document.getElementById("exceptions-newest").addEventListener("click", () => reloadExceptions(true));
    document.getElementById("exceptions-prev").addEventListener("click", () => pageExceptions(-1));
    document.getElementById("exceptions-next").addEventListener("click", () => pageExceptions(1));
    document.getElementById("site-explorer-form").addEventListener("submit", (event) => {
        event.preventDefault();
        loadSiteExplorer(true).catch((error) => window.alert(`Explorer failed: ${error.message}`));
    });
    document.getElementById("site-reset").addEventListener("click", () => {
        resetSiteFilters();
        loadSiteExplorer(true).catch((error) => window.alert(`Explorer failed: ${error.message}`));
    });
    document.getElementById("site-prev").addEventListener("click", () => pageSite(-1));
    document.getElementById("site-next").addEventListener("click", () => pageSite(1));
    document.getElementById("export-csv").addEventListener("click", () => exportSelected("csv"));
    document.getElementById("export-json").addEventListener("click", () => exportSelected("json"));

    document.getElementById("new-job-form").addEventListener("submit", async (event) => {
        event.preventDefault();

        const button = document.getElementById("create-job-button");
        button.disabled = true;
        button.textContent = "Starting…";

        try {
            await fetchJSON(`${API}/jobs`, {
                method: "POST",
                headers: { "Content-Type": "application/json" },
                body: JSON.stringify({
                    seed_url: document.getElementById("seed-url").value,
                    config: {
                        scope: document.getElementById("scope").value,
                        max_depth: parseInt(document.getElementById("max-depth").value, 10),
                        extraction: "standard",
                        user_agent: "CrawlerDB/1.0"
                    }
                })
            });

            document.getElementById("seed-url").value = "";
            await refreshOverview();
        } catch (error) {
            window.alert(`Create job failed: ${error.message}`);
        } finally {
            button.disabled = false;
            button.textContent = "Start new crawl";
        }
    });
}

document.addEventListener("DOMContentLoaded", async () => {
    registerActions();
    connectSSE();
    try {
        await refreshOverview();
    } catch (error) {
        addEvent({ type: "startup.error", payload: { message: error.message } });
    }
    window.setInterval(() => refreshOverview().catch(() => {}), 15000);
});

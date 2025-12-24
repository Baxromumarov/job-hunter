const API_URL = '';

const jobList = document.getElementById('jobList');
const resultBox = document.getElementById('analysisResult');
const sourcesList = document.getElementById('sourcesList');
const jobModal = document.getElementById('jobModal');
const applyModal = document.getElementById('applyModal');
const statsGrid = document.getElementById('statsGrid');
const statsModal = document.getElementById('statsModal');

const state = {
    jobs: [],
    sources: [],
    jobsPage: 0,
    sourcesPage: 0,
    pageSize: 20,
    currentTab: 'jobs',
    pendingApply: null,
    jobsTotal: 0,
    sourcesTotal: 0,
    statusFilter: 'active', // active, applied, rejected, closed, all
    activeTotal: 0,
    stats: null,
    statsHistoryMetric: '',
    statsHistoryLabel: '',
    statsHistoryPage: 0,
    statsHistoryTotal: 0,
    statsHistoryPageSize: 12,
    statsHistoryItems: [],
};

const escapeMap = {
    '&': '&amp;',
    '<': '&lt;',
    '>': '&gt;',
    '"': '&quot;',
    "'": '&#039;',
};

function escapeHTML(value) {
    if (!value) return '';
    return value.replace(/[&<>"']/g, (c) => escapeMap[c]);
}

function formatDate(value) {
    if (!value) return '';
    const date = new Date(value);
    if (Number.isNaN(date.getTime())) return '';
    const day = String(date.getDate()).padStart(2, '0');
    const month = String(date.getMonth() + 1).padStart(2, '0');
    const year = date.getFullYear();
    return `${day}-${month}-${year}`;
}

function formatDateTime(value) {
    if (!value) return '';
    const date = new Date(value);
    if (Number.isNaN(date.getTime())) return '';
    const day = String(date.getDate()).padStart(2, '0');
    const month = String(date.getMonth() + 1).padStart(2, '0');
    const year = date.getFullYear();
    const hours = String(date.getHours()).padStart(2, '0');
    const minutes = String(date.getMinutes()).padStart(2, '0');
    return `${day}-${month}-${year} ${hours}:${minutes}`;
}

function formatStatValue(metric, value) {
    if (metric === 'crawl_avg_seconds') {
        return `${Number(value || 0).toFixed(2)}s`;
    }
    const num = Number(value || 0);
    return Number.isFinite(num) ? Math.round(num).toLocaleString() : '0';
}

function sourceLabel(job) {
    if (job.source_url) {
        try {
            return new URL(job.source_url).hostname.replace(/^www\./, '');
        } catch {
            return job.source_url;
        }
    }
    return job.source_type || 'source';
}

function shortenText(text, limit = 180) {
    if (!text) return '';
    if (text.length <= limit) return text;
    return `${text.slice(0, limit)}…`;
}

function formatDescription(text) {
    if (!text) return 'No description provided yet.';
    return escapeHTML(text).replace(/\n/g, '<br>');
}

function renderJobCard(job) {
    const applied = job.applied || Boolean(job.applied_at);
    const source = sourceLabel(job);
    const posted = formatDate(job.posted_at || job.created_at);
    const description = shortenText(job.description || 'No description provided yet.');

    const statusPill = job.closed
        ? '<span class="pill error" style="margin-left:8px;">Closed</span>'
        : job.rejected
        ? '<span class="pill error" style="margin-left:8px;">Not a match</span>'
        : job.applied
        ? '<span class="pill success" style="margin-left:8px;">Applied</span>'
        : '';

    return `
        <div class="job-card card glow ${applied ? 'applied' : ''}" onclick="showJobDetail(${job.id})">
            <div class="job-header">
                <div>
                    <p class="eyebrow">${escapeHTML(source)} ${job.source_type ? '• ' + escapeHTML(job.source_type) : ''}</p>
                    <h3>${escapeHTML(job.title || 'Untitled role')}</h3>
                    <div class="job-meta">${escapeHTML(job.company || 'Unknown')} • ${escapeHTML(job.location || 'Remote')} ${posted ? '• ' + posted : ''} ${statusPill}</div>
                </div>
                <span class="score-badge">${job.match_score || 0}% match</span>
            </div>
            <div class="job-source">Source: <a href="${job.source_url || job.url}" target="_blank" rel="noopener" onclick="event.stopPropagation()">${escapeHTML(source)}</a></div>
            <p class="job-description">${escapeHTML(description)}</p>
            <div class="summary">Tap to view full description and apply.</div>
        </div>
    `;
}

function renderPagination(type, page, totalCount) {
    const isJobs = type === 'jobs';
    const totalPages = Math.max(1, Math.ceil((totalCount || 0) / state.pageSize));
    const prevDisabled = page <= 0 ? 'disabled' : '';
    const nextDisabled = page >= totalPages-1 ? 'disabled' : '';
    const prevHandler = isJobs ? `changePage("jobs", ${page - 1})` : `changePage("sources", ${page - 1})`;
    const nextHandler = isJobs ? `changePage("jobs", ${page + 1})` : `changePage("sources", ${page + 1})`;

    return `
        <div class="pagination">
            <button class="ghost" ${prevDisabled} onclick='${prevHandler}'>Prev</button>
            <span class="muted-text">Page ${page + 1} of ${totalPages}</span>
            <button class="ghost" ${nextDisabled} onclick='${nextHandler}'>Next</button>
        </div>
    `;
}

function renderJobs(jobs) {
    state.jobs = jobs;
    if (!Array.isArray(jobs) || jobs.length === 0) {
        jobList.innerHTML = '<div class="card">No jobs yet. We will pull fresh Go/backend roles automatically.</div>';
        updateActiveCount();
        return;
    }
    const filtered = jobs.filter((j) => {
        switch (state.statusFilter) {
        case 'applied':
            return j.applied;
        case 'rejected':
            return j.rejected;
        case 'closed':
            return j.closed;
        case 'active':
            return !j.rejected && !j.closed;
        default:
            return true;
        }
    });
    jobList.innerHTML = `
        ${filtered.map(renderJobCard).join('') || '<div class="card">No jobs matching this filter.</div>'}
        ${renderPagination('jobs', state.jobsPage, state.jobsTotal)}
    `;
    updateActiveCount();
}

function renderSources(items) {
    state.sources = items || [];
    if (!sourcesList) return;
    if (!state.sources.length) {
        sourcesList.innerHTML = '<div class="card">No sources yet. Add a careers page or job board to start checking.</div>';
        return;
    }

    const content = state.sources.map((src) => `
        <div class="source-row glow">
            <div class="info">
                <strong>${escapeHTML(src.url)}</strong>
                <div class="hint">
                    <span class="chip">${escapeHTML(src.type || src.source_type || 'unknown')}</span>
                    ${src.confidence ? `<span style="margin-left:6px;">${Math.round(src.confidence * 100)}% confidence</span>` : '<span style="margin-left:6px;">pending</span>'}
                    ${src.reason ? `<span style="margin-left:6px;">${escapeHTML(src.reason)}</span>` : ''}
                </div>
            </div>
            <span class="status ${src.status || 'accepted'}">${escapeHTML(src.status || 'accepted')}</span>
        </div>
    `).join('');

    sourcesList.innerHTML = content + renderPagination('sources', state.sourcesPage, state.sourcesTotal);
}

function renderStats(stats) {
    if (!statsGrid) return;
    if (!stats) {
        statsGrid.innerHTML = '<div class="card">Stats not available.</div>';
        return;
    }

    const cards = [
        {
            metric: 'pages_crawled',
            label: 'Pages Scanned',
            value: stats.pages_crawled ?? 0,
            sub: 'HTML pages parsed',
        },
        {
            metric: 'sources_total',
            label: 'Sources Available',
            value: stats.sources_total ?? 0,
            sub: 'Approved sources',
        },
        {
            metric: 'active_jobs',
            label: 'Jobs Available',
            value: stats.active_jobs ?? 0,
            sub: 'Active jobs right now',
        },
        {
            metric: 'jobs_total',
            label: 'Jobs Total',
            value: stats.jobs_total ?? 0,
            sub: 'All stored jobs',
        },
        {
            metric: 'ai_calls',
            label: 'AI Calls',
            value: stats.ai_calls ?? 0,
            sub: 'Classifier + matcher',
        },
        {
            metric: 'errors_total',
            label: 'Errors',
            value: stats.errors_total ?? 0,
            sub: 'Network/parse/store',
        },
    ];

    statsGrid.innerHTML = cards.map((card) => `
        <div class="card glow stat-card stat-button" onclick="openStatsHistory('${card.metric}', '${card.label}')">
            <span class="stat-label">${card.label}</span>
            <span class="stat-value">${card.value}</span>
            <span class="stat-sub">${card.sub}</span>
            <span class="stat-action">View history</span>
        </div>
    `).join('');
}

async function fetchJobs(page = 0) {
    state.jobsPage = page;
    if (!jobList) return;
    jobList.innerHTML = '<div class="loading">Loading jobs...</div>';
    try {
        const response = await fetch(`${API_URL}/jobs?limit=${state.pageSize}&offset=${page * state.pageSize}`);
        const payload = await response.json();
        const items = payload.items || payload || [];
        state.jobsTotal = payload.total || items.length;
        state.activeTotal = payload.active_total || state.activeTotal;
        renderJobs(items);
    } catch (error) {
        jobList.innerHTML = `<div class="card" style="color: red">Error loading jobs: ${error.message}</div>`;
    }
}

async function fetchSources(page = 0) {
    state.sourcesPage = page;
    if (!sourcesList) return;
    try {
        const response = await fetch(`${API_URL}/sources?limit=${state.pageSize}&offset=${page * state.pageSize}`);
        const payload = await response.json();
        const items = payload.items || payload || [];
        state.sourcesTotal = payload.total || items.length;
        const normalized = items.map((src) => ({
            ...src,
            reason: src.reason || src.classification_reason || '',
            status: src.status || 'accepted',
            type: src.type || src.source_type,
        }));
        renderSources(normalized);
    } catch (error) {
        console.error('Failed to load sources', error);
    }
}

async function fetchStats() {
    if (!statsGrid) return;
    statsGrid.innerHTML = '<div class="card">Loading stats...</div>';
    try {
        const response = await fetch(`${API_URL}/stats`);
        const payload = await response.json();
        state.stats = payload;
        renderStats(payload);
    } catch (error) {
        statsGrid.innerHTML = `<div class="card" style="color: red">Error loading stats: ${error.message}</div>`;
    }
}

async function fetchStatsHistory(page = 0) {
    if (!state.statsHistoryMetric || !statsModal) return;
    state.statsHistoryPage = page;
    statsModal.innerHTML = `
        <div class="modal-content stats-modal-content" onclick="event.stopPropagation()">
            <div class="modal-header">
                <div>
                    <div class="tag">Stats History</div>
                    <h2 class="modal-title">${state.statsHistoryLabel}</h2>
                    <div class="modal-meta">Loading snapshots...</div>
                </div>
                <button class="modal-close" onclick="closeStatsModal(event)">Close ✕</button>
            </div>
            <div class="loading">Loading history...</div>
        </div>
    `;
    statsModal.classList.remove('hidden');

    try {
        const limit = state.statsHistoryPageSize;
        const offset = page * limit;
        const metric = encodeURIComponent(state.statsHistoryMetric);
        const response = await fetch(`${API_URL}/stats/history?metric=${metric}&limit=${limit}&offset=${offset}`);
        const payload = await response.json();
        state.statsHistoryItems = payload.items || [];
        state.statsHistoryTotal = payload.total || 0;
        renderStatsHistory();
    } catch (error) {
        statsModal.innerHTML = `
            <div class="modal-content stats-modal-content" onclick="event.stopPropagation()">
                <div class="modal-header">
                    <div>
                        <div class="tag">Stats History</div>
                        <h2 class="modal-title">${state.statsHistoryLabel}</h2>
                        <div class="modal-meta">Error loading history</div>
                    </div>
                    <button class="modal-close" onclick="closeStatsModal(event)">Close ✕</button>
                </div>
                <div class="card" style="color: #fca5a5;">${error.message}</div>
            </div>
        `;
    }
}

function renderStatsHistory() {
    if (!statsModal) return;
    const items = state.statsHistoryItems || [];
    const total = state.statsHistoryTotal || 0;
    const metric = state.statsHistoryMetric;
    const label = state.statsHistoryLabel;
    const showing = items.length;

    const rows = items.map((item) => `
        <div class="stats-row">
            <div class="stats-time">${formatDateTime(item.created_at)}</div>
            <div class="stats-value">${formatStatValue(metric, item.value)}</div>
        </div>
    `).join('');

    statsModal.innerHTML = `
        <div class="modal-content stats-modal-content" onclick="event.stopPropagation()">
            <div class="modal-header">
                <div>
                    <div class="tag">Stats History</div>
                    <h2 class="modal-title">${label}</h2>
                    <div class="modal-meta">Showing ${showing} of ${total} snapshots</div>
                </div>
                <button class="modal-close" onclick="closeStatsModal(event)">Close ✕</button>
            </div>
            <div class="stats-history">
                <div class="stats-row stats-row-header">
                    <div class="stats-time">Timestamp</div>
                    <div class="stats-value">Value</div>
                </div>
                ${rows || '<div class="card">No history yet. Open Stats again to capture snapshots.</div>'}
            </div>
            ${renderStatsPagination(state.statsHistoryPage, total)}
        </div>
    `;
}

function renderStatsPagination(page, totalCount) {
    const totalPages = Math.max(1, Math.ceil((totalCount || 0) / state.statsHistoryPageSize));
    const prevDisabled = page <= 0 ? 'disabled' : '';
    const nextDisabled = page >= totalPages - 1 ? 'disabled' : '';
    return `
        <div class="pagination stats-pagination">
            <button class="ghost" ${prevDisabled} onclick="changeStatsHistoryPage(${page - 1})">Prev</button>
            <span class="muted-text">Page ${page + 1} of ${totalPages}</span>
            <button class="ghost" ${nextDisabled} onclick="changeStatsHistoryPage(${page + 1})">Next</button>
        </div>
    `;
}

function openStatsHistory(metric, label) {
    state.statsHistoryMetric = metric;
    state.statsHistoryLabel = label;
    fetchStatsHistory(0);
}

function changeStatsHistoryPage(page) {
    if (page < 0) return;
    const totalPages = Math.max(1, Math.ceil((state.statsHistoryTotal || 0) / state.statsHistoryPageSize));
    if (page >= totalPages) return;
    fetchStatsHistory(page);
}

function closeStatsModal(event) {
    if (event) event.stopPropagation();
    if (!statsModal) return;
    statsModal.classList.add('hidden');
    statsModal.innerHTML = '';
}

function upsertSourceEntry(entry) {
    const existingIdx = state.sources.findIndex((s) => s.url === entry.url);
    if (existingIdx >= 0) {
        state.sources[existingIdx] = { ...state.sources[existingIdx], ...entry };
    } else {
        state.sources.unshift(entry);
    }
    renderSources(state.sources);
}

async function addSource() {
    const urlInput = document.getElementById('sourceUrl');
    const typeSelect = document.getElementById('sourceType');
    const url = urlInput.value.trim();
    const sourceType = typeSelect.value;

    if (!url) return;

    // reset to first page to show newly added entry
    state.sourcesPage = 0;
    const checkingEntry = { url, source_type: sourceType, status: 'checking' };
    upsertSourceEntry(checkingEntry);

    resultBox.classList.remove('hidden');
    resultBox.innerHTML = '<div class="card">Checking source with AI...</div>';

    try {
        const response = await fetch(`${API_URL}/sources`, {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ url, source_type: sourceType }),
        });

        const data = await response.json();
        if (!response.ok) {
            throw new Error(data.error || 'Classification failed');
        }

        const isAccepted = data.is_job_site && data.tech_related && data.confidence >= 0.7;
        upsertSourceEntry({
            url,
            source_type: sourceType,
            type: sourceType,
            status: isAccepted ? 'accepted' : 'rejected',
            confidence: data.confidence,
            reason: data.reason || '',
        });

        resultBox.innerHTML = `
            <div class="alert ${isAccepted ? 'success' : 'error'}">
                <div style="margin-bottom: 6px; font-weight: 700;">Result</div>
                <div class="pill ${isAccepted ? 'success' : 'error'}">
                    <span>Status: ${isAccepted ? 'Accepted' : 'Rejected'}</span>
                </div>
                <div style="margin-top: 8px;">Tech Related: ${data.tech_related ? '✅' : '❌'}</div>
                <div>Confidence: ${(data.confidence * 100).toFixed(1)}%</div>
                <div style="margin-top: 4px; color: #cbd5e1;">Reason: ${escapeHTML(data.reason || '')}</div>
            </div>
        `;
        urlInput.value = '';
        if (isAccepted) {
            fetchSources(0);
        }
    } catch (error) {
        upsertSourceEntry({ url, source_type: sourceType, status: 'rejected', reason: error.message });
        resultBox.innerHTML = `<div class="card" style="color: red">Error: ${error.message}</div>`;
    }
}

function showJobDetail(id) {
    const job = state.jobs.find((j) => j.id === id);
    if (!job || !jobModal) return;

    const applied = job.applied || Boolean(job.applied_at);
    const source = sourceLabel(job);
    const posted = formatDate(job.posted_at || job.created_at);
    const summary = job.match_summary || 'Auto-selected for backend Go focus.';
    const descriptionHtml = formatDescription(job.description || 'No description provided yet.');

    jobModal.innerHTML = `
        <div class="modal-content" onclick="event.stopPropagation()">
            <div class="modal-header">
                <div>
                    <div class="tag">${escapeHTML(source)} ${job.source_type ? '• ' + escapeHTML(job.source_type) : ''}</div>
                    <h2 class="modal-title">${escapeHTML(job.title || 'Untitled role')}</h2>
                    <div class="modal-meta">${escapeHTML(job.company || 'Unknown company')} • ${escapeHTML(job.location || 'Remote')} • ${posted}</div>
                </div>
                <button class="modal-close" onclick="closeJobModal(event)">Close ✕</button>
            </div>
            <div class="job-source" style="margin-top: 8px;">Source: <a href="${job.source_url || job.url}" target="_blank" rel="noopener">${escapeHTML(source)}</a></div>
            <div class="modal-body">${descriptionHtml}</div>
            <div class="summary" style="margin-top: 12px;">Summary: ${escapeHTML(summary)}</div>
            <div class="modal-actions">
                <button ${applied ? 'disabled' : ''} onclick='applyToJob(${job.id}, ${JSON.stringify(job.url)}, event)'>${applied ? 'Applied' : 'Apply'}</button>
            </div>
        </div>
    `;
    jobModal.classList.remove('hidden');
}

function closeJobModal(event) {
    if (event) event.stopPropagation();
    if (!jobModal) return;
    jobModal.classList.add('hidden');
    jobModal.innerHTML = '';
}

function openLink(url) {
    window.open(url, '_blank', 'noopener');
}

async function applyToJob(id, url, event) {
    if (event) event.stopPropagation();
    openLink(url);
    state.pendingApply = { id, url };
    showApplyPrompt();
}

function changePage(type, page) {
    if (page < 0) return;
    if (type === 'jobs') {
        const totalPages = Math.max(1, Math.ceil((state.jobsTotal || 0) / state.pageSize));
        if (page >= totalPages) return;
        fetchJobs(page);
    } else {
        const totalPages = Math.max(1, Math.ceil((state.sourcesTotal || 0) / state.pageSize));
        if (page >= totalPages) return;
        fetchSources(page);
    }
}

function setTab(tab) {
    state.currentTab = tab;
    const jobsSection = document.querySelector('.jobs-section');
    const sourcesSection = document.querySelector('.sources-section');
    const statsSection = document.querySelector('.stats-section');
    const tabJobs = document.getElementById('tabJobs');
    const tabSources = document.getElementById('tabSources');
    const tabStats = document.getElementById('tabStats');

    if (tab === 'jobs') {
        jobsSection.classList.remove('hidden');
        sourcesSection.classList.add('hidden');
        statsSection.classList.add('hidden');
        tabJobs.classList.add('active');
        tabSources.classList.remove('active');
        tabStats.classList.remove('active');
        fetchJobs(state.jobsPage);
    } else if (tab === 'sources') {
        sourcesSection.classList.remove('hidden');
        jobsSection.classList.add('hidden');
        statsSection.classList.add('hidden');
        tabSources.classList.add('active');
        tabJobs.classList.remove('active');
        tabStats.classList.remove('active');
        fetchSources(state.sourcesPage);
    } else {
        statsSection.classList.remove('hidden');
        jobsSection.classList.add('hidden');
        sourcesSection.classList.add('hidden');
        tabStats.classList.add('active');
        tabJobs.classList.remove('active');
        tabSources.classList.remove('active');
        fetchStats();
    }
}

function setStatusFilter(filter) {
    state.statusFilter = filter;
    const ids = ['filterActive', 'filterApplied', 'filterRejected', 'filterClosed', 'filterAll'];
    ids.forEach((id) => {
        const el = document.getElementById(id);
        if (!el) return;
        const match = id.toLowerCase().includes(filter);
        el.classList.toggle('active', match);
    });
    renderJobs(state.jobs);
}

function updateActiveCount() {
    const el = document.getElementById('activeCount');
    if (!el) return;
    el.textContent = state.activeTotal || state.jobs.filter((j) => !j.rejected && !j.closed).length || 0;
}

function showApplyPrompt() {
    if (!applyModal || !state.pendingApply) return;
    applyModal.innerHTML = `
        <div class="confirm-content" onclick="event.stopPropagation()">
            <h3 style="margin: 0 0 8px 0;">Mark as applied?</h3>
            <p class="muted-text" style="margin: 0 0 14px 0;">We opened the listing in a new tab. Mark this job as applied to gray it out.</p>
            <div class="modal-actions" style="justify-content: flex-end; gap: 10px;">
                <button class="ghost" onclick="closeApplyPrompt(event)">Cancel</button>
                <button class="ghost" onclick="rejectJob(event)">Not a match</button>
                <button class="ghost" onclick="closeJob(event)">Closed</button>
                <button onclick="confirmApplied(event)">Yes, applied</button>
            </div>
        </div>
    `;
    applyModal.classList.remove('hidden');
}

function closeApplyPrompt(event) {
    if (event) event.stopPropagation();
    if (!applyModal) return;
    applyModal.classList.add('hidden');
    applyModal.innerHTML = '';
    state.pendingApply = null;
}

async function confirmApplied(event) {
    if (event) event.stopPropagation();
    const pending = state.pendingApply;
    if (!pending) {
        closeApplyPrompt();
        return;
    }

    try {
        const response = await fetch(`${API_URL}/jobs/${pending.id}/apply`, { method: 'POST' });
        if (!response.ok) {
            throw new Error('Failed to mark as applied');
        }
        fetchJobs(state.jobsPage);
        closeJobModal();
    } catch (error) {
        alert(`Could not mark as applied: ${error.message}`);
    } finally {
        closeApplyPrompt();
    }
}

async function rejectJob(event) {
    if (event) event.stopPropagation();
    const pending = state.pendingApply;
    if (!pending) {
        closeApplyPrompt();
        return;
    }
    try {
        const response = await fetch(`${API_URL}/jobs/${pending.id}/reject`, { method: 'POST' });
        if (!response.ok) {
            throw new Error('Failed to mark as not a match');
        }
        fetchJobs(state.jobsPage);
        closeJobModal();
    } catch (error) {
        alert(`Could not mark as not a match: ${error.message}`);
    } finally {
        closeApplyPrompt();
    }
}

async function closeJob(event) {
    if (event) event.stopPropagation();
    const pending = state.pendingApply;
    if (!pending) {
        closeApplyPrompt();
        return;
    }
    try {
        const response = await fetch(`${API_URL}/jobs/${pending.id}/close`, { method: 'POST' });
        if (!response.ok) {
            throw new Error('Failed to mark as closed');
        }
        fetchJobs(state.jobsPage);
        closeJobModal();
    } catch (error) {
        alert(`Could not mark as closed: ${error.message}`);
    } finally {
        closeApplyPrompt();
    }
}

// initial load
setTab('jobs');

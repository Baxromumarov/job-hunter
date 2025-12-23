const API_URL = '';

const jobList = document.getElementById('jobList');
const resultBox = document.getElementById('analysisResult');
const sourcesList = document.getElementById('sourcesList');
const jobModal = document.getElementById('jobModal');
const applyModal = document.getElementById('applyModal');

const state = {
    jobs: [],
    sources: [],
    jobsPage: 0,
    sourcesPage: 0,
    pageSize: 20,
    currentTab: 'jobs',
    pendingApply: null,
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
    if (!value) return 'Recently added';
    const date = new Date(value);
    return Number.isNaN(date.getTime()) ? 'Recently added' : date.toLocaleDateString();
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

    return `
        <div class="job-card card glow ${applied ? 'applied' : ''}" onclick="showJobDetail(${job.id})">
            <div class="job-header">
                <div>
                    <p class="eyebrow">${escapeHTML(source)} ${job.source_type ? '• ' + escapeHTML(job.source_type) : ''}</p>
                    <h3>${escapeHTML(job.title || 'Untitled role')}</h3>
                    <div class="job-meta">${escapeHTML(job.company || 'Unknown company')} • ${escapeHTML(job.location || 'Remote')} • ${posted}</div>
                </div>
                <span class="score-badge">${job.match_score || 0}% match</span>
            </div>
            <div class="job-source">Source: <a href="${job.source_url || job.url}" target="_blank" rel="noopener" onclick="event.stopPropagation()">${escapeHTML(source)}</a></div>
            <p class="job-description">${escapeHTML(description)}</p>
            <div class="summary">Tap to view full description and apply.</div>
        </div>
    `;
}

function renderPagination(type, page, length) {
    const isJobs = type === 'jobs';
    const prevDisabled = page <= 0 ? 'disabled' : '';
    const nextDisabled = length < state.pageSize ? 'disabled' : '';
    const prevHandler = isJobs ? `changePage("jobs", ${page - 1})` : `changePage("sources", ${page - 1})`;
    const nextHandler = isJobs ? `changePage("jobs", ${page + 1})` : `changePage("sources", ${page + 1})`;

    return `
        <div class="pagination">
            <button class="ghost" ${prevDisabled} onclick='${prevHandler}'>Prev</button>
            <span class="muted-text">Page ${page + 1}</span>
            <button class="ghost" ${nextDisabled} onclick='${nextHandler}'>Next</button>
        </div>
    `;
}

function renderJobs(jobs) {
    state.jobs = jobs;
    if (!Array.isArray(jobs) || jobs.length === 0) {
        jobList.innerHTML = '<div class="card">No jobs yet. We will pull fresh Go/backend roles automatically.</div>';
        return;
    }
    jobList.innerHTML = `
        ${jobs.map(renderJobCard).join('')}
        ${renderPagination('jobs', state.jobsPage, jobs.length)}
    `;
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
                <span class="hint">${escapeHTML(src.type || src.source_type || 'unknown')} • ${src.confidence ? `${Math.round(src.confidence * 100)}% confidence` : 'pending'}${src.reason ? ' • ' + escapeHTML(src.reason) : ''}</span>
            </div>
            <span class="status ${src.status || 'accepted'}">${escapeHTML(src.status || 'accepted')}</span>
        </div>
    `).join('');

    sourcesList.innerHTML = content + renderPagination('sources', state.sourcesPage, state.sources.length);
}

async function fetchJobs(page = 0) {
    state.jobsPage = page;
    if (!jobList) return;
    jobList.innerHTML = '<div class="loading">Loading jobs...</div>';
    try {
        const response = await fetch(`${API_URL}/jobs?limit=${state.pageSize}&offset=${page * state.pageSize}`);
        const payload = await response.json();
        const items = payload.items || payload || [];
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
            <div class="card" style="background: #f0fdf4; border-color: #bbf7d0;">
                <strong>Result:</strong><br>
                Status: ${isAccepted ? 'Accepted ✅' : 'Rejected ❌'}<br>
                Tech Related: ${data.tech_related ? '✅' : '❌'}<br>
                Confidence: ${(data.confidence * 100).toFixed(1)}%<br>
                Reason: ${escapeHTML(data.reason || '')}
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
        fetchJobs(page);
    } else {
        fetchSources(page);
    }
}

function setTab(tab) {
    state.currentTab = tab;
    const jobsSection = document.querySelector('.jobs-section');
    const sourcesSection = document.querySelector('.sources-section');
    const tabJobs = document.getElementById('tabJobs');
    const tabSources = document.getElementById('tabSources');

    if (tab === 'jobs') {
        jobsSection.classList.remove('hidden');
        sourcesSection.classList.add('hidden');
        tabJobs.classList.add('active');
        tabSources.classList.remove('active');
        fetchJobs(state.jobsPage);
    } else {
        sourcesSection.classList.remove('hidden');
        jobsSection.classList.add('hidden');
        tabSources.classList.add('active');
        tabJobs.classList.remove('active');
        fetchSources(state.sourcesPage);
    }
}

function showApplyPrompt() {
    if (!applyModal || !state.pendingApply) return;
    applyModal.innerHTML = `
        <div class="confirm-content" onclick="event.stopPropagation()">
            <h3 style="margin: 0 0 8px 0;">Mark as applied?</h3>
            <p class="muted-text" style="margin: 0 0 14px 0;">We opened the listing in a new tab. Mark this job as applied to gray it out.</p>
            <div class="modal-actions" style="justify-content: flex-end; gap: 10px;">
                <button class="ghost" onclick="closeApplyPrompt(event)">Cancel</button>
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

// initial load
setTab('jobs');

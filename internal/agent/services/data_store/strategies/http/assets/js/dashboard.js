// SenHub Agent console: Overview page.
//
// Reads the agent's APIs and renders the cards: getting started, probes,
// outputs, agent, licence, recent events. Every card renders on its own so
// one failing API does not blank the page; the failures are listed in the
// banner at the top.
(function () {
    const KEY = (window.AGENT_KEY || '').trim();
    const API = '/api/' + KEY + '/';
    const WEB = '/web/' + KEY + '/';
    const GS_FLAG = 'senhub.overview.gettingStartedHidden';
    const $ = (id) => document.getElementById(id);

    function esc(s) {
        return String(s == null ? '' : s).replace(/[&<>"']/g, c => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c]));
    }
    function pill(state, label) {
        const dot = (state === 'ok' || state === 'err' || state === 'warn') ? '<span class="dot ' + state + '"></span>' : '';
        return '<span class="pill ' + state + '">' + dot + esc(label) + '</span>';
    }
    function num(n) { return Number(n || 0).toLocaleString('en-US'); }
    function plural(n, word) { return num(n) + ' ' + word + (n === 1 ? '' : 's'); }

    // Go duration string ("26h3m12.5s") to "1d 2h" / "3h 4m" / "12m" / "30s".
    function uptime(s) {
        const m = String(s || '').match(/^(?:(\d+)h)?(?:(\d+)m)?(?:([\d.]+)s)?$/);
        if (!m) return String(s || '-');
        const h = parseInt(m[1] || '0', 10), mi = parseInt(m[2] || '0', 10), sec = Math.floor(parseFloat(m[3] || '0'));
        if (h >= 24) return Math.floor(h / 24) + 'd ' + (h % 24) + 'h ' + mi + 'm';
        if (h) return h + 'h ' + mi + 'm';
        if (mi) return mi + 'm ' + sec + 's';
        return sec + 's';
    }
    function parseTime(t) {
        if (!t) return null;
        const d = typeof t === 'number' ? new Date(t * 1000) : new Date(t);
        return isNaN(d.getTime()) ? null : d;
    }
    // "12 s", "3 min", "2 h", "5 d" since t.
    function rel(t) {
        const d = parseTime(t);
        if (!d) return '';
        const s = Math.max(0, Math.round((Date.now() - d.getTime()) / 1000));
        if (s < 60) return s + ' s';
        if (s < 3600) return Math.floor(s / 60) + ' min';
        if (s < 86400) return Math.floor(s / 3600) + ' h';
        return Math.floor(s / 86400) + ' d';
    }
    const MONTHS = ['Jan', 'Feb', 'Mar', 'Apr', 'May', 'Jun', 'Jul', 'Aug', 'Sep', 'Oct', 'Nov', 'Dec'];
    function pad(n) { return (n < 10 ? '0' : '') + n; }
    function clock(d) { return pad(d.getHours()) + ':' + pad(d.getMinutes()) + ':' + pad(d.getSeconds()); }
    // HH:MM:SS when today, "Sep 5, 07:02" otherwise.
    function when(t) {
        const d = parseTime(t);
        if (!d) return '';
        const now = new Date();
        if (d.toDateString() === now.toDateString()) return clock(d);
        return MONTHS[d.getMonth()] + ' ' + d.getDate() + ', ' + pad(d.getHours()) + ':' + pad(d.getMinutes());
    }
    function dateOnly(t) {
        const d = parseTime(t);
        if (!d) return String(t || '-');
        return MONTHS[d.getMonth()] + ' ' + d.getDate() + ', ' + d.getFullYear();
    }
    // Long error texts get cut; the full text stays in the tooltip.
    function short(text, max) {
        const t = String(text == null ? '' : text);
        if (t.length <= max) return esc(t);
        return '<span title="' + esc(t) + '">' + esc(t.slice(0, max).replace(/\s+\S*$/, '')) + '…</span>';
    }
    function shortKey(k) { return k.length > 12 ? k.slice(0, 8) + '…' + k.slice(-4) : k; }

    async function get(path) {
        const r = await fetch(API + path, { headers: { 'Accept': 'application/json' } });
        if (!r.ok) throw new Error('HTTP ' + r.status);
        return r.json();
    }

    function copyText(text) {
        if (navigator.clipboard && navigator.clipboard.writeText) {
            return navigator.clipboard.writeText(text).then(() => true, () => copyFallback(text));
        }
        return Promise.resolve(copyFallback(text));
    }
    function copyFallback(text) {
        const ta = document.createElement('textarea');
        ta.value = text;
        ta.className = 'sr-only';
        ta.setAttribute('readonly', '');
        document.body.appendChild(ta);
        ta.select();
        let ok = false;
        try { ok = document.execCommand('copy'); } catch (e) { ok = false; }
        document.body.removeChild(ta);
        return ok;
    }

    function gsHidden() {
        try { return localStorage.getItem(GS_FLAG) === '1'; } catch (e) { return false; }
    }
    function gsHide() {
        try { localStorage.setItem(GS_FLAG, '1'); } catch (e) { /* storage unavailable: hide for this view only */ }
        $('gs-card').classList.add('hide');
    }

    // ---- Probes -----------------------------------------------------------

    function probeState(p) {
        if (!p.enabled) return 'disabled';
        if (p.health === 'failed') return 'failing';
        if (p.running) return 'running';
        if (p.authorized === false) return 'locked';
        return 'stopped';
    }
    function probeStats(list) {
        const st = { total: list.length, running: 0, failing: 0, disabled: 0, other: 0 };
        list.forEach(p => {
            const s = probeState(p);
            if (s === 'running') st.running++;
            else if (s === 'failing') st.failing++;
            else if (s === 'disabled') st.disabled++;
            else st.other++;
        });
        return st;
    }
    function renderProbes(data, system) {
        const list = (data && data.probes) || [];
        const st = probeStats(list);
        $('probes-count').textContent = st.total;
        let pills = '';
        if (st.running) pills += pill('ok', st.running + ' running');
        if (st.failing) pills += pill('err', st.failing + ' failing');
        if (st.other) pills += pill('warn', st.other + ' stopped');
        if (st.disabled) pills += pill('off', st.disabled + ' disabled');
        $('probes-pills').innerHTML = pills;

        if (!list.length) {
            $('probes-body').innerHTML = '<div class="empty-line">No probe configured yet. <a href="' + WEB + 'probes?new=1">Add a probe</a> to start collecting.</div>';
        } else {
            const order = { failing: 0, stopped: 1, locked: 1, running: 2, disabled: 3 };
            const rows = list.slice().sort((a, b) => order[probeState(a)] - order[probeState(b)]);
            let html = '<table class="tbl compact"><thead><tr><th>Name</th><th>Type</th><th>State</th><th class="right">Metrics</th><th class="right">Last run</th></tr></thead><tbody>';
            rows.forEach(p => {
                const s = probeState(p);
                let state, last;
                if (s === 'failing') {
                    state = pill('err', 'failing');
                    last = '<span class="reason">' + short(p.last_error || p.reason || 'collection failed', 80) + '</span>';
                } else if (s === 'running') {
                    state = pill('ok', 'running');
                    last = p.last_update ? esc(rel(p.last_update)) : '-';
                } else if (s === 'disabled') {
                    state = pill('off', 'disabled');
                    last = '-';
                } else if (s === 'locked') {
                    state = pill('pro', 'licence needed');
                    last = '<span class="reason">' + short(p.reason || 'not authorized', 80) + '</span>';
                } else {
                    state = pill('warn', 'stopped');
                    last = p.reason ? '<span class="reason">' + short(p.reason, 80) + '</span>' : '-';
                }
                const metrics = s === 'disabled' ? '-' : num(p.metrics_count);
                html += '<tr><td class="name">' + esc(p.name) + '</td><td><code>' + esc(p.type) + '</code></td><td>' + state +
                    '</td><td class="num">' + metrics + '</td><td class="num">' + last + '</td></tr>';
            });
            html += '</tbody></table>';
            $('probes-body').innerHTML = html;
        }

        const total = system && system.cache ? system.cache.total_metrics : null;
        let newest = null;
        list.forEach(p => { const d = parseTime(p.last_update); if (d && (!newest || d > newest)) newest = d; });
        let foot = total == null ? '' : num(total) + ' metrics cached';
        if (newest) foot += (foot ? ', ' : '') + 'refreshed ' + rel(newest) + ' ago';
        $('probes-foot').textContent = foot || '-';
        return st;
    }

    // ---- Outputs ----------------------------------------------------------

    function outputPill(o) {
        if (!o.enabled || o.state === 'disabled') return pill('off', 'disabled');
        switch (o.state) {
            case 'listening': return pill('ok', 'listening');
            case 'exporting': return pill('ok', 'exporting');
            case 'failing': return pill('err', 'failing');
            case 'idle': return pill('off', 'idle');
            default: return pill('off', o.state || 'unknown');
        }
    }
    function outputTarget(p) {
        p = p || {};
        return p.endpoint || p.url || p.host || p.server || '';
    }
    function pollers(o) {
        return (o.readers || []).filter(r => r.endpoint !== 'web');
    }
    function httpLine(o) {
        const p = o.params || {};
        const parts = [];
        if (p.port) parts.push('<code>' + esc((p.bind_address || '0.0.0.0') + ':' + p.port) + '</code>');
        const eps = (o.readers || []).filter(r => r.enabled).map(r => r.endpoint);
        if (eps.length) parts.push(esc(eps.join(', ')));
        const seen = pollers(o).filter(r => r.total > 0).sort((a, b) => (parseTime(b.last) || 0) - (parseTime(a.last) || 0));
        if (seen.length) {
            const r = seen[0];
            parts.push('last ' + esc(r.endpoint) + ' read ' + esc(rel(r.last)) + ' ago' + (r.from ? ' from ' + esc(r.from) : ''));
        }
        if (o.state === 'listening' && !seen.length && pollers(o).some(r => r.enabled)) parts.push('<span class="warn">no poller seen yet</span>');
        if (o.state === 'failing' && o.reason) parts.push('<span class="bad">' + short(o.reason, 120) + '</span>');
        return parts.join(' &middot; ');
    }
    function pushLine(o) {
        const p = o.params || {};
        const parts = [];
        const target = outputTarget(p);
        let head = target ? '<code>' + esc(target) + '</code>' : '';
        const extra = [];
        if (p.protocol) extra.push(esc(String(p.protocol).toUpperCase()));
        if (p.tls && p.tls.enabled) extra.push('TLS');
        if (head && extra.length) head += ' ' + extra.join(', ');
        if (head) parts.push((!o.enabled ? 'Push to ' : '') + head);
        if (p.signals && typeof p.signals === 'object') {
            const sig = [];
            Object.keys(p.signals).forEach(k => {
                const v = p.signals[k];
                if (v && v.enabled !== false) sig.push(k + (v.interval ? ' every ' + v.interval : ''));
            });
            if (sig.length) parts.push(esc(sig.join(', ')));
        } else if (p.interval) {
            parts.push('every ' + esc(p.interval) + (typeof p.interval === 'number' ? ' s' : ''));
        }
        const a = o.activity;
        if (o.enabled && a) {
            if (a.last_success) parts.push('last export ' + esc(rel(a.last_success)) + ' ago');
            if (a.failures) parts.push('<span class="' + (o.state === 'failing' ? 'bad' : 'warn') + '">' + num(a.failures) + ' failed export' + (a.failures === 1 ? '' : 's') + '</span>');
            else if (a.last_success) parts.push('0 errors');
        }
        if (o.enabled && o.state === 'failing' && (o.reason || (a && a.last_error))) parts.push('<span class="bad">' + short(o.reason || a.last_error, 120) + '</span>');
        else if (o.enabled && o.state === 'idle' && !(a && a.successes)) parts.push('nothing exported yet');
        return parts.join(' &middot; ');
    }
    function renderOutputs(data) {
        const list = (data && data.outputs) || [];
        $('outputs-count').textContent = list.length;
        if (!list.length) {
            $('outputs-body').innerHTML = '<div class="empty-line">No output configured. <a href="' + WEB + 'outputs">Add an output</a>.</div>';
        } else {
            let html = '';
            list.forEach(o => {
                const off = !o.enabled || o.state === 'disabled';
                const title = o.type === 'http' ? (o.display_name || 'Console and pull endpoints') : o.name;
                let action;
                if (o.type === 'http') action = '<a class="btn sm" href="' + WEB + 'outputs/http#urls">Sensor URLs</a>';
                else if (off) action = '<button type="button" class="btn sm" data-enable="' + esc(o.name) + '">Enable</button>';
                else action = '<a class="btn sm" href="' + WEB + 'outputs/' + encodeURIComponent(o.name) + '">Edit</a>';
                html += '<div class="orow"><div class="oicon' + (off ? ' off' : '') + '">' + esc(String(o.type).toUpperCase().slice(0, 6)) + '</div>' +
                    '<div><div class="t">' + esc(title) + ' ' + outputPill(o) + '</div>' +
                    '<div class="d">' + (o.type === 'http' ? httpLine(o) : pushLine(o)) + '</div></div>' + action + '</div>';
            });
            $('outputs-body').innerHTML = html;
            $('outputs-body').querySelectorAll('button[data-enable]').forEach(b => b.addEventListener('click', () => enableOutput(b.dataset.enable, list, b)));
        }
        const paths = list.filter(o => o.enabled && o.state !== 'disabled' && o.state !== 'failing').length;
        $('outputs-foot').textContent = 'Data leaves this host on ' + plural(paths, 'path');
        return list;
    }
    async function enableOutput(name, list, btn) {
        const o = list.find(x => x.name === name);
        if (!o) return;
        btn.disabled = true;
        try {
            const r = await fetch(API + 'config/outputs/' + encodeURIComponent(name), {
                method: 'PUT', headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ enabled: true, params: o.params || {} })
            });
            const d = await r.json().catch(() => ({}));
            if (!r.ok) throw new Error(d.error || ('HTTP ' + r.status));
            showBanner('ok', 'Output ' + name + ' enabled.');
            refresh();
        } catch (e) {
            showBanner('err', 'Could not enable ' + name + ': ' + e.message);
            btn.disabled = false;
        }
    }

    // ---- Getting started --------------------------------------------------

    function renderGettingStarted(system, probeSt, outputs) {
        const card = $('gs-card');
        if (gsHidden()) { card.classList.add('hide'); return; }
        const steps = [];
        const s1 = { done: true, title: 'Agent is running', desc: 'Service up for ' + esc(uptime(system && system.uptime)) + '. Console on port ' + esc(system && system.port || '?') + '.' };
        steps.push(s1);

        const s2 = { done: !!(probeSt && probeSt.total > 0), title: 'Probes configured' };
        if (s2.done) s2.desc = esc(plural(probeSt.total, 'probe') + ', ' + probeSt.running + ' running, ' + probeSt.failing + ' failing') + '. <a href="' + WEB + 'probes">Review</a>';
        else s2.desc = 'Nothing is collected yet. <a href="' + WEB + 'probes?new=1">Add a probe</a>';
        steps.push(s2);

        const push = (outputs || []).filter(o => o.mode === 'push' && o.enabled && o.state !== 'disabled' && o.state !== 'failing');
        const http = (outputs || []).find(o => o.type === 'http');
        const readAny = http ? pollers(http).some(r => r.total > 0) : false;
        const s3 = { done: push.length > 0 || readAny, title: 'Data is sent somewhere' };
        if (s3.done) {
            const bits = push.map(o => esc(String(o.type).toUpperCase()) + ' push' + (outputTarget(o.params) ? ' to <code>' + esc(outputTarget(o.params)) + '</code>' : ''));
            if (readAny) bits.push('pull endpoints read');
            s3.desc = bits.join(', ') + '. <a href="' + WEB + 'outputs">Outputs</a>';
        } else {
            const failing = (outputs || []).filter(o => o.mode === 'push' && o.enabled && o.state === 'failing');
            s3.desc = (failing.length ? failing.map(o => esc(o.name)).join(', ') + ' is failing. ' : 'No output delivers data yet. ') + '<a href="' + WEB + 'outputs">Outputs</a>';
        }
        steps.push(s3);

        const polls = http ? pollers(http).filter(r => r.total > 0) : [];
        const s4 = { done: polls.length > 0, title: 'Give the poller its sensor URL' };
        if (s4.done) {
            const r = polls.sort((a, b) => (parseTime(b.last) || 0) - (parseTime(a.last) || 0))[0];
            s4.desc = 'Last ' + esc(r.endpoint) + ' poll ' + esc(rel(r.last)) + ' ago' + (r.from ? ' from ' + esc(r.from) : '') + '. <a href="' + WEB + 'outputs/http#urls">Sensor URLs</a>';
        } else {
            s4.desc = 'No PRTG, Nagios or Prometheus poll seen yet. <a href="' + WEB + 'outputs/http#urls">Build a sensor URL</a>';
        }
        steps.push(s4);

        const done = steps.filter(s => s.done).length;
        if (done === steps.length) { card.classList.add('hide'); return; }
        $('gs-count').textContent = done + ' of ' + steps.length + ' done';
        let nowSet = false, html = '';
        steps.forEach((s, i) => {
            let cls = 'step';
            if (s.done) cls += ' done';
            else if (!nowSet) { cls += ' now'; nowSet = true; }
            html += '<div class="' + cls + '"><span class="num">' + (s.done ? '&#10003;' : (i + 1)) + '</span><div><div class="t">' + esc(s.title) + '</div><div class="d">' + s.desc + '</div></div></div>';
        });
        $('gs-steps').innerHTML = html;
        card.classList.remove('hide');
    }

    // ---- Agent ------------------------------------------------------------

    function renderAgent(d, outputs) {
        const failedOutputs = (d.outputs_failing || []).concat((d.strategy_failures || []).map(f => f.strategy))
            .concat(((outputs && outputs.outputs) || []).filter(o => o.enabled && o.state === 'failing').map(o => o.name))
            .filter((n, i, a) => a.indexOf(n) === i);
        const failing = failedOutputs.length > 0;
        $('agent-pill').innerHTML = failing ? pill('warn', 'output failing') : pill('ok', d.status === 'running' || !d.status ? 'running' : d.status);
        const commit = d.commit ? ' (' + String(d.commit).slice(0, 7) + ')' : '';
        const res = d.resources || {};
        const cache = d.cache || {};
        const rows = [
            ['Version', esc((d.version || '?') + commit)],
            ['Host', esc(d.hostname || '?') + (d.os ? ', ' + esc(d.os + (d.arch ? '/' + d.arch : '')) : '')],
            ['Uptime', esc(uptime(d.uptime))],
            ['Port', esc(d.port || '?')],
            ['Config', d.config_path ? esc(d.config_path) : '-'],
            ['Memory', res.memory_usage_mb != null ? esc(Number(res.memory_usage_mb).toFixed(1)) + ' MB' : '-'],
            ['CPU', res.cpu_percent != null ? esc(Number(res.cpu_percent).toFixed(1)) + ' %' : '-'],
            ['Goroutines', res.goroutines != null ? num(res.goroutines) : '-'],
            ['Cache', cache.total_metrics != null ? num(cache.total_metrics) + ' series' + (cache.ttl ? ', TTL ' + esc(cache.ttl) : '') : '-']
        ];
        $('agent-kv').innerHTML = rows.map(r => '<dt>' + r[0] + '</dt><dd>' + r[1] + '</dd>').join('');
        const w = $('agent-watch');
        if (d.config_watch) {
            w.textContent = 'Configuration watch off: edits by hand need a restart' + (d.config_watch.reason ? ' (' + d.config_watch.reason + ')' : '') + '.';
            w.classList.remove('hide');
        } else {
            w.classList.add('hide');
        }
        const ts = d.health && d.health.timestamp ? parseTime(d.health.timestamp) : null;
        let foot = ts ? 'Health checked ' + clock(ts) : 'Health ' + esc((d.health && d.health.status) || '-');
        if (failing) foot += ', ' + failedOutputs.join(', ') + ' failing';
        $('agent-foot').textContent = foot;
    }

    // ---- Licence ----------------------------------------------------------

    function renderLicence(lic, catalog) {
        lic = lic || {};
        const tier = (lic.tier || 'free').toLowerCase();
        const tierLabel = tier.charAt(0).toUpperCase() + tier.slice(1);
        let p;
        if (lic.status === 'expired') p = pill('err', 'expired');
        else if (lic.status === 'grace_period') p = pill('warn', tierLabel + ', grace period');
        else if (lic.status === 'active') p = pill('pro', tierLabel);
        else p = pill('off', 'Free');
        $('lic-pill').innerHTML = p;
        const probes = (catalog && catalog.probes) || [];
        const available = probes.filter(x => x.authorized !== false).length;
        const locked = probes.filter(x => x.tier === 'pro' && x.authorized === false).length;
        let expires = '-';
        if (lic.expires_at) {
            expires = esc(dateOnly(lic.expires_at));
            if (lic.days_remaining != null) expires += ' <span class="muted">(' + num(lic.days_remaining) + ' d)</span>';
        }
        const rows = [
            ['Tier', esc(tierLabel)],
            ['Expires', expires],
            ['Probe types', catalog ? num(available) + ' available' : '-'],
            ['Pro types', catalog ? num(locked) + ' need a licence' : '-'],
            ['Agent key', '<span title="' + esc(KEY) + '">' + esc(shortKey(KEY)) + '</span><button type="button" class="btn sm" id="copy-key">Copy</button><span class="copied hide" id="copied">copied</span>']
        ];
        $('lic-kv').innerHTML = rows.map(r => '<dt>' + r[0] + '</dt><dd>' + r[1] + '</dd>').join('');
        $('copy-key').addEventListener('click', async () => {
            const ok = await copyText(KEY);
            const c = $('copied');
            c.textContent = ok ? 'copied' : 'copy failed';
            c.classList.remove('hide');
            setTimeout(() => c.classList.add('hide'), 1500);
        });
    }

    // ---- Events -----------------------------------------------------------

    function renderEvents(data) {
        const list = ((data && data.events) || []).slice(0, 10);
        $('events-count').textContent = data && data.count ? data.count : '';
        if (!list.length) {
            $('events-body').innerHTML = '<div class="empty-line">None yet</div>';
            return;
        }
        $('events-body').innerHTML = list.map(e => {
            const lvl = e.level === 'error' ? 'err' : (e.level === 'warn' ? 'warn' : 'ok');
            return '<div class="ev"><span class="dot ' + lvl + '"></span><div class="msg">' +
                (e.subject ? '<b>' + esc(e.subject) + '</b> ' : '') + short(e.message, 160) +
                '<div class="xs muted">' + esc(when(e.time)) + (e.kind ? ' &middot; ' + esc(e.kind) : '') + '</div></div></div>';
        }).join('');
    }

    // ---- Page -------------------------------------------------------------

    let bannerTimer = null;
    function showBanner(kind, msg) {
        const b = $('banner');
        b.className = 'banner ' + kind;
        b.textContent = msg;
        clearTimeout(bannerTimer);
        if (kind === 'ok') bannerTimer = setTimeout(hideBanner, 4000);
    }
    function hideBanner() { $('banner').className = 'banner err hide'; }

    async function refresh() {
        const names = ['info/system', 'config/probes', 'config/outputs', 'license/status', 'catalog/probes', 'info/events'];
        const res = await Promise.allSettled(names.map(get));
        const val = (i) => res[i].status === 'fulfilled' ? res[i].value : null;
        const failed = names.filter((n, i) => res[i].status === 'rejected');
        const system = val(0), probes = val(1), outputs = val(2), lic = val(3), catalog = val(4), events = val(5);

        let probeSt = null, outputList = null;
        if (system) renderAgent(system, outputs);
        else { $('agent-pill').innerHTML = pill('err', 'unreachable'); $('agent-kv').innerHTML = ''; }
        if (probes) probeSt = renderProbes(probes, system);
        else $('probes-body').innerHTML = '<div class="empty-line">Probe list unavailable.</div>';
        if (outputs) outputList = renderOutputs(outputs);
        else $('outputs-body').innerHTML = '<div class="empty-line">Output list unavailable.</div>';
        if (lic || catalog) renderLicence(lic, catalog);
        else $('lic-kv').innerHTML = '';
        if (events) renderEvents(events);
        else $('events-body').innerHTML = '<div class="empty-line">Events unavailable.</div>';
        if (system && probeSt && outputList) renderGettingStarted(system, probeSt, outputList);
        else $('gs-card').classList.add('hide');

        if (failed.length) showBanner('err', 'Could not load ' + failed.join(', ') + '. The rest of the page is current; the agent may be restarting.');
        else if ($('banner').classList.contains('err') && !$('banner').classList.contains('hide')) hideBanner();
        $('refresh-note').textContent = ', last ' + clock(new Date());
    }

    $('gs-hide').addEventListener('click', gsHide);
    refresh();
    setInterval(refresh, 30000);
})();

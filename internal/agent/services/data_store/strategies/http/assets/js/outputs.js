// SenHub Agent console: helpers shared by the Outputs pages
// (outputs.html, output-editor.html, output-http.html).
//
// Everything here is presentation: relative times, state pills, the
// facts and life lines of an output row, the rendering of a connection
// test, a small YAML serializer for previews. The API calls go through
// Outputs.api so every page reports server errors the same way.
(function () {
    const key = (window.AGENT_KEY || (document.querySelector('meta[name="agent-key"]') || {}).content || '').trim();
    const base = '/api/' + key + '/';
    const webBase = '/web/' + key + '/';

    function esc(s) {
        return String(s == null ? '' : s).replace(/[&<>"']/g, c => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c]));
    }

    async function api(path, opts) {
        const init = { method: (opts && opts.method) || 'GET', headers: {} };
        if (opts && opts.body !== undefined) {
            init.headers['Content-Type'] = 'application/json';
            init.body = JSON.stringify(opts.body);
        }
        const r = await fetch(base + path, init);
        let data = null;
        const text = await r.text();
        try { data = text ? JSON.parse(text) : {}; } catch (e) { data = { error: text || ('HTTP ' + r.status) }; }
        if (!r.ok) {
            const err = new Error((data && (data.error || data.message)) || ('HTTP ' + r.status));
            err.status = r.status;
            err.data = data;
            throw err;
        }
        return data;
    }

    // rel renders an ISO timestamp as "8 s ago", "3 min ago", "2 h ago".
    function rel(iso) {
        if (!iso) return '';
        const t = new Date(iso).getTime();
        if (isNaN(t)) return '';
        const s = Math.max(0, Math.round((Date.now() - t) / 1000));
        if (s < 60) return s + ' s ago';
        const m = Math.round(s / 60);
        if (m < 60) return m + ' min ago';
        const h = Math.round(m / 60);
        if (h < 48) return h + ' h ago';
        return Math.round(h / 24) + ' d ago';
    }

    function clock(d) {
        d = d || new Date();
        const p = n => String(n).padStart(2, '0');
        return p(d.getHours()) + ':' + p(d.getMinutes()) + ':' + p(d.getSeconds());
    }

    const STATE = {
        listening: 'ok', exporting: 'ok', failing: 'err', idle: 'warn', disabled: 'off'
    };
    function statePill(state) {
        const cls = STATE[state] || 'off';
        return '<span class="pill ' + cls + '"><span class="dot ' + cls + '"></span>' + esc(state || 'unknown') + '</span>';
    }
    function modePill(mode) {
        return '<span class="pill acc">' + esc(mode || 'push') + '</span>';
    }

    const SHORT = { http: 'HTTP', otlp: 'OTLP', prtg: 'PRTG', senhub: 'CLOUD', event: 'EVENT' };
    function shortType(t) { return SHORT[t] || String(t || '?').slice(0, 5).toUpperCase(); }

    // The agent reports the file's full path; on Windows it is backslash-separated.
    function basename(p) { return String(p || '').split(/[\\/]/).pop(); }

    function isRedacted(v) { return v === '***' || v === '[REDACTED]'; }

    // stripRedacted removes the values the listing masked, so they are
    // never written back as literal secrets. Returns the cleaned copy and
    // the dotted paths that were dropped.
    function stripRedacted(obj) {
        const stripped = [];
        function walk(v, path) {
            if (Array.isArray(v)) return v.map((x, i) => walk(x, path + '[' + i + ']'));
            if (v && typeof v === 'object') {
                const out = {};
                for (const k of Object.keys(v)) {
                    const p = path ? path + '.' + k : k;
                    if (isRedacted(v[k])) { stripped.push(p); continue; }
                    out[k] = walk(v[k], p);
                }
                return out;
            }
            return v;
        }
        return { params: walk(obj || {}, ''), stripped };
    }

    // The form engine owns the one YAML writer; every page that shows a
    // fragment loads it, so there is a single set of quoting rules.
    function toYAML(obj, indent) {
        return SchemaForm.toYAML(obj, indent || 0);
    }

    // yamlHTML colours keys and ${...} references the way pre.yaml expects.
    function yamlHTML(text) {
        return String(text).split('\n').map(line => {
            const m = line.match(/^(\s*)([^\s:#][^:]*?):(\s|$)(.*)$/);
            let out;
            if (m) out = esc(m[1]) + '<span class="k">' + esc(m[2]) + '</span>:' + esc(m[3]) + esc(m[4]);
            else out = esc(line);
            return out.replace(/(\$\{[^}]*\})/g, '<span class="s">$1</span>');
        }).join('\n');
    }

    function fragmentYAML(type, params) {
        const body = toYAML(params || {}, 2);
        return type + ':' + (body.trim() ? '\n' + body : ' {}');
    }

    function dur(v) { return v == null || v === '' ? '' : String(v); }
    function signalOn(sig, def) {
        if (!sig || typeof sig !== 'object' || sig.enabled === undefined) return def;
        return !!sig.enabled;
    }

    // facts renders the one-line description of an output from its params.
    function facts(o) {
        const p = o.params || {};
        const t = o.type;
        if (t === 'http') {
            const tls = p.tls && p.tls.enabled ? 'TLS on' : 'no TLS';
            const eps = Array.isArray(p.endpoints) ? p.endpoints : [];
            return '<code>' + esc((p.bind_address || '127.0.0.1') + ':' + (p.port || 8080)) + '</code>, ' + tls +
                (eps.length ? ' &middot; endpoints ' + eps.map(e => '<code>' + esc(e) + '</code>').join(' ') : '');
        }
        if (t === 'otlp') {
            const proto = (p.protocol || 'grpc') === 'http' ? 'HTTP' : 'gRPC';
            const tls = p.tls && p.tls.enabled === false ? 'TLS off' : 'TLS on';
            const hdr = p.headers && typeof p.headers === 'object' && Object.keys(p.headers).some(k => k.toLowerCase() === 'authorization');
            const sig = p.signals || {};
            const on = [], off = [];
            const mi = sig.metrics && sig.metrics.interval ? dur(sig.metrics.interval) : '30s';
            (signalOn(sig.metrics, true) ? on : off).push('metrics every ' + mi);
            (signalOn(sig.logs, true) ? on : off).push('logs');
            (signalOn(sig.traces, false) ? on : off).push('traces');
            (signalOn(sig.entities, false) ? on : off).push('entities');
            return '<code>' + esc(p.endpoint || '(no endpoint)') + '</code> ' + proto + ', ' + tls + (hdr ? ', bearer token' : '') +
                ' &middot; signals: ' + esc(on.join(', ') || 'none') +
                (off.length ? ' &middot; ' + esc(off.map(s => s.split(' ')[0]).join(', ')) + ' off' : '');
        }
        if (t === 'prtg') {
            return 'Push to <code>' + esc(p.server_url || '(no server_url)') + '</code> every ' + esc(dur(p.interval) || '5s') +
                ', values valid ' + esc(dur(p.data_retention_period) || '2m');
        }
        if (t === 'event') {
            return 'Push to <code>' + esc(p.server_url || '(no server_url)') + '</code>, sync every ' + esc(dur(p.sync_interval) || '30s') +
                ', queue ' + esc(p.queue_size || 1000);
        }
        if (t === 'senhub') {
            return 'Batches to <code>intake.senhub.io</code> every ' + esc(dur(p.interval) || '5s') + ', authenticated by the agent key';
        }
        const keys = Object.keys(p);
        return keys.length ? keys.map(k => '<code>' + esc(k) + '</code>').join(' ') : 'No parameters';
    }

    const FAMILY = { prtg: 'PRTG', nagios: 'Nagios', prometheus: 'Prometheus', web: 'Console' };

    // life renders the activity line: who read the http output, or the
    // last export of a push output.
    function life(o) {
        if (!o.enabled || o.state === 'disabled') {
            return 'Disabled. File <code>' + esc(basename(o.path)) + '</code>; nothing is lost.';
        }
        if (o.state === 'failing') {
            return '<span class="lifebad">' + esc(o.reason || 'failing') + '</span>';
        }
        if (o.type === 'http') {
            const readers = (o.readers || []).filter(r => r.endpoint !== 'web');
            const parts = [];
            for (const r of readers) {
                if (r.last) parts.push('Last ' + FAMILY[r.endpoint] + ' request ' + rel(r.last) + (r.from ? ' from ' + esc(r.from) : ''));
            }
            const enabled = readers.filter(r => r.enabled);
            const silent = enabled.filter(r => !r.last);
            if (enabled.length && silent.length) parts.push('<span class="lifewarn">no ' + esc(silent.map(r => FAMILY[r.endpoint]).join(' or ')) + ' request seen since start</span>');
            if (!enabled.length) parts.push('no pull endpoint enabled; the console only');
            return parts.join(' &middot; ');
        }
        const a = o.activity || {};
        if (a.last_success) {
            return 'Last export ' + rel(a.last_success) + ' &middot; ' + esc(a.successes || 0) + ' exports' +
                (a.failures ? ' &middot; <span class="lifewarn">' + esc(a.failures) + ' failures</span>' : ' &middot; 0 failures');
        }
        return 'No export yet' + (o.state === 'idle' ? ', nothing to send so far' : '');
    }

    // stepLabel names a connection test step for the operator.
    function stepLabel(step, params) {
        const p = params || {};
        const ep = String(p.endpoint || '');
        const host = ep.replace(/^\[?([^\]]*?)\]?:\d+$/, '$1');
        switch (step.name) {
            case 'config': return 'Config';
            case 'dns': return 'DNS ' + host;
            case 'tcp': return 'TCP ' + ep;
            case 'tls': return 'TLS';
            case 'export': return 'Export 1 metric';
            case 'reach': return 'Reach ' + (p.server_url || '');
            case 'listen': return 'Listen';
            default: return step.name;
        }
    }
    function stepValue(step) {
        if (!step.passed) return step.error || 'failed';
        if (step.name === 'tcp' || step.name === 'export') return (step.detail ? step.detail + ', ' : '') + (step.duration_ms || 0) + ' ms';
        return step.detail || 'OK';
    }
    function stepsHTML(res, params) {
        const steps = (res && res.steps) || [];
        return '<div class="metric-list">' + steps.map(s =>
            '<div class="' + (s.passed ? '' : 'bad') + '"><span>' + esc(stepLabel(s, params)) + '</span><span class="v">' + esc(stepValue(s)) + '</span></div>'
        ).join('') + '</div>';
    }
    function testPill(res) {
        const ok = !!(res && res.valid);
        const n = ((res && res.steps) || []).length;
        return '<span class="pill ' + (ok ? 'ok' : 'err') + '"><span class="dot ' + (ok ? 'ok' : 'err') + '"></span>' + (ok ? 'test passed' : 'test failed') + '</span>' +
            '<span class="small muted">' + n + ' step' + (n === 1 ? '' : 's') + ', ' + esc((res && res.duration_ms) || 0) + ' ms</span>';
    }

    function banner(el, cls, html) {
        if (!el) return;
        if (!html) { el.innerHTML = ''; el.classList.add('hide'); return; }
        el.className = 'banner ' + cls;
        el.innerHTML = '<div class="grow">' + html + '</div><button class="link-btn xs" type="button" data-close>Dismiss</button>';
        el.querySelector('[data-close]').addEventListener('click', () => { el.classList.add('hide'); });
    }

    window.Outputs = {
        key, base, webBase, esc, api, rel, clock, statePill, modePill, shortType, basename,
        isRedacted, stripRedacted, toYAML, yamlHTML, fragmentYAML, facts, life, stepsHTML, testPill, banner, FAMILY
    };
})();

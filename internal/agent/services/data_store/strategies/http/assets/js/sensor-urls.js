// SenHub Agent console: the Sensor URLs tab of the HTTP output.
//
// Builds the URL a poller reads (PRTG HTTP Data Advanced, Nagios,
// Prometheus) from three choices: the poller, the probe, the tag filter.
// The URL semantics are those of the former Sensor Builder:
//   /api/<key>/<format>/metrics/<probe>?tags=<key>:<v1,v2>&show_tags=false
// Prometheus answers for every probe at once on /api/<key>/prometheus/metrics.
// Requires base.js (SenHubBase) and tag-filters.js (TagFilters).
(function () {
    const FORMATS = {
        prtg: { title: 'PRTG', desc: 'HTTP Data Advanced sensor, JSON with channels and lookups.' },
        nagios: { title: 'Nagios', desc: 'check_http compatible text with perfdata and exit codes.' },
        prometheus: { title: 'Prometheus', desc: 'One scrape target for every probe. No per-probe URL.' }
    };

    function esc(s) {
        return String(s == null ? '' : s).replace(/[&<>"']/g, c => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c]));
    }

    class SensorURLs {
        constructor(agentKey, root) {
            this.key = agentKey;
            this.base = new SenHubBase(agentKey);
            this.root = root;
            this.$ = sel => root.querySelector(sel);
            this.fmtBox = this.$('#su-fmt');
            this.probeSel = this.$('#su-probe');
            this.filterWrap = this.$('#su-filter-wrap');
            this.tagsBox = this.$('#su-tags');
            this.showTags = this.$('#su-showtags');
            this.urlIn = this.$('#su-url');
            this.copyBtn = this.$('#su-copy');
            this.previewBtn = this.$('#su-preview');
            this.resp = this.$('#su-resp');
            this.respHead = this.$('#su-resp-head');
            this.respBody = this.$('#su-resp-body');
            this.hint = this.$('#su-hint');
            this.format = '';
            this.probe = '';
            this.tags = {};
            this.view = 'table';
            this.last = null;
            this.ready = false;
            this.tagFilters = new TagFilters(this.base, this.tagsBox, tags => { this.tags = tags; this.build(); });
            this.wire();
        }

        wire() {
            this.fmtBox.addEventListener('change', ev => {
                if (ev.target.name === 'su-format') { this.format = ev.target.value; this.build(); }
            });
            this.probeSel.addEventListener('change', () => this.setProbe(this.probeSel.value));
            this.showTags.addEventListener('change', () => this.build());
            this.copyBtn.addEventListener('click', () => this.copy());
            this.previewBtn.addEventListener('click', () => this.preview());
            this.respHead.addEventListener('click', ev => {
                const chip = ev.target.closest('[data-view]');
                if (chip) this.switchView(chip.dataset.view);
            });
        }

        // load fills the poller cards and the probe list, then applies the
        // deep link (#urls&probe=<name>&fmt=<format>&tag_<key>=<values>).
        async load(state) {
            state = state || {};
            await Promise.all([this.loadEndpoints(), this.loadProbes()]);
            if (state.fmt && this.fmtBox.querySelector('input[value="' + CSS.escape(state.fmt) + '"]')) {
                this.fmtBox.querySelector('input[value="' + CSS.escape(state.fmt) + '"]').checked = true;
                this.format = state.fmt;
            }
            if (state.probe) {
                const opt = [...this.probeSel.options].find(o => o.value === String(state.probe));
                if (opt) { this.probeSel.value = opt.value; await this.setProbe(opt.value, state.tags || {}); }
            }
            this.ready = true;
            this.build();
            if (this.format && (this.probe || this.format === 'prometheus')) this.preview();
        }

        async loadEndpoints() {
            let enabled = [];
            try {
                const data = await this.base.fetchAPI('info/endpoints');
                enabled = (data.endpoints || []).filter(e => e.enabled === true).map(e => e.name).filter(n => FORMATS[n]);
            } catch (e) {
                this.fmtBox.innerHTML = '<p class="hint">' + esc('Could not load the endpoints: ' + e.message) + '</p>';
                return;
            }
            if (!enabled.length) {
                this.fmtBox.innerHTML = '<p class="hint">No PRTG, Nagios or Prometheus endpoint is enabled on this output. Enable one in the Settings tab.</p>';
                return;
            }
            const order = ['prtg', 'nagios', 'prometheus'];
            enabled.sort((a, b) => order.indexOf(a) - order.indexOf(b));
            this.fmtBox.innerHTML = enabled.map((n, i) => {
                const f = FORMATS[n];
                return '<label><input type="radio" name="su-format" value="' + esc(n) + '"' + (i === 0 ? ' checked' : '') + '>' +
                    '<div><div class="t">' + esc(f.title) + '</div><div class="d">' + esc(f.desc) + '</div></div></label>';
            }).join('');
            this.format = enabled[0];
        }

        async loadProbes() {
            this.probeSel.innerHTML = '<option value="">Select a probe</option>';
            try {
                const data = await this.base.fetchAPI('info/probes');
                const probes = (data.probes || []).slice().sort((a, b) => a.localeCompare(b));
                const counts = data.probe_metrics || {};
                for (const p of probes) {
                    const opt = document.createElement('option');
                    opt.value = p;
                    opt.textContent = counts[p] != null ? p + ' (' + counts[p] + ' metrics)' : p;
                    this.probeSel.appendChild(opt);
                }
                if (!probes.length) this.probeSel.innerHTML = '<option value="">No probe has reported yet</option>';
            } catch (e) {
                this.probeSel.innerHTML = '<option value="">' + esc('Could not load the probes: ' + e.message) + '</option>';
            }
        }

        async setProbe(name, restoreTags) {
            this.probe = name;
            if (name) {
                await this.tagFilters.loadTags(name);
                this.filterWrap.classList.remove('hide');
                if (restoreTags && Object.keys(restoreTags).length) this.restoreTags(restoreTags);
            } else {
                this.tagFilters.clearTags();
                this.filterWrap.classList.add('hide');
            }
            this.build();
        }

        restoreTags(tags) {
            for (const [k, v] of Object.entries(tags)) {
                const main = this.tagsBox.querySelector('#tag-' + CSS.escape(k));
                if (!main) continue;
                main.checked = true;
                for (const value of String(v).split(',')) {
                    const cb = this.tagsBox.querySelector('#value-' + CSS.escape(k) + '-' + CSS.escape(this.tagFilters.escapeId(value)));
                    if (cb) cb.checked = true;
                }
                this.tagFilters.updateTagUI(k, 'multi', true);
            }
            this.tagFilters.updateSelectedTags();
        }

        path() {
            const isProm = this.format === 'prometheus';
            if (!this.format || (!this.probe && !isProm)) return '';
            let url = isProm ? '/api/' + this.key + '/prometheus/metrics'
                : '/api/' + this.key + '/' + this.format + '/metrics/' + encodeURIComponent(this.probe);
            const q = [];
            for (const [k, v] of Object.entries(this.tags)) q.push('tags=' + encodeURIComponent(k) + ':' + encodeURIComponent(v));
            if (!this.showTags.checked) q.push('show_tags=false');
            return url + (q.length ? '?' + q.join('&') : '');
        }

        build() {
            const isProm = this.format === 'prometheus';
            this.probeSel.disabled = isProm;
            const p = this.path();
            if (!p) {
                this.urlIn.value = '';
                this.urlIn.placeholder = this.format ? 'Pick a probe to build the URL' : 'Pick a poller to build the URL';
                this.copyBtn.disabled = this.previewBtn.disabled = true;
            } else {
                this.urlIn.value = location.origin + p;
                this.copyBtn.disabled = this.previewBtn.disabled = false;
            }
            if (this.hint) {
                this.hint.innerHTML = isProm
                    ? 'Prometheus scrapes every probe from this one URL; a probe selection only narrows the tag filter.'
                    : this.format === 'nagios'
                        ? 'For Nagios: a <code>check_http</code> command on this URL, or the summary on <code>/nagios/checks</code>.'
                        : 'For PRTG: Add Sensor, HTTP Data Advanced, paste as URL. Uses the host name of this console; replace it with one PRTG can reach.';
            }
            this.updateHash();
        }

        updateHash() {
            if (!this.ready) return;
            const parts = ['urls'];
            if (this.format) parts.push('fmt=' + encodeURIComponent(this.format));
            if (this.probe) parts.push('probe=' + encodeURIComponent(this.probe));
            for (const [k, v] of Object.entries(this.tags)) parts.push('tag_' + encodeURIComponent(k) + '=' + encodeURIComponent(v));
            history.replaceState({}, '', location.pathname + location.search + '#' + parts.join('&'));
        }

        async copy() {
            if (!this.urlIn.value) return;
            const ok = await this.base.copyToClipboard(this.urlIn.value);
            const t = this.copyBtn.textContent;
            this.copyBtn.textContent = ok ? 'Copied' : 'Copy failed';
            setTimeout(() => { this.copyBtn.textContent = t; }, 1500);
        }

        async preview() {
            const p = this.path();
            if (!p) return;
            this.resp.classList.remove('hide');
            this.respHead.innerHTML = '<span class="small muted">Loading...</span>';
            this.respBody.innerHTML = '';
            const t0 = performance.now();
            try {
                const r = await fetch(p);
                const ms = Math.round(performance.now() - t0);
                const text = await r.text();
                let json = null;
                if (this.format === 'prtg') { try { json = JSON.parse(text); } catch (e) { json = null; } }
                const results = json && json.prtg && Array.isArray(json.prtg.result) ? json.prtg.result : null;
                this.last = { status: r.status, ms, text, json, results };
                this.view = results ? 'table' : 'json';
                this.renderResp();
            } catch (e) {
                this.respHead.innerHTML = '<span class="pill err"><span class="dot err"></span>request failed</span>';
                this.respBody.innerHTML = '<p class="hint">' + esc(e.message) + '</p>';
            }
        }

        renderResp() {
            const L = this.last;
            if (!L) return;
            const ok = L.status >= 200 && L.status < 300;
            let head = '<span class="small"><span class="pill ' + (ok ? 'ok' : 'err') + '"><span class="dot ' + (ok ? 'ok' : 'err') + '"></span>' + L.status + '</span> ';
            if (L.results) head += L.results.length + ' channel' + (L.results.length === 1 ? '' : 's') + ', ';
            else head += L.text.split('\n').filter(Boolean).length + ' lines, ';
            head += L.ms + ' ms</span>';
            if (L.results) {
                head += '<span class="row"><button type="button" class="chip' + (this.view === 'table' ? ' on' : '') + '" data-view="table">Table</button>' +
                    '<button type="button" class="chip' + (this.view === 'json' ? ' on' : '') + '" data-view="json">JSON</button></span>';
            }
            this.respHead.innerHTML = head;
            if (L.results && this.view === 'table') {
                const hasLookup = L.results.some(r => r.ValueLookup || r.valuelookup);
                let html = '<div class="resp"><table><thead><tr><th>Channel</th><th>Value</th><th>Unit</th>' + (hasLookup ? '<th>Lookup</th>' : '') + '</tr></thead><tbody>';
                for (const r of L.results) {
                    const v = typeof r.value === 'number' ? (Number.isInteger(r.value) ? r.value : r.value.toFixed(2)) : r.value;
                    html += '<tr><td>' + esc(r.channel) + '</td><td>' + esc(v) + '</td><td>' + esc(r.customunit || r.unit || '') + '</td>' +
                        (hasLookup ? '<td>' + esc(r.ValueLookup || r.valuelookup || '') + '</td>' : '') + '</tr>';
                }
                html += '</tbody></table></div>';
                this.respBody.innerHTML = html;
            } else {
                const text = L.json ? JSON.stringify(L.json, null, 2) : L.text;
                const pre = document.createElement('pre');
                pre.className = 'dark';
                pre.textContent = text;
                this.respBody.innerHTML = '';
                this.respBody.appendChild(pre);
            }
        }

        switchView(v) { this.view = v; this.renderResp(); }
    }

    // parseHash reads "#urls&fmt=prtg&probe=cpu&tag_core=0,1" into
    // {tab, fmt, probe, tags}. Legacy ?endpoint=&probe=&tag_x= query
    // parameters of the old Sensor Builder are honoured too.
    SensorURLs.parseHash = function () {
        const out = { tab: 'settings', tags: {} };
        const h = (location.hash || '').replace(/^#/, '');
        const parts = h.split('&');
        if (parts[0]) out.tab = parts[0];
        const read = (k, v) => {
            if (k === 'probe') out.probe = v;
            else if (k === 'fmt' || k === 'endpoint') out.fmt = v;
            else if (k.startsWith('tag_')) out.tags[k.slice(4)] = v;
        };
        const dec = (s) => { try { return decodeURIComponent(s); } catch (e) { return null; } };
        for (const p of parts.slice(1)) {
            const i = p.indexOf('=');
            if (i <= 0) continue;
            const k = dec(p.slice(0, i)), v = dec(p.slice(i + 1));
            if (k !== null && v !== null) read(k, v);
        }
        for (const [k, v] of new URLSearchParams(location.search).entries()) read(k, v);
        if (out.probe && out.tab === 'settings' && location.search) out.tab = 'urls';
        return out;
    };

    window.SensorURLs = SensorURLs;
})();

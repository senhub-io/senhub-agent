// SenHub Agent console: the schema-driven form engine shared by the probe
// and output editors. Framework-free, self-contained.
//
// A SchemaForm renders the ParamSpec list of a catalogue entry into two
// places: the "Required to start" grid (required, essential and
// conditionally essential keys) and one collapsed <details class="sec">
// per schema group. It keeps the values as a nested object and reports
// only what the operator set: defaults, empty strings, empty lists and
// stored or redacted secrets are never sent back.
(function (global) {
    'use strict';

    const GROUP_LABELS = {
        connection: 'Connection', auth: 'Authentication', tls: 'TLS', collection: 'Collection',
        replication: 'Replication', detail: 'Detail metrics', identity: 'Identity', metrics: 'Metrics',
        discovery: 'Topology discovery', targets: 'Targets', request: 'Request', source: 'Source',
        filter: 'Filters', advanced: 'Advanced', command: 'Command', privacy: 'Privacy', parsing: 'Parsing',
        filtering: 'Filtering', selection: 'Selection', listen: 'Listen', cache: 'Cache', prometheus: 'Prometheus',
        signals: 'Signals', delivery: 'Delivery', resource: 'Resource attributes', memory: 'Memory and persistence',
        governance: 'Governance', general: 'General', settings: 'Settings',
    };

    const CHEV = '<svg class="chev" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5"><path d="m9 6 6 6-6 6"/></svg>';
    let uidCounter = 0;

    function esc(s) {
        return String(s == null ? '' : s).replace(/[&<>"']/g, c => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c]));
    }
    function titleCase(s) {
        return String(s || '').replace(/[_-]+/g, ' ').replace(/\b\w/g, c => c.toUpperCase());
    }
    function clone(v) { return v === undefined ? undefined : JSON.parse(JSON.stringify(v)); }
    function isObj(v) { return v !== null && typeof v === 'object' && !Array.isArray(v); }
    function isStored(v) {
        return typeof v === 'string' && (v === '***' || v === '[REDACTED]' || v.startsWith('${secret:'));
    }
    function isEmpty(v) {
        if (v === undefined || v === null || v === '') return true;
        if (Array.isArray(v)) return v.length === 0;
        if (isObj(v)) return Object.keys(v).length === 0;
        return false;
    }
    function fmtScalar(v) {
        if (Array.isArray(v)) return v.map(fmtScalar).join(', ');
        if (typeof v === 'boolean') return v ? 'on' : 'off';
        if (isStored(v)) return 'stored';
        if (isObj(v)) return Object.keys(v).length + ' set';
        return String(v);
    }
    function hasNestedBlocks(p) {
        return (p.fields || []).some(f => f.kind === 'block' || f.kind === 'block_list');
    }
    function el(tag, cls, html) {
        const e = document.createElement(tag);
        if (cls) e.className = cls;
        if (html !== undefined) e.innerHTML = html;
        return e;
    }

    class SchemaForm {
        constructor(opts) {
            opts = opts || {};
            this.starter = opts.starter || null;
            this.sections = opts.sections || null;
            this.params = opts.params || [];
            this.groupLabels = Object.assign({}, GROUP_LABELS, opts.groupLabels || {});
            this.hide = opts.hide || [];
            this.onChange = typeof opts.onChange === 'function' ? opts.onChange : function () {};
            this.uid = 'sf' + (++uidCounter);
            this._open = new Set();
            this._rows = {};
            this._errors = [];
            this.setValues(opts.values || {}, true);
        }

        // ---------- values ----------

        get(path) {
            return String(path).split('.').reduce((o, k) => (o == null ? undefined : o[k]), this._values);
        }
        set(path, v) {
            const parts = String(path).split('.');
            let o = this._values;
            for (let i = 0; i < parts.length - 1; i++) {
                if (!isObj(o[parts[i]])) o[parts[i]] = {};
                o = o[parts[i]];
            }
            const last = parts[parts.length - 1];
            if (v === undefined || v === '' || (Array.isArray(v) && v.length === 0)) delete o[last];
            else o[last] = v;
        }
        setValues(obj, silent) {
            this._values = this._normalise(clone(obj) || {}, this.params);
            this._rows = {};
            if (!silent) this.render();
        }
        // values() is the nested object of what the operator set. It skips
        // hidden top-level keys, hidden alternatives (an essential_when that
        // does not hold), defaults typed back as themselves, empty values,
        // and stored or redacted secrets, which the server keeps.
        values() {
            const out = {};
            for (const p of this.params) {
                if (this.hide.includes(p.key) || this.isHidden(p)) continue;
                const v = this._clean(this._values[p.key]);
                if (v !== undefined) out[p.key] = v;
            }
            for (const k of Object.keys(this._values)) {
                if (this.params.some(p => p.key === k)) continue;
                if (this.hide.includes(k)) continue;
                const v = this._clean(this._values[k]);
                if (v !== undefined) out[k] = v;
            }
            return out;
        }
        _clean(v) {
            if (v === undefined || v === null) return undefined;
            if (typeof v === 'string') return (v === '' || isStored(v)) ? undefined : v;
            if (Array.isArray(v)) {
                const a = v.map(x => this._clean(x)).filter(x => x !== undefined);
                return a.length ? a : undefined;
            }
            if (isObj(v)) {
                const o = {};
                for (const k of Object.keys(v)) {
                    const c = this._clean(v[k]);
                    if (c !== undefined) o[k] = c;
                }
                return Object.keys(o).length ? o : undefined;
            }
            return v;
        }
        _normalise(obj, params) {
            if (!isObj(obj)) return {};
            for (const p of params || []) {
                if (p.also_accepts && obj[p.key] === undefined) {
                    for (const alt of p.also_accepts) {
                        if (obj[alt] !== undefined) { obj[p.key] = obj[alt]; delete obj[alt]; break; }
                    }
                }
                if (p.kind === 'block') {
                    if (typeof obj[p.key] === 'boolean' && (p.fields || []).some(f => f.key === 'enabled')) obj[p.key] = { enabled: obj[p.key] };
                    if (isObj(obj[p.key])) obj[p.key] = this._normalise(obj[p.key], p.fields);
                }
            }
            return obj;
        }

        // ---------- conditions ----------

        _condHolds(cond) {
            if (!cond || !cond.length) return true;
            return cond.every(c => {
                let cur = this._values[c.key];
                if (cur === undefined || cur === '') {
                    const sib = this.params.find(p => p.key === c.key);
                    cur = sib ? sib.default : undefined;
                }
                return (c.values || []).some(v => String(v) === String(cur));
            });
        }
        isEssential(p) {
            return !!(p.required || p.essential || (p.essential_when && p.essential_when.length && this._condHolds(p.essential_when)));
        }
        isHidden(p) {
            return !!(p.essential_when && p.essential_when.length && !this._condHolds(p.essential_when));
        }
        _visible() {
            return this.params.filter(p => !this.hide.includes(p.key) && !this.isHidden(p));
        }
        essentialKeys() {
            return this._visible().filter(p => this.isEssential(p)).map(p => p.key);
        }
        paths() {
            const out = [];
            const walk = (prefix, params) => {
                for (const p of params || []) {
                    out.push(prefix + p.key);
                    if (p.kind === 'block' || p.kind === 'block_list') walk(prefix + p.key + '.', p.fields);
                }
            };
            walk('', this._visible());
            return out;
        }
        missing() {
            const out = [];
            const walkBlock = (prefix, fields, val) => {
                for (const f of fields || []) {
                    const v = val ? val[f.key] : undefined;
                    if (f.required && isEmpty(v)) out.push({ key: prefix + f.key, label: prefix + f.key });
                    else if (f.kind === 'block' && (f.required || !isEmpty(v))) walkBlock(prefix + f.key + '.', f.fields, v);
                }
            };
            for (const p of this._visible()) {
                const v = this._values[p.key];
                if (p.required && isEmpty(v)) { out.push({ key: p.key, label: p.key }); continue; }
                if (p.kind === 'block' && (this.isEssential(p) || !isEmpty(v))) walkBlock(p.key + '.', p.fields, v);
            }
            return out;
        }

        // ---------- summaries ----------

        _groups() {
            const groups = [];
            for (const p of this._visible()) {
                if (this.isEssential(p)) continue;
                const name = p.group || 'general';
                let g = groups.find(x => x.name === name);
                if (!g) { g = { name, params: [] }; groups.push(g); }
                g.params.push(p);
            }
            return groups;
        }
        _summaryOf(params) {
            const parts = [];
            for (const p of params) {
                const v = this._values[p.key];
                if (p.kind === 'block') {
                    const c = this._clean(v);
                    if (c) parts.push(p.key + ' ' + (c.enabled === false ? 'off' : (c.enabled === true ? 'on' : Object.keys(c).length + ' set')));
                    continue;
                }
                if (p.kind === 'block_list' || p.kind === 'map') {
                    const c = this._clean(v);
                    if (c) parts.push((Array.isArray(c) ? c.length : Object.keys(c).length) + ' ' + p.key);
                    continue;
                }
                if (!isEmpty(v)) parts.push(p.key + ' = ' + fmtScalar(v));
            }
            if (parts.length) return { text: parts.join(' · '), set: true, count: parts.length };
            const d = params.filter(p => p.default !== undefined && p.kind !== 'block').slice(0, 3).map(p => p.key + ' ' + fmtScalar(p.default));
            return { text: d.length ? 'Defaults: ' + d.join(', ') : 'Defaults', set: false, count: 0 };
        }
        summary(group) {
            const g = this._groups().find(x => x.name === group);
            return g ? this._summaryOf(g.params).text : '';
        }
        groupLabel(name) {
            return this.groupLabels[name] || titleCase(name);
        }

        // ---------- rendering ----------

        render() {
            if (this.starter) {
                this.starter.querySelectorAll(':scope > [data-sf="' + this.uid + '"]').forEach(n => n.remove());
                for (const p of this._visible()) {
                    if (!this.isEssential(p)) continue;
                    const node = this._paramNode(p, p.key, true);
                    node.dataset.sf = this.uid;
                    this.starter.appendChild(node);
                }
            }
            if (this.sections) {
                this.sections.innerHTML = '';
                for (const g of this._groups()) {
                    const d = el('details', 'sec');
                    d.dataset.group = g.name;
                    if (this._open.has(g.name)) d.open = true;
                    const s = this._summaryOf(g.params);
                    d.innerHTML = '<summary>' + CHEV + '<span class="t">' + esc(this.groupLabel(g.name)) + '</span><span class="sum' + (s.set ? ' set' : '') + '"></span></summary>';
                    this._fillSummary(d.querySelector('.sum'), s);
                    d.addEventListener('toggle', () => { if (d.open) this._open.add(g.name); else this._open.delete(g.name); });
                    const body = el('div', 'body');
                    const grid = el('div', 'fgrid');
                    for (const p of g.params) grid.appendChild(this._paramNode(p, p.key, false));
                    body.appendChild(grid);
                    d.appendChild(body);
                    this.sections.appendChild(d);
                }
            }
            if (this._errors.length) this._applyErrors();
        }
        _fillSummary(span, s) {
            span.className = 'sum' + (s.set ? ' set' : '');
            span.innerHTML = (s.set ? '<span class="n">' + s.count + '</span> ' : '') + esc(s.text);
        }
        _refreshSummaries() {
            if (!this.sections) return;
            for (const g of this._groups()) {
                const d = this.sections.querySelector('details[data-group="' + g.name + '"]');
                if (d) this._fillSummary(d.querySelector('.sum'), this._summaryOf(g.params));
            }
        }
        // _changed runs after every edit: structural edits re-render
        // (the starter block can change with an enum), text edits only
        // refresh the summaries so the focused input survives.
        _changed(structural, path) {
            if (structural) this.render();
            else this._refreshSummaries();
            this.onChange(this.values(), path);
        }
        _affectsConditions(path) {
            return this.params.some(p => (p.essential_when || []).some(c => c.key === path));
        }

        _paramNode(p, path, inStarter) {
            let node;
            if (p.kind === 'block') node = this._block(p, path);
            else if (p.kind === 'block_list') node = this._blockList(p, path);
            else if (p.kind === 'map') node = this._map(p, path);
            else node = this._control(p, path);
            if (p.kind === 'block' || p.kind === 'block_list' || p.kind === 'map' || (p.kind === 'string_list' && !p.enum)) node.classList.add('span');
            return node;
        }

        _label(p, extra) {
            const lbl = el('span', 'lbl');
            let h = esc(p.key);
            if (p.required) h += ' <span class="req">Required</span>';
            else if (p.default !== undefined && p.kind !== 'block' && p.kind !== 'bool') h += ' <span class="opt">default ' + esc(fmtScalar(p.default)) + '</span>';
            if (extra) h += extra;
            lbl.innerHTML = h;
            return lbl;
        }
        _field(p, path) {
            const wrap = el('div', 'field');
            wrap.dataset.path = path;
            wrap.appendChild(this._label(p));
            return wrap;
        }
        _finish(wrap, p, stored) {
            const hint = el('span', 'hint');
            let t = p.description || '';
            const sep = t ? (/[.!?]$/.test(t) ? ' ' : '. ') : '';
            if (p.secret && !stored) t += sep + 'Kept in the secret store, never in the file.';
            if (!p.secret && stored) t += sep + 'The agent hides this value; Replace to set a new one.';
            if (t.trim()) { hint.textContent = t; wrap.appendChild(hint); }
            const ferr = el('span', 'ferr');
            ferr.hidden = true;
            wrap.appendChild(ferr);
            return wrap;
        }

        _control(p, path) {
            const v = this.get(path);
            const wrap = this._field(p, path);
            const stored = isStored(v);
            const self = this;
            if (stored) {
                const st = el('div', 'secret-state');
                const ref = v.startsWith('${') ? v : '';
                st.innerHTML = '<span aria-hidden="true">&#9679;</span><span>Stored</span><code title="' + esc(ref || 'kept by the agent') + '">' + esc(ref || 'value kept by the agent') + '</code><button type="button" class="btn sm">Replace</button>';
                st.querySelector('button').onclick = () => { self.set(path, ''); self._changed(true, path); self.focus(path); };
                wrap.appendChild(st);
                return this._finish(wrap, p, true);
            }
            if (p.kind === 'bool') {
                const cur = (v === undefined || v === '') ? !!p.default : !!v;
                const sw = el('label', 'sw');
                sw.style.height = '34px';
                sw.innerHTML = '<input type="checkbox"' + (cur ? ' checked' : '') + '><span class="k"></span><span class="sw-t">' + (cur ? 'On' : 'Off') + '</span>';
                sw.querySelector('input').onchange = (e) => {
                    const on = e.target.checked;
                    self.set(path, (p.default !== undefined && on === !!p.default) ? undefined : on);
                    self._changed(true, path);
                };
                wrap.appendChild(sw);
                return this._finish(wrap, p, false);
            }
            if (p.enum && p.kind !== 'string_list') {
                if (p.enum.length <= 3) {
                    const seg = el('div', 'seg');
                    const cur = (v === undefined || v === '') ? p.default : v;
                    for (const e of p.enum) {
                        const l = el('label');
                        l.innerHTML = '<input type="radio" name="' + esc(this.uid + '-' + path) + '"' + (String(cur) === String(e) ? ' checked' : '') + '><span>' + esc(e) + '</span>';
                        l.querySelector('input').onchange = () => { self.set(path, e); self._changed(true, path); };
                        seg.appendChild(l);
                    }
                    wrap.appendChild(seg);
                } else {
                    const sel = el('select', 'sel');
                    sel.innerHTML = '<option value="">' + (p.default !== undefined ? 'Default: ' + esc(p.default) : 'Not set') + '</option>' +
                        p.enum.map(e => '<option value="' + esc(e) + '"' + (String(v) === String(e) ? ' selected' : '') + '>' + esc(e) + '</option>').join('');
                    sel.onchange = (e) => { self.set(path, e.target.value); self._changed(true, path); };
                    wrap.appendChild(sel);
                }
                return this._finish(wrap, p, false);
            }
            if (p.kind === 'string_list' && p.enum) {
                const row = el('div', 'row wrap');
                row.style.minHeight = '34px';
                const cur = Array.isArray(v) ? v : (Array.isArray(p.default) ? p.default : []);
                for (const e of p.enum) {
                    const c = el('button', 'chip' + (cur.includes(e) ? ' on' : ''));
                    c.type = 'button';
                    c.textContent = e;
                    c.onclick = () => {
                        const s = new Set(Array.isArray(self.get(path)) ? self.get(path) : cur);
                        if (s.has(e)) s.delete(e); else s.add(e);
                        self.set(path, [...s]);
                        self._changed(true, path);
                    };
                    row.appendChild(c);
                }
                wrap.appendChild(row);
                return this._finish(wrap, p, false);
            }
            if (p.kind === 'string_list') {
                const ta = el('textarea', 'ta');
                ta.style.minHeight = '60px';
                ta.placeholder = p.example ? p.example : 'One per line';
                ta.value = Array.isArray(v) ? v.join('\n') : (v == null ? '' : String(v));
                ta.oninput = (e) => { self.set(path, e.target.value.split('\n').map(s => s.trim()).filter(Boolean)); self._changed(false, path); };
                wrap.appendChild(ta);
                return this._finish(wrap, p, false);
            }
            const w = el('div', 'inwrap');
            const inp = el('input', 'in');
            inp.type = p.secret ? 'password' : ((p.kind === 'int' || p.kind === 'float') ? 'number' : 'text');
            if (p.kind === 'float') inp.step = 'any';
            if (p.kind === 'duration') inp.placeholder = (p.default !== undefined ? 'Default: ' + p.default : 'e.g. 30s') + ' (30s, 5m)';
            else inp.placeholder = p.example ? 'e.g. ' + p.example : (p.default !== undefined ? 'Default: ' + fmtScalar(p.default) : '');
            if (v !== undefined && v !== null) inp.value = String(v);
            inp.autocomplete = p.secret ? 'new-password' : 'off';
            inp.oninput = (e) => {
                let val = e.target.value;
                if (val !== '' && (p.kind === 'int' || p.kind === 'float') && inp.type === 'number') {
                    const n = Number(val);
                    val = Number.isFinite(n) ? n : val;
                }
                self.set(path, val);
                self._changed(false, path);
            };
            w.appendChild(inp);
            if (p.secret) {
                const aff = el('div', 'aff');
                const show = el('button', 'btn ghost', 'Show');
                show.type = 'button'; show.title = 'Show the value';
                show.onclick = () => { const t = inp.type === 'password'; inp.type = t ? 'text' : 'password'; show.textContent = t ? 'Hide' : 'Show'; };
                const ref = el('button', 'btn ghost', 'Ref');
                ref.type = 'button'; ref.title = 'Reference a secret already in the store, or an environment variable';
                ref.onclick = () => {
                    inp.type = 'text'; show.textContent = 'Hide';
                    inp.placeholder = '${secret:name} or ${env:VAR}';
                    if (!inp.value) { inp.value = '${'; self.set(path, inp.value); }
                    inp.focus(); inp.setSelectionRange(inp.value.length, inp.value.length);
                };
                aff.appendChild(show); aff.appendChild(ref);
                w.appendChild(aff);
            }
            wrap.appendChild(w);
            return this._finish(wrap, p, false);
        }

        _block(p, path) {
            const b = el('div', 'blk field');
            b.dataset.path = path;
            const h = el('div', 'bh');
            h.innerHTML = '<span class="t">' + esc(p.key) + (p.required ? ' <span class="req">Required</span>' : '') + '</span><span class="hint">' + esc(p.description || '') + '</span>';
            b.appendChild(h);
            const g = el('div', 'fgrid');
            for (const f of p.fields || []) {
                const node = this._paramNode(f, path + '.' + f.key, false);
                g.appendChild(node);
            }
            b.appendChild(g);
            const ferr = el('span', 'ferr'); ferr.hidden = true; b.appendChild(ferr);
            return b;
        }

        _blockList(p, path) {
            const self = this;
            const b = el('div', 'blk field');
            b.dataset.path = path;
            b.innerHTML = '<div class="bh"><span class="t">' + esc(p.key) + (p.required ? ' <span class="req">Required</span>' : '') + '</span><span class="hint">' + esc(p.description || '') + '</span></div>';
            const rows = Array.isArray(this.get(path)) ? this.get(path) : [];
            if (hasNestedBlocks(p)) {
                b.appendChild(el('p', 'hint', esc(rows.length) + ' ' + (rows.length === 1 ? 'entry' : 'entries') + '. Nested entries are edited in <b>Edit as YAML</b>; the form only counts them.'));
                const ferr = el('span', 'ferr'); ferr.hidden = true; b.appendChild(ferr);
                return b;
            }
            const t = el('table', 'rows');
            t.innerHTML = '<thead><tr>' + (p.fields || []).map(f => '<th>' + esc(f.key) + (f.required ? ' <span class="req">Req</span>' : '') + '</th>').join('') + '<th></th></tr></thead>';
            const tb = el('tbody');
            if (!rows.length) tb.innerHTML = '<tr><td colspan="' + ((p.fields || []).length + 1) + '" class="hint" style="padding:6px 4px">None.</td></tr>';
            rows.forEach((r, i) => {
                const tr = el('tr');
                for (const f of p.fields || []) {
                    const td = el('td');
                    td.appendChild(this._cell(f, r, i, path));
                    tr.appendChild(td);
                }
                const td = el('td'); td.style.width = '32px';
                const x = el('button', 'btn sm ghost', '&times;'); x.type = 'button'; x.title = 'Remove';
                x.onclick = () => { const arr = self.get(path).slice(); arr.splice(i, 1); self.set(path, arr); self._changed(true, path); };
                td.appendChild(x); tr.appendChild(td);
                tb.appendChild(tr);
            });
            t.appendChild(tb); b.appendChild(t);
            const add = el('div'); add.style.marginTop = '6px';
            const btn = el('button', 'btn sm', 'Add'); btn.type = 'button';
            btn.onclick = () => { const arr = (self.get(path) || []).slice(); arr.push({}); self._setRaw(path, arr); self._changed(true, path); };
            add.appendChild(btn); b.appendChild(add);
            const ferr = el('span', 'ferr'); ferr.hidden = true; b.appendChild(ferr);
            return b;
        }
        // _setRaw keeps an empty row alive (set() would drop an empty list).
        _setRaw(path, v) {
            const parts = String(path).split('.');
            let o = this._values;
            for (let i = 0; i < parts.length - 1; i++) { if (!isObj(o[parts[i]])) o[parts[i]] = {}; o = o[parts[i]]; }
            o[parts[parts.length - 1]] = v;
        }
        _cell(f, row, i, path) {
            const self = this;
            const cur = row[f.key];
            const update = (val) => {
                const arr = self.get(path).slice();
                arr[i] = Object.assign({}, arr[i]);
                if (val === '' || val === undefined) delete arr[i][f.key]; else arr[i][f.key] = val;
                self._setRaw(path, arr);
                self._changed(false, path);
            };
            if (f.kind === 'bool') {
                const c = el('input'); c.type = 'checkbox'; c.checked = cur === undefined ? !!f.default : !!cur;
                c.onchange = (e) => update(e.target.checked);
                return c;
            }
            if (f.enum) {
                const s = el('select', 'sel');
                s.innerHTML = '<option value="">' + (f.default !== undefined ? 'Default: ' + esc(f.default) : 'Not set') + '</option>' + f.enum.map(e => '<option value="' + esc(e) + '"' + (String(cur) === String(e) ? ' selected' : '') + '>' + esc(e) + '</option>').join('');
                s.onchange = (e) => update(e.target.value);
                return s;
            }
            const inp = el('input', 'in');
            inp.type = f.secret ? 'password' : ((f.kind === 'int' || f.kind === 'float') ? 'number' : 'text');
            inp.placeholder = f.description || f.key;
            if (f.kind === 'string_list') { inp.placeholder = (f.description || f.key) + ' (comma-separated)'; if (Array.isArray(cur)) inp.value = cur.join(', '); }
            else if (cur !== undefined && cur !== null) inp.value = isStored(cur) ? '' : String(cur);
            if (isStored(cur)) inp.placeholder = 'Stored';
            inp.oninput = (e) => {
                let val = e.target.value;
                if (f.kind === 'string_list') val = val.split(',').map(s => s.trim()).filter(Boolean);
                else if (val !== '' && (f.kind === 'int' || f.kind === 'float')) { const n = Number(val); val = Number.isFinite(n) ? n : val; }
                update(val);
            };
            return inp;
        }

        _map(p, path) {
            const self = this;
            const b = this._field(p, path);
            b.classList.add('blk');
            b.querySelector('.lbl').insertAdjacentHTML('beforeend', ' <span class="opt">key = value</span>');
            if (!this._rows[path]) {
                const v = this.get(path);
                this._rows[path] = isObj(v) ? Object.keys(v).map(k => [k, v[k]]) : [];
            }
            const rows = this._rows[path];
            const sync = () => {
                const o = {};
                for (const [k, val] of rows) if (k.trim()) o[k.trim()] = val;
                self.set(path, Object.keys(o).length ? o : undefined);
            };
            const t = el('table', 'rows');
            const tb = el('tbody');
            if (!rows.length) tb.innerHTML = '<tr><td colspan="3" class="hint" style="padding:6px 4px">None.</td></tr>';
            rows.forEach((r, i) => {
                const tr = el('tr');
                const k = el('input', 'in'); k.placeholder = 'key'; k.value = r[0];
                k.oninput = (e) => { rows[i][0] = e.target.value; sync(); self._changed(false, path); };
                const v = el('input', 'in'); v.placeholder = 'value'; v.value = isStored(r[1]) ? '' : String(r[1] == null ? '' : r[1]);
                if (isStored(r[1])) v.placeholder = 'Stored';
                v.oninput = (e) => { rows[i][1] = e.target.value; sync(); self._changed(false, path); };
                const x = el('button', 'btn sm ghost', '&times;'); x.type = 'button'; x.title = 'Remove';
                x.onclick = () => { rows.splice(i, 1); sync(); self._changed(true, path); };
                const td1 = el('td'), td2 = el('td'), td3 = el('td'); td3.style.width = '32px';
                td1.appendChild(k); td2.appendChild(v); td3.appendChild(x);
                tr.appendChild(td1); tr.appendChild(td2); tr.appendChild(td3);
                tb.appendChild(tr);
            });
            t.appendChild(tb); b.appendChild(t);
            const add = el('div'); add.style.marginTop = '6px';
            const btn = el('button', 'btn sm', 'Add'); btn.type = 'button';
            btn.onclick = () => { rows.push(['', '']); self._changed(true, path); const last = b.querySelector('tbody tr:last-child input'); if (last) last.focus(); };
            add.appendChild(btn); b.appendChild(add);
            return this._finish(b, p, false);
        }

        // ---------- errors and focus ----------

        _fieldNode(path) {
            const roots = [this.starter, this.sections].filter(Boolean);
            let p = String(path).replace(/^params\./, '').replace(/\[\d+\]/g, '');
            while (p) {
                for (const r of roots) {
                    const n = r.querySelector('[data-path="' + p.replace(/"/g, '\\"') + '"]');
                    if (n) return n;
                }
                const i = p.lastIndexOf('.');
                if (i < 0) break;
                p = p.slice(0, i);
            }
            return null;
        }
        showErrors(list) {
            this._errors = (list || []).map(e => ({ key: String(e.key || ''), message: String(e.message || '') }));
            return this._applyErrors();
        }
        _applyErrors() {
            let anchored = 0;
            for (const e of this._errors) {
                const f = this._fieldNode(e.key);
                if (!f) continue;
                anchored++;
                f.querySelectorAll(':scope > .inwrap > .in, :scope > .in, :scope > .ta, :scope > .sel').forEach(i => i.classList.add('err'));
                const fe = f.querySelector(':scope > .ferr');
                if (fe) { fe.hidden = false; fe.textContent = e.message; }
                const sec = f.closest('details.sec');
                if (sec) { sec.classList.add('bad'); sec.open = true; if (sec.dataset.group) this._open.add(sec.dataset.group); }
            }
            return anchored;
        }
        clearErrors() {
            this._errors = [];
            for (const r of [this.starter, this.sections]) {
                if (!r) continue;
                r.querySelectorAll('.err').forEach(i => i.classList.remove('err'));
                r.querySelectorAll('.ferr').forEach(e => { e.hidden = true; e.textContent = ''; });
                r.querySelectorAll('details.sec.bad').forEach(s => s.classList.remove('bad'));
            }
        }
        focus(path) {
            const f = this._fieldNode(path);
            if (!f) return false;
            const sec = f.closest('details.sec');
            if (sec) { sec.open = true; if (sec.dataset.group) this._open.add(sec.dataset.group); }
            f.scrollIntoView({ behavior: 'smooth', block: 'center' });
            const c = f.querySelector('input:not([type=hidden]), select, textarea, button');
            if (c) c.focus({ preventScroll: true });
            return true;
        }

        // ---------- YAML ----------

        static toYAML(obj, indent) {
            return yamlLines(obj, indent || 0).join('\n');
        }
        static parseYAML(text) {
            return parseYAML(text);
        }
    }

    // ---- YAML writer (subset: mappings, lists, scalars) ----
    function yamlScalar(v) {
        if (v === null || v === undefined) return 'null';
        if (typeof v === 'boolean' || typeof v === 'number') return String(v);
        const s = String(v);
        const plain = s !== '' && !/^[\s\-?:,\[\]{}#&*!|>'"%@`]/.test(s) && !/[:#]\s|\s[:#]|[\n\r\t]|:$/.test(s) && !/\s$/.test(s) &&
            !/^(true|false|yes|no|on|off|null|~)$/i.test(s) && !/^[+-]?(\d[\d_]*(\.\d*)?|\.\d+)([eE][+-]?\d+)?$/.test(s) && !/^0x|^0o/.test(s) && !/^[+-]?\.?(inf|nan)$/i.test(s);
        return plain ? s : JSON.stringify(s);
    }
    function yamlLines(v, indent) {
        const pad = ' '.repeat(indent);
        const out = [];
        if (Array.isArray(v)) {
            if (!v.length) return [pad + '[]'];
            for (const item of v) {
                if (isObj(item)) {
                    const inner = yamlLines(item, indent + 2);
                    if (!inner.length) { out.push(pad + '- {}'); continue; }
                    out.push(pad + '- ' + inner[0].slice(indent + 2));
                    for (let i = 1; i < inner.length; i++) out.push(inner[i]);
                } else if (Array.isArray(item)) {
                    out.push(pad + '- [' + item.map(yamlScalar).join(', ') + ']');
                } else out.push(pad + '- ' + yamlScalar(item));
            }
            return out;
        }
        if (isObj(v)) {
            for (const k of Object.keys(v)) {
                const val = v[k];
                if (val === undefined) continue;
                if (Array.isArray(val)) {
                    if (!val.length) out.push(pad + k + ': []');
                    else if (val.every(x => !isObj(x) && !Array.isArray(x))) out.push(pad + k + ': [' + val.map(yamlScalar).join(', ') + ']');
                    else { out.push(pad + k + ':'); out.push(...yamlLines(val, indent)); }
                } else if (isObj(val)) {
                    if (!Object.keys(val).length) out.push(pad + k + ': {}');
                    else { out.push(pad + k + ':'); out.push(...yamlLines(val, indent + 2)); }
                } else out.push(pad + k + ': ' + yamlScalar(val));
            }
            return out;
        }
        return [pad + yamlScalar(v)];
    }

    // ---- YAML reader (the subset the writer emits, plus comments) ----
    function stripComment(s) {
        let q = null;
        for (let i = 0; i < s.length; i++) {
            const c = s[i];
            if (q) { if (c === '\\' && q === '"') i++; else if (c === q) q = null; }
            else if (c === '"' || c === "'") q = c;
            else if (c === '#' && (i === 0 || /\s/.test(s[i - 1]))) return s.slice(0, i);
        }
        return s;
    }
    function parseScalar(s, line) {
        s = s.trim();
        if (s === '' || s === '~' || s === 'null') return null;
        if (s === 'true') return true;
        if (s === 'false') return false;
        if (s === '[]') return [];
        if (s === '{}') return {};
        if (s[0] === '"') {
            try { const v = JSON.parse(s); if (typeof v === 'string') return v; } catch (e) { /* fall through */ }
            throw new Error('line ' + line + ': unterminated or invalid quoted string');
        }
        if (s[0] === "'") {
            if (s.length < 2 || s[s.length - 1] !== "'") throw new Error('line ' + line + ': unterminated quoted string');
            return s.slice(1, -1).replace(/''/g, "'");
        }
        if (s[0] === '[') {
            if (s[s.length - 1] !== ']') throw new Error('line ' + line + ': inline list must close on the same line');
            return splitFlow(s.slice(1, -1), line).map(x => parseScalar(x, line));
        }
        if (s[0] === '{') throw new Error('line ' + line + ': inline mappings are not supported here; write one key per line');
        if (/^[+-]?(\d+(\.\d*)?|\.\d+)([eE][+-]?\d+)?$/.test(s)) return Number(s);
        return s;
    }
    function splitFlow(s, line) {
        const out = [];
        let cur = '', q = null;
        for (let i = 0; i < s.length; i++) {
            const c = s[i];
            if (q) { cur += c; if (c === '\\' && q === '"') { cur += s[++i] || ''; } else if (c === q) q = null; }
            else if (c === '"' || c === "'") { q = c; cur += c; }
            else if (c === ',') { out.push(cur); cur = ''; }
            else cur += c;
        }
        if (q) throw new Error('line ' + line + ': unterminated quoted string');
        if (cur.trim() !== '' || out.length) out.push(cur);
        return out.filter(x => x.trim() !== '');
    }
    function parseYAML(text) {
        const lines = [];
        String(text || '').split(/\r?\n/).forEach((raw, i) => {
            if (raw.indexOf('\t') >= 0 && /^\s*\t/.test(raw)) throw new Error('line ' + (i + 1) + ': tabs are not allowed for indentation');
            const s = stripComment(raw);
            if (s.trim() === '') return;
            lines.push({ n: i + 1, indent: s.match(/^ */)[0].length, text: s.trim() });
        });
        let pos = 0;
        function parseNode(indent) {
            if (pos >= lines.length) return null;
            const l = lines[pos];
            if (l.indent < indent) return null;
            if (l.text.startsWith('- ') || l.text === '-') return parseList(l.indent);
            return parseMap(l.indent);
        }
        function keyOf(text, n) {
            const m = text.match(/^("(?:[^"\\]|\\.)*"|'(?:[^']|'')*'|[^\s:][^:]*?)\s*:(?:\s+(.*))?$/);
            if (!m) return null;
            let k = m[1];
            if (k[0] === '"' || k[0] === "'") k = parseScalar(k, n);
            return { key: String(k), rest: m[2] === undefined ? '' : m[2] };
        }
        function parseMap(indent) {
            const obj = {};
            while (pos < lines.length) {
                const l = lines[pos];
                if (l.indent < indent) break;
                if (l.indent > indent) throw new Error('line ' + l.n + ': unexpected indentation');
                if (l.text.startsWith('- ')) throw new Error('line ' + l.n + ': a list item where a key was expected');
                const kv = keyOf(l.text, l.n);
                if (!kv) throw new Error('line ' + l.n + ': expected "key: value"');
                pos++;
                if (kv.rest !== '') { obj[kv.key] = parseScalar(kv.rest, l.n); continue; }
                const next = lines[pos];
                if (next && (next.indent > indent || (next.indent === indent && next.text.startsWith('- ')))) obj[kv.key] = parseNode(next.indent);
                else obj[kv.key] = null;
            }
            return obj;
        }
        function parseList(indent) {
            const arr = [];
            while (pos < lines.length) {
                const l = lines[pos];
                if (l.indent < indent) break;
                if (l.indent > indent) throw new Error('line ' + l.n + ': unexpected indentation');
                if (!(l.text.startsWith('- ') || l.text === '-')) break;
                const body = l.text === '-' ? '' : l.text.slice(2).trim();
                pos++;
                if (body === '') { const next = lines[pos]; arr.push(next && next.indent > indent ? parseNode(next.indent) : null); continue; }
                const kv = keyOf(body, l.n);
                if (kv && !/^["'\[]/.test(body)) {
                    // "- key: value" opens a mapping whose other keys sit at indent + 2.
                    const inner = indent + 2;
                    lines.splice(pos, 0, { n: l.n, indent: inner, text: body });
                    arr.push(parseMap(inner));
                } else arr.push(parseScalar(body, l.n));
            }
            return arr;
        }
        const v = parseNode(lines.length ? lines[0].indent : 0);
        if (pos < lines.length) throw new Error('line ' + lines[pos].n + ': unexpected content');
        return v;
    }

    SchemaForm.GROUP_LABELS = GROUP_LABELS;
    SchemaForm.esc = esc;
    SchemaForm.isStored = isStored;
    SchemaForm.titleCase = titleCase;
    global.SchemaForm = SchemaForm;
})(window);

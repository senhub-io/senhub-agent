// SenHub Agent console: the header every page shares.
//
// The markup of the header is static in each page; this script fills the
// agent identity on its right (host, state, version, uptime) from
// /info/system, so the operator sees "it runs" without visiting Overview.
(function () {
    const meta = document.querySelector('meta[name="agent-key"]');
    const key = (window.AGENT_KEY || (meta && meta.content) || '').trim();
    if (!key) return;
    const box = document.querySelector('.hdr-status');
    if (!box) return;

    function esc(s) { return String(s).replace(/[&<>"']/g, c => ({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c])); }
    // /info/system reports uptime as a Go duration string ("26h3m12.5s");
    // keep the two largest units.
    function uptime(s) {
        const m = String(s || '').match(/^(?:(\d+)h)?(?:(\d+)m)?(?:([\d.]+)s)?$/);
        if (!m) return String(s || '');
        const h = parseInt(m[1] || '0', 10), mi = parseInt(m[2] || '0', 10), sec = Math.floor(parseFloat(m[3] || '0'));
        if (h >= 24) return Math.floor(h / 24) + 'd ' + (h % 24) + 'h';
        if (h) return h + 'h ' + mi + 'm';
        if (mi) return mi + 'm';
        return sec + 's';
    }
    async function refresh() {
        try {
            const r = await fetch('/api/' + key + '/info/system');
            if (!r.ok) throw new Error('HTTP ' + r.status);
            const d = await r.json();
            const failing = (d.outputs_failing || []).length > 0;
            const state = failing ? 'warn' : 'ok';
            const label = failing ? 'output failing' : 'running';
            const version = String(d.version || '?');
            box.innerHTML = '<span class="host">' + esc(d.hostname || d.host || '') + '</span>' +
                '<span class="pill ' + state + '"><span class="dot ' + state + '"></span>' + label + '</span>' +
                '<span>' + (/^\d/.test(version) ? 'v' : '') + esc(version) + '</span>' +
                '<span title="uptime">up ' + uptime(d.uptime) + '</span>';
        } catch (e) {
            box.innerHTML = '<span class="pill err"><span class="dot err"></span>unreachable</span>';
        }
    }
    refresh();
    setInterval(refresh, 30000);
})();

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

// confirmDestructive asks in the page, in the console's own style, and
// resolves true only when the operator presses the destructive button.
//
// It replaces window.confirm(), which was the one interaction that left
// the page's style: it cannot be styled, it blocks the renderer, and
// after a few uses Chrome offers the viewer a "prevent this page from
// creating more dialogs" checkbox, after which confirm() always returns
// false and deleting silently stops working with nothing said on screen.
//
// Keyboard: Escape cancels, Enter confirms, and focus starts on Cancel so
// a stray Enter does not delete anything.
function confirmDestructive({ title, body, confirmLabel = 'Delete', cancelLabel = 'Cancel' }) {
    return new Promise((resolve) => {
        const back = document.createElement('div');
        back.className = 'sh-confirm-back';
        back.innerHTML =
            '<div class="sh-confirm card" role="alertdialog" aria-modal="true">' +
            '<h2 class="sh-confirm-t"></h2>' +
            '<p class="sh-confirm-b small"></p>' +
            '<div class="row sh-confirm-a">' +
            '<button type="button" class="btn" data-act="cancel"></button>' +
            '<button type="button" class="btn danger" data-act="ok"></button>' +
            '</div></div>';
        back.querySelector('.sh-confirm-t').textContent = title;
        back.querySelector('.sh-confirm-b').textContent = body || '';
        const cancel = back.querySelector('[data-act="cancel"]');
        const ok = back.querySelector('[data-act="ok"]');
        cancel.textContent = cancelLabel;
        ok.textContent = confirmLabel;

        const previous = document.activeElement;
        const close = (answer) => {
            document.removeEventListener('keydown', onKey, true);
            back.remove();
            if (previous && previous.focus) previous.focus();
            resolve(answer);
        };
        const onKey = (ev) => {
            if (ev.key === 'Escape') { ev.preventDefault(); close(false); }
            else if (ev.key === 'Enter') { ev.preventDefault(); close(true); }
        };
        cancel.addEventListener('click', () => close(false));
        ok.addEventListener('click', () => close(true));
        back.addEventListener('mousedown', (ev) => { if (ev.target === back) close(false); });
        document.addEventListener('keydown', onKey, true);
        document.body.appendChild(back);
        cancel.focus();
    });
}

window.confirmDestructive = confirmDestructive;

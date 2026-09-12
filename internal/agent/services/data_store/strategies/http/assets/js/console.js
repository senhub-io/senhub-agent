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

// A picture per category, not per vendor. Sixty-six types would mean as
// many third-party marks to obtain, keep current and redistribute, and
// the reader does not need to recognise a brand to find the card they
// came for: they need to see at a glance that this one is a database
// and that one is a queue. Drawn here, in one stroke weight, so every
// card carries one and none of them is missing.
const GLYPH = {
  database: '<path d="M4 6c0-1.1 3.6-2 8-2s8 .9 8 2-3.6 2-8 2-8-.9-8-2z"/><path d="M4 6v12c0 1.1 3.6 2 8 2s8-.9 8-2V6"/><path d="M4 12c0 1.1 3.6 2 8 2s8-.9 8-2"/>',
  host: '<rect x="3" y="4" width="18" height="7" rx="1.5"/><rect x="3" y="13" width="18" height="7" rx="1.5"/><path d="M7 7.5h.01M7 16.5h.01"/>',
  web: '<circle cx="12" cy="12" r="8.5"/><path d="M3.5 12h17"/><path d="M12 3.5c2.3 2.4 3.4 5.4 3.4 8.5s-1.1 6.1-3.4 8.5c-2.3-2.4-3.4-5.4-3.4-8.5S9.7 5.9 12 3.5z"/>',
  network: '<circle cx="12" cy="5" r="2.2"/><circle cx="5" cy="18" r="2.2"/><circle cx="19" cy="18" r="2.2"/><path d="M12 7.2v4.3M12 11.5 6.2 16.4M12 11.5l5.8 4.9"/>',
  messaging: '<rect x="3" y="5" width="18" height="13" rx="2"/><path d="m3.8 6.5 8.2 6 8.2-6"/>',
  integration: '<path d="M9 7V4.5a2 2 0 0 1 4 0V7"/><rect x="6" y="7" width="10" height="6" rx="1.5"/><path d="M11 13v3a3.5 3.5 0 0 0 3.5 3.5H18"/>',
  logs: '<rect x="4" y="3" width="16" height="18" rx="2"/><path d="M8 8h8M8 12h8M8 16h5"/>',
  containers: '<path d="m12 3 8 4.5v9L12 21l-8-4.5v-9L12 3z"/><path d="M4 7.5 12 12l8-4.5M12 12v9"/>',
  virtualization: '<path d="m12 3 8 4-8 4-8-4 8-4z"/><path d="m4 12 8 4 8-4"/><path d="m4 17 8 4 8-4"/>',
  storage: '<rect x="3" y="4" width="18" height="16" rx="2"/><circle cx="12" cy="12" r="3.2"/><path d="M12 8.8V4.5M17.5 17.5l-2.8-2.8"/>',
  custom: '<path d="M14.5 3.5a5 5 0 0 0-6 6.4L3.8 14.6a2 2 0 0 0 2.8 2.8l4.7-4.7a5 5 0 0 0 6.4-6l-3 3-2.4-2.4 3-3z"/>',
  other: '<circle cx="12" cy="12" r="8.5"/><path d="M12 8v4.5M12 16h.01"/>'
};
function glyphFor(category) {
  return '<svg class="tico" viewBox="0 0 24 24" aria-hidden="true" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round">' + (GLYPH[category] || GLYPH.other) + '</svg>';
}

window.glyphFor = glyphFor;

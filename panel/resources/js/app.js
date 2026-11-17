// Theme: light / dark / system. Applied before paint by the inline script in the layout;
// this only wires the toggle. Storage may be blocked, so every access is guarded.
window.hpTheme = {
    get() { try { return localStorage.getItem('hp-theme') || 'system'; } catch { return 'system'; } },
    set(v) {
        try { v === 'system' ? localStorage.removeItem('hp-theme') : localStorage.setItem('hp-theme', v); } catch {}
        v === 'system' ? document.documentElement.removeAttribute('data-theme') : document.documentElement.setAttribute('data-theme', v);
    },
};

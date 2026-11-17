export function bytes(n: number): string {
    if (!Number.isFinite(n)) return '–';
    const u = ['B', 'KB', 'MB', 'GB', 'TB'];
    let i = 0;
    while (n >= 1024 && i < u.length - 1) {
        n /= 1024;
        i++;
    }
    return `${i ? n.toFixed(1) : Math.round(n)} ${u[i]}`;
}

export function ago(iso?: string | null): string {
    if (!iso || iso.startsWith('0001')) return 'never';
    const s = Math.round((Date.now() - new Date(iso).getTime()) / 1000);
    const rtf = new Intl.RelativeTimeFormat(undefined, { numeric: 'auto' });
    if (s < 60) return rtf.format(-s, 'second');
    if (s < 3600) return rtf.format(-Math.round(s / 60), 'minute');
    if (s < 86400) return rtf.format(-Math.round(s / 3600), 'hour');
    return rtf.format(-Math.round(s / 86400), 'day');
}

export function when(iso?: string | null): string {
    return iso && !iso.startsWith('0001') ? new Date(iso).toLocaleString() : '–';
}

export function duration(sec: number): string {
    const d = Math.floor(sec / 86400);
    const h = Math.floor((sec % 86400) / 3600);
    const m = Math.floor((sec % 3600) / 60);
    return d ? `${d}d ${h}h` : h ? `${h}h ${m}m` : `${m}m`;
}

export function rate(n: number): string {
    return `${bytes(n)}/s`;
}

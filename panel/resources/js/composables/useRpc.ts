export interface RpcResult<T = any> {
    ok: boolean;
    data: T;
    error: string | null;
    unavailable?: boolean;
    extra?: Record<string, any>;
}

function xsrf(): string {
    const m = document.cookie.match(/(?:^|; )XSRF-TOKEN=([^;]+)/);
    return m ? decodeURIComponent(m[1]) : '';
}

async function post(url: string, body: BodyInit, json = true): Promise<Response> {
    const headers: Record<string, string> = { Accept: 'application/json', 'X-XSRF-TOKEN': xsrf() };
    if (json) headers['Content-Type'] = 'application/json';
    return fetch(url, { method: 'POST', headers, body, credentials: 'same-origin' });
}

/** Call a Service operation through the Panel. Never throws: network and server problems become `ok: false`. */
export async function rpc<T = any>(service: 'core' | 'db' | 'backup' | 'mail' | 'system', method: string, ...args: unknown[]): Promise<RpcResult<T>> {
    try {
        const r = await post('/rpc', JSON.stringify({ service, method, args }));
        if (r.status === 401 || r.status === 419) {
            window.location.reload();
        }
        return (await r.json()) as RpcResult<T>;
    } catch {
        return { ok: false, data: null as T, error: 'The panel could not be reached. Check your connection.' };
    }
}

export async function upload(domain: string, path: string, file: File): Promise<RpcResult> {
    const fd = new FormData();
    fd.append('file', file);
    fd.append('path', path);
    try {
        const r = await post(`/sites/${encodeURIComponent(domain)}/upload`, fd, false);
        if (r.status === 422) {
            const j = await r.json();
            return { ok: false, data: null, error: j.message ?? 'The file was rejected.' };
        }
        return (await r.json()) as RpcResult;
    } catch {
        return { ok: false, data: null, error: 'Upload failed. Check your connection.' };
    }
}

export async function postJson(url: string, body: unknown): Promise<{ status: number; json: any }> {
    try {
        const r = await post(url, JSON.stringify(body));
        return { status: r.status, json: await r.json().catch(() => ({})) };
    } catch {
        return { status: 0, json: { error: 'The panel could not be reached.' } };
    }
}

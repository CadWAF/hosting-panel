import { ref } from 'vue';
import { toast } from 'vue-sonner';
import type { RpcResult } from './useRpc';

/** Await an RPC call, toasting failures (and optionally success). */
export async function act<T>(call: Promise<RpcResult<T>>, success?: string): Promise<RpcResult<T>> {
    const r = await call;
    if (!r.ok) toast.error(r.error ?? 'Something went wrong.');
    else if (success) toast.success(success);
    return r;
}

/** Tracks an in-flight operation so buttons can disable themselves. */
export function useBusy() {
    const busy = ref(false);
    async function run<T>(fn: () => Promise<T>): Promise<T> {
        busy.value = true;
        try {
            return await fn();
        } finally {
            busy.value = false;
        }
    }
    return { busy, run };
}

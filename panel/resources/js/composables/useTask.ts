import { onBeforeUnmount, ref, watch } from 'vue';
import { rpc } from './useRpc';

export interface TaskInfo {
    id: string;
    kind: string;
    title: string;
    status: 'running' | 'done' | 'failed' | 'interrupted';
    log: string;
}

/** Follow a long-running Task until it finishes; calls `onDone` once with the final state. */
export function useTask(service: 'core' | 'db' | 'backup' | 'mail', onDone?: (t: TaskInfo) => void) {
    const task = ref<TaskInfo | null>(null);
    let timer: ReturnType<typeof setTimeout> | undefined;
    let current = '';

    async function tick() {
        const r = await rpc<TaskInfo>(service, 'task', current);
        if (r.ok && r.data) {
            task.value = r.data;
            if (r.data.status !== 'running') {
                onDone?.(r.data);
                return;
            }
        }
        timer = setTimeout(tick, 2000);
    }

    function follow(id: string | null | undefined) {
        clearTimeout(timer);
        task.value = null;
        if (!id) return;
        current = id;
        tick();
    }

    function dismiss() {
        clearTimeout(timer);
        task.value = null;
    }

    onBeforeUnmount(() => clearTimeout(timer));
    return { task, follow, dismiss };
}

export function useInterval(fn: () => void, ms: number) {
    const id = ref<ReturnType<typeof setInterval>>();
    const start = () => {
        id.value = setInterval(() => {
            if (!document.hidden) fn();
        }, ms);
    };
    start();
    onBeforeUnmount(() => clearInterval(id.value));
    watch(() => ms, () => {});
}

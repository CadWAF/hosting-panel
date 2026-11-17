<script setup lang="ts">
import { onMounted, ref, watch } from 'vue';
import { RefreshCwIcon } from '@lucide/vue';
import { Button } from '@/components/ui/button';
import { Card } from '@/components/ui/card';
import { Skeleton } from '@/components/ui/skeleton';
import { ToggleGroup, ToggleGroupItem } from '@/components/ui/toggle-group';
import { rpc } from '@/composables/useRpc';

const props = defineProps<{ domain: string }>();
const name = ref('error');
const text = ref<string | null>(null);
async function load() {
    text.value = null;
    const r = await rpc<string>('core', 'log', props.domain, name.value);
    text.value = r.ok ? String(r.data ?? '') : (r.error ?? 'Could not load the log.');
}
onMounted(load);
watch(name, load);
</script>

<template>
    <div class="flex flex-col gap-3">
        <div class="flex flex-wrap items-center justify-between gap-2">
            <ToggleGroup v-model="name" type="single" variant="outline" size="sm" aria-label="Log">
                <ToggleGroupItem value="error">nginx error</ToggleGroupItem>
                <ToggleGroupItem value="access">access</ToggleGroupItem>
                <ToggleGroupItem value="php">PHP</ToggleGroupItem>
                <ToggleGroupItem value="apache">Apache</ToggleGroupItem>
            </ToggleGroup>
            <Button variant="outline" size="sm" @click="load"><RefreshCwIcon /> Refresh</Button>
        </div>
        <Card class="p-0"><Skeleton v-if="text === null" class="m-4 h-40" /><pre v-else class="max-h-[60svh] overflow-auto p-4 text-xs whitespace-pre-wrap">{{ text || 'Empty' }}</pre></Card>
    </div>
</template>

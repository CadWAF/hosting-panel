<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue';
import { Head } from '@inertiajs/vue3';
import { DownloadIcon, RefreshCwIcon, SearchIcon } from '@lucide/vue';
import PageHeader from '@/components/app/PageHeader.vue';
import ServiceDown from '@/components/app/ServiceDown.vue';
import { Button } from '@/components/ui/button';
import { Card } from '@/components/ui/card';
import { Input } from '@/components/ui/input';
import { Select, SelectContent, SelectGroup, SelectItem, SelectLabel, SelectTrigger, SelectValue } from '@/components/ui/select';
import { Switch } from '@/components/ui/switch';
import { useI18n } from '@/composables/useI18n';
import { rpc } from '@/composables/useRpc';
import { bytes } from '@/lib/format';

interface Source { id: string; label: string; group: string; size: number }
const props = defineProps<{ sources: Source[]; units: string[]; available: boolean }>();
const { t } = useI18n();

const groups = computed(() => {
    const g: Record<string, Source[]> = {};
    for (const s of props.sources) (g[s.group] ??= []).push(s);
    return g;
});
const current = ref(props.sources[0]?.id ?? (props.units[0] ? `unit:${props.units[0]}` : ''));
const lines = ref('300');
const q = ref('');
const text = ref('');
const loading = ref(false);
const follow = ref(false);
const pre = ref<HTMLElement | null>(null);
let timer: ReturnType<typeof setInterval> | undefined;

async function load() {
    if (!current.value) return;
    loading.value = true;
    const r = await rpc<string>('system', 'readLog', current.value, Number(lines.value), q.value.trim());
    loading.value = false;
    text.value = r.ok ? r.data || '' : (r.error ?? '');
    requestAnimationFrame(() => { if (pre.value) pre.value.scrollTop = pre.value.scrollHeight; });
}
let debounce: ReturnType<typeof setTimeout> | undefined;
watch([current, lines], load, { immediate: true });
watch(q, () => { clearTimeout(debounce); debounce = setTimeout(load, 350); });
watch(follow, (on) => { clearInterval(timer); if (on) timer = setInterval(load, 3000); });
onBeforeUnmount(() => { clearInterval(timer); clearTimeout(debounce); });

const shown = computed(() => (text.value || '').split('\n'));
const tone = (l: string) => (/\b(error|fail(ed|ure)?|fatal|critical|denied|refused)\b/i.test(l) ? 'text-destructive' : /\bwarn(ing)?\b/i.test(l) ? 'text-warning' : '');
function save() {
    const a = document.createElement('a');
    a.href = URL.createObjectURL(new Blob([text.value], { type: 'text/plain' }));
    a.download = `${current.value.replace(/[^a-z0-9.-]+/gi, '_')}.log`;
    a.click();
    URL.revokeObjectURL(a.href);
}
</script>

<template>
    <Head :title="t('Logs')" />
    <PageHeader :title="t('Logs')" :description="t('Read system, web, security and service logs without logging in over SSH.')" />
    <ServiceDown v-if="!available" name="System" />

    <template v-if="available">
        <div class="flex flex-wrap items-end gap-3">
            <label class="grid min-w-56 gap-1.5 text-sm font-medium">{{ t('Log') }}
                <Select v-model="current"><SelectTrigger class="w-full"><SelectValue :placeholder="t('Choose a log')" /></SelectTrigger>
                    <SelectContent>
                        <SelectGroup v-for="(items, g) in groups" :key="g"><SelectLabel>{{ g }}</SelectLabel><SelectItem v-for="s in items" :key="s.id" :value="s.id">{{ s.label }} · {{ bytes(s.size) }}</SelectItem></SelectGroup>
                        <SelectGroup v-if="units.length"><SelectLabel>{{ t('Service journals') }}</SelectLabel><SelectItem v-for="u in units" :key="u" :value="`unit:${u}`">{{ u }}</SelectItem></SelectGroup>
                    </SelectContent></Select>
            </label>
            <label class="grid w-32 gap-1.5 text-sm font-medium">{{ t('Lines') }}
                <Select v-model="lines"><SelectTrigger class="w-full"><SelectValue /></SelectTrigger><SelectContent><SelectItem v-for="n in ['100', '300', '1000', '2000']" :key="n" :value="n">{{ n }}</SelectItem></SelectContent></Select></label>
            <label class="grid min-w-48 flex-1 gap-1.5 text-sm font-medium">{{ t('Filter') }}
                <div class="relative"><SearchIcon class="text-muted-foreground pointer-events-none absolute top-1/2 left-2.5 size-4 -translate-y-1/2" /><Input v-model="q" class="pl-8" :placeholder="t('Only lines containing…')" autocomplete="off" /></div></label>
            <label class="flex items-center gap-2 pb-2 text-sm font-medium"><Switch v-model="follow" /> {{ t('Follow') }}</label>
            <Button variant="outline" :disabled="loading" @click="load"><RefreshCwIcon :class="{ 'animate-spin': loading }" /> {{ t('Refresh') }}</Button>
            <Button variant="outline" :disabled="!text" @click="save"><DownloadIcon /> {{ t('Save') }}</Button>
        </div>
        <Card class="gap-0 overflow-hidden py-0">
            <pre ref="pre" class="max-h-[60vh] min-h-64 overflow-auto p-4 text-xs leading-relaxed" aria-live="off"><template v-if="text"><span v-for="(l, i) in shown" :key="i" class="block break-all whitespace-pre-wrap" :class="tone(l)">{{ l }}</span></template><span v-else class="text-muted-foreground">{{ t('This log is empty.') }}</span></pre>
        </Card>
    </template>
</template>

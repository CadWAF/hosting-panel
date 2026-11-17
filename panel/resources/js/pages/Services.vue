<script setup lang="ts">
import { ref } from 'vue';
import { Head, router } from '@inertiajs/vue3';
import { LockIcon, PlayIcon, RotateCwIcon, ScrollTextIcon, SquareIcon } from '@lucide/vue';
import PageHeader from '@/components/app/PageHeader.vue';
import StatusBadge from '@/components/app/StatusBadge.vue';
import { Button } from '@/components/ui/button';
import { Card, CardContent, CardDescription, CardHeader, CardTitle, CardAction } from '@/components/ui/card';
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from '@/components/ui/dialog';
import { Switch } from '@/components/ui/switch';
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table';
import { act, useBusy } from '@/composables/useActions';
import { useI18n } from '@/composables/useI18n';
import { rpc } from '@/composables/useRpc';
import { useInterval } from '@/composables/useTask';

interface Unit { unit: string; description: string; active: string; sub: string; enabled: boolean; protected: boolean }
const props = defineProps<{ services: { name: string; up: boolean; note: string }[]; checks: { name: string; up: boolean; last_error?: string }[]; units: Unit[]; all: boolean }>();
const { t } = useI18n();
const { busy, run } = useBusy();
useInterval(() => router.reload({ only: ['services', 'checks', 'units'], preserveScroll: true }), 10000);
const reload = () => router.reload({ only: ['units', 'services'], preserveScroll: true });

async function action(u: Unit, a: string) {
    const r = await run(() => act(rpc('system', 'serviceAction', u.unit, a)));
    if (r.ok) reload();
}
const showAll = (v: boolean) => router.get('/services', v ? { all: 1 } : {}, { preserveScroll: true, preserveState: true, only: ['units', 'all'] });
const logOpen = ref(false);
const logOf = ref('');
const logText = ref('');
async function showLog(u: Unit) {
    logOf.value = u.unit; logText.value = '…'; logOpen.value = true;
    const r = await act(rpc('system', 'readLog', `unit:${u.unit}`, 200, ''));
    logText.value = r.ok ? r.data || t('No output.') : '';
}
const tone = (a: string) => (a === 'active' ? 'running' : a === 'failed' ? 'failed' : 'off');
</script>

<template>
    <Head :title="t('Services')" />
    <PageHeader :title="t('Services')" :description="t('Panel components and the services running on this server.')" />

    <section class="grid gap-3 sm:grid-cols-2 lg:grid-cols-3" :aria-label="t('Panel components')">
        <Card v-for="s in services" :key="s.name" class="gap-1 py-4">
            <CardHeader class="px-5"><CardTitle>{{ s.name }}</CardTitle>
                <CardAction><StatusBadge :status="s.up ? 'running' : 'down'" :labels="{ running: t('Running'), down: t('Unavailable') }" /></CardAction>
                <CardDescription>{{ t(s.note) }}</CardDescription></CardHeader>
        </Card>
    </section>

    <Card class="gap-0 py-0">
        <CardHeader class="border-b px-5 py-4"><CardTitle>{{ t('System services') }}</CardTitle>
            <CardAction><label class="text-muted-foreground flex items-center gap-2 text-sm font-normal"><Switch :model-value="all" :aria-label="t('Show all services')" @update:model-value="showAll" /> {{ t('Show all') }}</label></CardAction></CardHeader>
        <Table>
            <TableHeader><TableRow><TableHead class="px-5">{{ t('Service') }}</TableHead><TableHead>{{ t('Status') }}</TableHead><TableHead>{{ t('Start at boot') }}</TableHead><TableHead class="px-5 text-right">{{ t('Actions') }}</TableHead></TableRow></TableHeader>
            <TableBody>
                <TableRow v-for="u in units" :key="u.unit">
                    <TableCell class="px-5"><p class="font-mono text-sm font-medium">{{ u.unit.replace('.service', '') }}</p><p class="text-muted-foreground max-w-md truncate text-xs">{{ u.description }}</p></TableCell>
                    <TableCell><StatusBadge :status="tone(u.active)" :labels="{ running: t('Running'), failed: t('Failed'), off: t('Stopped') }" /></TableCell>
                    <TableCell><Switch :model-value="u.enabled" :disabled="busy || u.protected" :aria-label="`${t('Start at boot')} ${u.unit}`" @update:model-value="(v: boolean) => action(u, v ? 'enable' : 'disable')" /></TableCell>
                    <TableCell class="px-5"><div class="flex items-center justify-end gap-1">
                        <LockIcon v-if="u.protected" class="text-muted-foreground mr-1 size-3.5" :title="t('Needed to reach this panel; cannot be stopped here.')" />
                        <Button v-if="u.active !== 'active'" variant="outline" size="icon-sm" :aria-label="t('Start')" :title="t('Start')" :disabled="busy" @click="action(u, 'start')"><PlayIcon /></Button>
                        <Button v-else-if="!u.protected" variant="outline" size="icon-sm" :aria-label="t('Stop')" :title="t('Stop')" :disabled="busy" @click="action(u, 'stop')"><SquareIcon /></Button>
                        <Button variant="outline" size="icon-sm" :aria-label="t('Restart')" :title="t('Restart')" :disabled="busy || u.active !== 'active'" @click="action(u, 'restart')"><RotateCwIcon /></Button>
                        <Button variant="outline" size="icon-sm" :aria-label="t('Logs')" :title="t('Logs')" @click="showLog(u)"><ScrollTextIcon /></Button>
                    </div></TableCell>
                </TableRow>
            </TableBody>
        </Table>
    </Card>

    <template v-if="checks.length">
        <h2 class="text-lg font-semibold">{{ t('Monitored by Notifier') }}</h2>
        <Card class="gap-0 py-0">
            <ul class="divide-y">
                <li v-for="c in checks" :key="c.name" class="flex items-center justify-between gap-3 px-5 py-3">
                    <span>{{ c.name }}<span v-if="!c.up && c.last_error" class="text-muted-foreground text-sm"> · {{ c.last_error }}</span></span>
                    <StatusBadge :status="c.up ? 'up' : 'down'" :labels="{ up: t('Up'), down: t('Down') }" />
                </li>
            </ul>
        </Card>
    </template>

    <Dialog v-model:open="logOpen">
        <DialogContent class="sm:max-w-3xl">
            <DialogHeader><DialogTitle>{{ logOf }}</DialogTitle><DialogDescription>{{ t('Last 200 journal lines') }}</DialogDescription></DialogHeader>
            <pre class="bg-muted max-h-96 overflow-auto rounded-md p-3 text-xs whitespace-pre-wrap">{{ logText }}</pre>
        </DialogContent>
    </Dialog>
</template>

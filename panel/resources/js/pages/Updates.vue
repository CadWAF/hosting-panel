<script setup lang="ts">
import { computed, ref } from 'vue';
import { Head, router } from '@inertiajs/vue3';
import { CircleCheckIcon, DownloadIcon, PowerIcon, RefreshCwIcon, ShieldAlertIcon } from '@lucide/vue';
import ConfirmDialog from '@/components/app/ConfirmDialog.vue';
import PageHeader from '@/components/app/PageHeader.vue';
import ServiceDown from '@/components/app/ServiceDown.vue';
import TaskPanel from '@/components/app/TaskPanel.vue';
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table';
import { act } from '@/composables/useActions';
import { useI18n } from '@/composables/useI18n';
import { rpc } from '@/composables/useRpc';
import { useTask } from '@/composables/useTask';
import { ago } from '@/lib/format';

interface Up { name: string; version: string; current: string; security: boolean }
interface Updates { upgradable: Up[]; security: number; reboot_required: boolean; last_checked: number; auto_updates: boolean }
const props = defineProps<{ updates: Updates | null; available: boolean }>();
const { t } = useI18n();
const { task, follow, dismiss } = useTask('system', () => router.reload({ only: ['updates'], preserveScroll: true }));
const running = computed(() => task.value?.status === 'running');
const checked = computed(() => (props.updates?.last_checked ? ago(new Date(props.updates.last_checked * 1000).toISOString()) : t('never')));

async function start(method: string) {
    const r = await act(rpc('system', method));
    if (r.ok) follow(r.data?.id);
}
const rebootOpen = ref(false);
async function reboot() {
    const r = await act(rpc('system', 'reboot'), t('Rebooting. The panel will be back in a minute.'));
    return r;
}
</script>

<template>
    <Head :title="t('Updates')" />
    <PageHeader :title="t('Updates')" :description="t('Operating system packages and security patches.')">
        <Button variant="outline" :disabled="running" @click="start('checkUpdates')"><RefreshCwIcon /> {{ t('Check now') }}</Button>
        <Button :disabled="running || !updates?.upgradable.length" @click="start('applyUpdates')"><DownloadIcon /> {{ t('Install all updates') }}</Button>
    </PageHeader>
    <ServiceDown v-if="!available" name="System" />
    <TaskPanel :task="task" @dismiss="dismiss" />

    <Alert v-if="updates?.reboot_required" role="status"><PowerIcon /><AlertTitle>{{ t('A restart is needed') }}</AlertTitle>
        <AlertDescription><p>{{ t('Updates were installed that only take effect after a reboot (usually a new kernel).') }}</p><Button class="mt-2" size="sm" variant="outline" @click="rebootOpen = true">{{ t('Restart the server') }}</Button></AlertDescription></Alert>

    <template v-if="updates">
        <section class="grid gap-4 sm:grid-cols-3">
            <Card class="py-4"><CardContent class="px-5"><p class="text-muted-foreground text-sm">{{ t('Available updates') }}</p><p class="tabular text-2xl font-semibold">{{ updates.upgradable.length }}</p></CardContent></Card>
            <Card class="py-4"><CardContent class="px-5"><p class="text-muted-foreground flex items-center gap-1.5 text-sm"><ShieldAlertIcon class="size-3.5" /> {{ t('Security updates') }}</p><p class="tabular text-2xl font-semibold" :class="{ 'text-destructive': updates.security }">{{ updates.security }}</p></CardContent></Card>
            <Card class="py-4"><CardContent class="px-5"><p class="text-muted-foreground text-sm">{{ t('Last checked') }}</p><p class="text-2xl font-semibold">{{ checked }}</p>
                <p class="text-muted-foreground text-xs">{{ updates.auto_updates ? t('Security updates install automatically.') : t('Automatic updates are off (see Software).') }}</p></CardContent></Card>
        </section>

        <Card v-if="!updates.upgradable.length" class="py-8"><CardContent class="flex flex-col items-center gap-2 px-5 text-center"><CircleCheckIcon class="text-success size-8" /><p class="font-semibold">{{ t('Everything is up to date') }}</p></CardContent></Card>
        <Card v-else class="gap-0 py-0">
            <CardHeader class="border-b px-5 py-4"><CardTitle>{{ t('Packages') }}</CardTitle></CardHeader>
            <Table>
                <TableHeader><TableRow><TableHead class="px-5">{{ t('Package') }}</TableHead><TableHead>{{ t('Installed') }}</TableHead><TableHead>{{ t('New version') }}</TableHead><TableHead class="px-5 text-right" /></TableRow></TableHeader>
                <TableBody>
                    <TableRow v-for="u in updates.upgradable" :key="u.name">
                        <TableCell class="px-5 font-mono text-sm font-medium">{{ u.name }}</TableCell>
                        <TableCell class="text-muted-foreground font-mono text-xs">{{ u.current }}</TableCell>
                        <TableCell class="font-mono text-xs">{{ u.version }}</TableCell>
                        <TableCell class="px-5 text-right"><Badge v-if="u.security" variant="outline" class="border-destructive/30 bg-destructive/10 text-destructive">{{ t('Security') }}</Badge></TableCell>
                    </TableRow>
                </TableBody>
            </Table>
        </Card>
    </template>
    <ConfirmDialog v-model:open="rebootOpen" :title="t('Restart the server?')" :description="t('Sites, mail and this panel will be unavailable for about a minute.')" :confirm-label="t('Restart')" @confirm="reboot" />
</template>

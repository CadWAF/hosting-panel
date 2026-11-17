<script setup lang="ts">
import { reactive, ref } from 'vue';
import { Head, router } from '@inertiajs/vue3';
import { CalendarClockIcon, PencilIcon, PlayIcon, PlusIcon, Trash2Icon } from '@lucide/vue';
import ConfirmDialog from '@/components/app/ConfirmDialog.vue';
import EmptyState from '@/components/app/EmptyState.vue';
import PageHeader from '@/components/app/PageHeader.vue';
import ServiceDown from '@/components/app/ServiceDown.vue';
import TaskPanel from '@/components/app/TaskPanel.vue';
import { Button } from '@/components/ui/button';
import { Card } from '@/components/ui/card';
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog';
import { Input } from '@/components/ui/input';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select';
import { Switch } from '@/components/ui/switch';
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table';
import { act, useBusy } from '@/composables/useActions';
import { useI18n } from '@/composables/useI18n';
import { rpc } from '@/composables/useRpc';
import { useTask } from '@/composables/useTask';

interface Job { id: string; name: string; schedule: string; user: string; command: string; enabled: boolean }
defineProps<{ jobs: Job[]; available: boolean }>();
const { t } = useI18n();
const { busy, run } = useBusy();
const reload = () => router.reload({ only: ['jobs'], preserveScroll: true });
const { task, follow, dismiss } = useTask('system', () => reload());

const presets = [
    { label: 'Every minute', v: '* * * * *' }, { label: 'Every 5 minutes', v: '*/5 * * * *' }, { label: 'Hourly', v: '0 * * * *' },
    { label: 'Daily at 03:00', v: '0 3 * * *' }, { label: 'Weekly (Monday 04:00)', v: '0 4 * * 1' }, { label: 'Monthly (1st, 05:00)', v: '0 5 1 * *' }, { label: 'At boot', v: '@reboot' },
];
const open = ref(false);
const editing = ref<string | null>(null);
const form = reactive({ name: '', schedule: '0 3 * * *', user: 'root', command: '', enabled: true });
function openNew() { editing.value = null; Object.assign(form, { name: '', schedule: '0 3 * * *', user: 'root', command: '', enabled: true }); open.value = true; }
function openEdit(j: Job) { editing.value = j.id; Object.assign(form, j); open.value = true; }
async function save() {
    const body = { ...form, name: form.name.trim(), command: form.command.trim(), schedule: form.schedule.trim(), user: form.user.trim() || 'root' };
    const r = await run(() => (editing.value ? act(rpc('system', 'updateCron', editing.value, body), t('Saved')) : act(rpc('system', 'addCron', body), t('Job added'))));
    if (r.ok) { open.value = false; reload(); }
}
async function toggle(j: Job, enabled: boolean) {
    const r = await act(rpc('system', 'updateCron', j.id, { ...j, enabled }));
    reload();
    return r;
}
async function runNow(j: Job) {
    const r = await act(rpc('system', 'runCron', j.id));
    if (r.ok) follow(r.data?.id);
}
const delOpen = ref(false);
const del = ref<Job | null>(null);
async function remove() {
    if (!del.value) return;
    const r = await act(rpc('system', 'deleteCron', del.value.id), t('Job deleted'));
    if (r.ok) reload();
}
</script>

<template>
    <Head :title="t('Scheduler')" />
    <PageHeader :title="t('Scheduler')" :description="t('Server-wide scheduled commands (cron). Site cron jobs live on each site.')">
        <Button @click="openNew"><PlusIcon /> {{ t('New job') }}</Button>
    </PageHeader>
    <ServiceDown v-if="!available" name="System" />
    <TaskPanel :task="task" @dismiss="dismiss" />

    <EmptyState v-if="available && !jobs.length" :title="t('No scheduled jobs')" :description="t('Run backups, clean-ups or scripts on a schedule.')"><template #icon><CalendarClockIcon /></template></EmptyState>
    <Card v-if="jobs.length" class="gap-0 py-0">
        <Table>
            <TableHeader><TableRow><TableHead class="w-16 px-5">{{ t('On') }}</TableHead><TableHead>{{ t('Name') }}</TableHead><TableHead>{{ t('Schedule') }}</TableHead><TableHead>{{ t('Command') }}</TableHead><TableHead class="px-5 text-right">{{ t('Actions') }}</TableHead></TableRow></TableHeader>
            <TableBody>
                <TableRow v-for="j in jobs" :key="j.id" :class="{ 'opacity-60': !j.enabled }">
                    <TableCell class="px-5"><Switch :model-value="j.enabled" :aria-label="j.name" @update:model-value="(v: boolean) => toggle(j, v)" /></TableCell>
                    <TableCell class="font-medium">{{ j.name }}<p class="text-muted-foreground text-xs font-normal">{{ t('as') }} {{ j.user }}</p></TableCell>
                    <TableCell class="font-mono text-sm">{{ j.schedule }}</TableCell>
                    <TableCell class="text-muted-foreground max-w-xs truncate font-mono text-xs" :title="j.command">{{ j.command }}</TableCell>
                    <TableCell class="px-5"><div class="flex justify-end gap-1">
                        <Button variant="outline" size="icon-sm" :aria-label="t('Run now')" :title="t('Run now')" :disabled="task?.status === 'running'" @click="runNow(j)"><PlayIcon /></Button>
                        <Button variant="outline" size="icon-sm" :aria-label="t('Edit')" :title="t('Edit')" @click="openEdit(j)"><PencilIcon /></Button>
                        <Button variant="ghost" size="icon-sm" class="text-muted-foreground hover:text-destructive" :aria-label="t('Delete')" @click="del = j; delOpen = true"><Trash2Icon /></Button>
                    </div></TableCell>
                </TableRow>
            </TableBody>
        </Table>
    </Card>

    <Dialog v-model:open="open">
        <DialogContent class="sm:max-w-lg">
            <DialogHeader><DialogTitle>{{ editing ? t('Edit job') : t('New job') }}</DialogTitle><DialogDescription>{{ t('Five fields: minute hour day month weekday. Or a macro such as @daily.') }}</DialogDescription></DialogHeader>
            <form class="grid gap-4" @submit.prevent="save">
                <label class="grid gap-1.5 text-sm font-medium">{{ t('Name') }}<Input v-model="form.name" required maxlength="80" :placeholder="t('Clean temporary files')" autocomplete="off" /></label>
                <div class="grid gap-3 sm:grid-cols-[1fr_9rem]">
                    <label class="grid gap-1.5 text-sm font-medium">{{ t('Schedule') }}<Input v-model="form.schedule" required class="font-mono" autocomplete="off" /></label>
                    <label class="grid gap-1.5 text-sm font-medium">{{ t('Run as') }}<Input v-model="form.user" class="font-mono" autocomplete="off" /></label>
                </div>
                <div class="flex flex-wrap gap-1.5"><Button v-for="p in presets" :key="p.v" type="button" size="xs" :variant="form.schedule === p.v ? 'default' : 'outline'" @click="form.schedule = p.v">{{ t(p.label) }}</Button></div>
                <label class="grid gap-1.5 text-sm font-medium">{{ t('Command') }}<Input v-model="form.command" required class="font-mono" placeholder="find /tmp -mtime +7 -delete" autocomplete="off" /></label>
                <DialogFooter><Button type="button" variant="outline" @click="open = false">{{ t('Cancel') }}</Button><Button type="submit" :disabled="busy">{{ t('Save') }}</Button></DialogFooter>
            </form>
        </DialogContent>
    </Dialog>
    <ConfirmDialog v-model:open="delOpen" :title="t('Delete {name}?', { name: del?.name ?? '' })" :description="t('The job will no longer run.')" @confirm="remove" />
</template>

<script setup lang="ts">
import { computed, reactive, ref } from 'vue';
import { Head, router } from '@inertiajs/vue3';
import { ArchiveIcon, HardDriveIcon, PlayIcon, PlusIcon, RotateCcwIcon, Trash2Icon } from '@lucide/vue';
import PageHeader from '@/components/app/PageHeader.vue';
import ServiceDown from '@/components/app/ServiceDown.vue';
import ConfirmDialog from '@/components/app/ConfirmDialog.vue';
import TaskPanel from '@/components/app/TaskPanel.vue';
import EmptyState from '@/components/app/EmptyState.vue';
import StatusBadge from '@/components/app/StatusBadge.vue';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { Card } from '@/components/ui/card';
import { Checkbox } from '@/components/ui/checkbox';
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog';
import { Field, FieldGroup, FieldLabel, FieldDescription } from '@/components/ui/field';
import { Input } from '@/components/ui/input';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select';
import { Tabs, TabsList, TabsTrigger } from '@/components/ui/tabs';
import { Textarea } from '@/components/ui/textarea';
import { act, useBusy } from '@/composables/useActions';
import { rpc } from '@/composables/useRpc';
import { useInterval, useTask } from '@/composables/useTask';
import { ago } from '@/lib/format';
import { useI18n } from '@/composables/useI18n';

const { t } = useI18n();

interface Dest { id: number; name: string; type: string; config: Record<string, string> }
interface Job { id: number; name: string; site: string; home_path: string; databases: unknown[]; destination_id: number; schedule: string; keep: number }
interface Run { id: number; job_id: number; status: string; started_at: string; size?: number }
const props = defineProps<{ destinations: Dest[]; jobs: Job[]; runs: Run[]; sites: { domain: string; user: string }[]; databases: { value: string; label: string }[]; available: boolean }>();

const reload = () => router.reload({ only: ['destinations', 'jobs', 'runs'] });
useInterval(reload, 10000);
const { task, follow, dismiss } = useTask('backup', () => reload());
const { busy, run } = useBusy();
const destName = (id: number) => props.destinations.find((d) => d.id === id)?.name ?? '?';
const runsFor = (id: number) => props.runs.filter((r) => r.job_id === id).slice(0, 5);
const where = (d: Dest) => d.config.path && d.type === 'sftp' ? `${d.config.user}@${d.config.host}:${d.config.path}` : (d.config.path ?? d.config.repo ?? '');

// destination
const destOpen = ref(false);
const d = reactive({ type: 'local', name: '', path: '', host: '', port: '22', user: '', repo: '', pass: '', key: '' });
function openDest() { Object.assign(d, { type: 'local', name: '', path: '', host: '', port: '22', user: '', repo: '', pass: '', key: '' }); destOpen.value = true; }
async function addDest() {
    const config = d.type === 'local' ? { path: d.path } : d.type === 'sftp' ? { host: d.host, port: d.port, user: d.user, path: d.path } : { repo: d.repo };
    const r = await run(() => act(rpc('backup', 'addDestination', { name: d.name, type: d.type, config, passphrase: d.pass, key: d.key })));
    if (!r.ok) return;
    destOpen.value = false;
    reload();
    await act(rpc('backup', 'testDestination', r.data.id), t('Destination saved and tested.'));
}
const testDest = (id: number) => act(rpc('backup', 'testDestination', id), t('Connection works.'));
const rmDest = ref<Dest | null>(null);
const rmDestOpen = ref(false);
async function delDest() { if (rmDest.value && (await act(rpc('backup', 'deleteDestination', rmDest.value.id))).ok) reload(); }
async function installBorg() { const r = await act(rpc('backup', 'installBorg')); if (r.ok) { destOpen.value = false; follow(r.data?.id); } }

// job
const jobOpen = ref(false);
const j = reactive({ name: '', site: '', files: true, dbs: [] as string[], dest: '', schedule: '0 3 * * *', keep: 7 });
function openJob() { Object.assign(j, { name: '', site: '', files: true, dbs: [], dest: String(props.destinations[0]?.id ?? ''), schedule: '0 3 * * *', keep: 7 }); jobOpen.value = true; }
async function addJob() {
    const site = props.sites.find((s) => s.domain === j.site);
    const dbs = j.dbs.map((v) => { const [engine, ...n] = v.split(':'); return { engine, name: n.join(':') }; });
    const r = await run(() => act(rpc('backup', 'addJob', { name: j.name, site: j.site, home_path: j.files && site ? `/srv/sites/${site.user}` : '', databases: dbs, destination_id: Number(j.dest), schedule: j.schedule, keep: Number(j.keep) }), 'Backup job created'));
    if (r.ok) { jobOpen.value = false; reload(); }
}
async function runNow(id: number) { const r = await act(rpc('backup', 'runJob', id)); if (r.ok) follow(r.data?.id); }
const rmJob = ref<Job | null>(null);
const rmJobOpen = ref(false);
async function delJob() { if (rmJob.value && (await act(rpc('backup', 'deleteJob', rmJob.value.id))).ok) reload(); }

// restore
const restoreOpen = ref(false);
const restoring = ref<number | null>(null);
const rs = reactive({ mode: 'beside', files: true, dbs: false });
function openRestore(id: number) { restoring.value = id; Object.assign(rs, { mode: 'beside', files: true, dbs: false }); restoreOpen.value = true; }
const modeModel = computed({ get: () => rs.mode, set: (v: string) => { rs.mode = v; if (v === 'beside') rs.dbs = false; } });
async function restore() {
    if (!restoring.value) return;
    const r = await run(() => act(rpc('backup', 'restore', restoring.value, { mode: rs.mode, files: rs.files, databases: rs.dbs })));
    if (r.ok) { restoreOpen.value = false; follow(r.data?.id); }
}
</script>

<template>
    <Head :title="t('Backups')" />
    <PageHeader :title="t('Backups')" :description="t('Sites and databases to a local folder, an SFTP server or a Borg repository.')">
        <Button variant="outline" :disabled="!available" @click="openDest"><HardDriveIcon /> {{ t('Add destination') }}</Button>
        <Button :disabled="!available || !destinations.length" @click="openJob"><PlusIcon /> {{ t('New backup job') }}</Button>
    </PageHeader>

    <ServiceDown v-if="!available" name="Backup" />
    <TaskPanel :task="task" @dismiss="dismiss" />

    <section class="flex flex-col gap-3">
        <h2 class="text-lg font-semibold">{{ t('Destinations') }}</h2>
        <EmptyState v-if="!destinations.length" :title="t('Add a destination first')" :description="t('This is where backups are stored.')"><template #icon><HardDriveIcon /></template></EmptyState>
        <Card v-else class="gap-0 py-0">
            <ul class="divide-y">
                <li v-for="x in destinations" :key="x.id" class="flex flex-wrap items-center justify-between gap-3 p-4">
                    <div class="min-w-0">
                        <p class="flex items-center gap-2 font-medium">{{ x.name }} <Badge variant="secondary">{{ x.type }}</Badge></p>
                        <p class="text-muted-foreground truncate text-sm">{{ where(x) }}</p>
                    </div>
                    <div class="flex gap-2">
                        <Button variant="outline" size="sm" @click="testDest(x.id)">{{ t('Test') }}</Button>
                        <Button variant="outline" size="icon-sm" class="text-destructive" :aria-label="`${t('Remove')} ${x.name}`" @click="rmDest = x; rmDestOpen = true"><Trash2Icon /></Button>
                    </div>
                </li>
            </ul>
        </Card>
    </section>

    <section class="flex flex-col gap-3">
        <h2 class="text-lg font-semibold">{{ t('Backup jobs') }}</h2>
        <EmptyState v-if="!jobs.length" :title="t('No backup jobs yet')"><template #icon><ArchiveIcon /></template></EmptyState>
        <Card v-for="x in jobs" :key="x.id" class="gap-3 p-4">
            <div class="flex flex-wrap items-center justify-between gap-3">
                <div>
                    <p class="font-medium">{{ x.name }}</p>
                    <p class="text-muted-foreground text-sm">{{ x.site }} · {{ [x.home_path ? t('files') : '', x.databases.length ? `${x.databases.length} ${t('databases')}` : ''].filter(Boolean).join(' + ') }} → {{ destName(x.destination_id) }} · {{ x.schedule || t('manual') }} · {{ t('keep') }} {{ x.keep }}</p>
                </div>
                <div class="flex gap-2">
                    <Button size="sm" @click="runNow(x.id)"><PlayIcon /> {{ t('Back up now') }}</Button>
                    <Button variant="outline" size="icon-sm" class="text-destructive" :aria-label="`${t('Delete')} ${x.name}`" @click="rmJob = x; rmJobOpen = true"><Trash2Icon /></Button>
                </div>
            </div>
            <ul v-if="runsFor(x.id).length" class="flex flex-col gap-1.5">
                <li v-for="r in runsFor(x.id)" :key="r.id" class="flex flex-wrap items-center justify-between gap-2 text-sm">
                    <span class="flex min-w-0 items-center gap-2"><StatusBadge :status="r.status" /><span class="text-muted-foreground">{{ ago(r.started_at) }}<template v-if="r.size"> · {{ (r.size / 1048576).toFixed(1) }} MB</template></span></span>
                    <Button v-if="r.status === 'ok'" variant="ghost" size="xs" @click="openRestore(r.id)"><RotateCcwIcon /> {{ t('Restore') }}</Button>
                </li>
            </ul>
        </Card>
    </section>

    <Dialog v-model:open="destOpen">
        <DialogContent class="max-h-[90svh] overflow-y-auto sm:max-w-lg">
            <form class="flex flex-col gap-4" @submit.prevent="addDest">
                <DialogHeader><DialogTitle>{{ t('Add destination') }}</DialogTitle><DialogDescription>{{ t('The connection is tested right after saving.') }}</DialogDescription></DialogHeader>
                <Tabs v-model="d.type"><TabsList class="w-full"><TabsTrigger value="local">{{ t('Local folder') }}</TabsTrigger><TabsTrigger value="sftp">{{ t('SFTP') }}</TabsTrigger><TabsTrigger value="borg">{{ t('Borg') }}</TabsTrigger></TabsList></Tabs>
                <FieldGroup>
                    <Field><FieldLabel for="d-name">{{ t('Name') }}</FieldLabel><Input id="d-name" v-model="d.name" required /></Field>
                    <template v-if="d.type === 'local'"><Field><FieldLabel for="d-path">{{ t('Folder') }}</FieldLabel><Input id="d-path" v-model="d.path" placeholder="/var/backups/panel" required /></Field></template>
                    <template v-else-if="d.type === 'sftp'">
                        <div class="grid grid-cols-[1fr_6rem] gap-3"><Field><FieldLabel for="d-host">{{ t('Host') }}</FieldLabel><Input id="d-host" v-model="d.host" required /></Field><Field><FieldLabel for="d-port">{{ t('Port') }}</FieldLabel><Input id="d-port" v-model="d.port" /></Field></div>
                        <Field><FieldLabel for="d-user">{{ t('User') }}</FieldLabel><Input id="d-user" v-model="d.user" required /></Field>
                        <Field><FieldLabel for="d-rpath">{{ t('Remote folder') }}</FieldLabel><Input id="d-rpath" v-model="d.path" placeholder="/backups" required /></Field>
                        <Field><FieldLabel for="d-key">{{ t('Private key (stays on this server)') }}</FieldLabel><Textarea id="d-key" v-model="d.key" rows="4" class="font-mono text-xs" spellcheck="false" /><FieldDescription>{{ t('The server host key is pinned on the first successful connection.') }}</FieldDescription></Field>
                    </template>
                    <template v-else>
                        <Field><FieldLabel for="d-repo">{{ t('Repository') }}</FieldLabel><Input id="d-repo" v-model="d.repo" placeholder="/var/backups/borg or ssh://user@host/./repo" required /></Field>
                        <Field><FieldLabel for="d-pass">{{ t('Passphrase (8+ characters)') }}</FieldLabel><Input id="d-pass" v-model="d.pass" type="password" required /><FieldDescription>{{ t('Keep it somewhere safe: without it the backups cannot be opened.') }}</FieldDescription></Field>
                        <Field v-if="d.repo.startsWith('ssh://')"><FieldLabel for="d-bkey">{{ t('SSH private key') }}</FieldLabel><Textarea id="d-bkey" v-model="d.key" rows="4" class="font-mono text-xs" spellcheck="false" /></Field>
                        <Button type="button" variant="outline" @click="installBorg">{{ t('Install BorgBackup on this server') }}</Button>
                    </template>
                </FieldGroup>
                <DialogFooter><Button type="button" variant="outline" @click="destOpen = false">{{ t('Cancel') }}</Button><Button type="submit" :disabled="busy">{{ t('Save and test') }}</Button></DialogFooter>
            </form>
        </DialogContent>
    </Dialog>

    <Dialog v-model:open="jobOpen">
        <DialogContent class="max-h-[90svh] overflow-y-auto sm:max-w-lg">
            <form class="flex flex-col gap-4" @submit.prevent="addJob">
                <DialogHeader><DialogTitle>{{ t('New backup job') }}</DialogTitle><DialogDescription>{{ t('Choose what to back up and where it goes.') }}</DialogDescription></DialogHeader>
                <FieldGroup>
                    <Field><FieldLabel for="j-name">{{ t('Name') }}</FieldLabel><Input id="j-name" v-model="j.name" required /></Field>
                    <Field><FieldLabel>{{ t('Site') }}</FieldLabel><Select v-model="j.site" required><SelectTrigger class="w-full"><SelectValue :placeholder="t('Choose a site')" /></SelectTrigger><SelectContent><SelectItem v-for="s in sites" :key="s.domain" :value="s.domain">{{ s.domain }}</SelectItem></SelectContent></Select></Field>
                    <Field orientation="horizontal"><Checkbox id="j-files" v-model="j.files" /><FieldLabel for="j-files">{{ t('Site files') }}</FieldLabel></Field>
                    <fieldset v-if="databases.length" class="flex flex-col gap-2">
                        <legend class="mb-1 text-sm font-medium">{{ t('Databases') }}</legend>
                        <label v-for="x in databases" :key="x.value" class="flex items-center gap-2 text-sm">
                            <Checkbox :model-value="j.dbs.includes(x.value)" @update:model-value="(v) => (j.dbs = v ? [...j.dbs, x.value] : j.dbs.filter((y) => y !== x.value))" /> {{ x.label }}
                        </label>
                    </fieldset>
                    <Field><FieldLabel>{{ t('Destination') }}</FieldLabel><Select v-model="j.dest"><SelectTrigger class="w-full"><SelectValue /></SelectTrigger><SelectContent><SelectItem v-for="x in destinations" :key="x.id" :value="String(x.id)">{{ x.name }}</SelectItem></SelectContent></Select></Field>
                    <div class="grid grid-cols-2 gap-3">
                        <Field><FieldLabel>{{ t('Schedule') }}</FieldLabel>
                            <Select v-model="j.schedule"><SelectTrigger class="w-full"><SelectValue /></SelectTrigger><SelectContent>
                                <SelectItem value="0 3 * * *">{{ t('Every day, 03:00') }}</SelectItem><SelectItem value="0 3 * * 0">{{ t('Every Sunday, 03:00') }}</SelectItem><SelectItem value="0 */6 * * *">{{ t('Every 6 hours') }}</SelectItem><SelectItem value="">{{ t('Manual only') }}</SelectItem>
                            </SelectContent></Select>
                        </Field>
                        <Field><FieldLabel for="j-keep">{{ t('Keep last') }}</FieldLabel><Input id="j-keep" v-model="j.keep" type="number" min="1" max="365" /></Field>
                    </div>
                </FieldGroup>
                <DialogFooter><Button type="button" variant="outline" @click="jobOpen = false">{{ t('Cancel') }}</Button><Button type="submit" :disabled="busy || !j.site">{{ t('Create') }}</Button></DialogFooter>
            </form>
        </DialogContent>
    </Dialog>

    <Dialog v-model:open="restoreOpen">
        <DialogContent class="sm:max-w-md">
            <form class="flex flex-col gap-4" @submit.prevent="restore">
                <DialogHeader><DialogTitle>{{ t('Restore this backup') }}</DialogTitle><DialogDescription>{{ t('Choose where the restored data goes.') }}</DialogDescription></DialogHeader>
                <Tabs v-model="modeModel"><TabsList class="w-full"><TabsTrigger value="beside">{{ t('Next to the live site') }}</TabsTrigger><TabsTrigger value="over">{{ t('Over the live site') }}</TabsTrigger></TabsList></Tabs>
                <p class="text-muted-foreground text-sm">{{ rs.mode === 'beside' ? t('Files go to a new restore-… folder. Nothing is overwritten.') : t('Replaces current files with the backed up ones.') }}</p>
                <Field orientation="horizontal"><Checkbox id="r-files" v-model="rs.files" /><FieldLabel for="r-files">{{ t('Files') }}</FieldLabel></Field>
                <Field orientation="horizontal"><Checkbox id="r-dbs" v-model="rs.dbs" :disabled="rs.mode !== 'over'" /><FieldLabel for="r-dbs">{{ t('Databases (only when restoring over the live site)') }}</FieldLabel></Field>
                <DialogFooter><Button type="button" variant="outline" @click="restoreOpen = false">{{ t('Cancel') }}</Button><Button type="submit" :variant="rs.mode === 'over' ? 'destructive' : 'default'" :disabled="busy">{{ t('Restore') }}</Button></DialogFooter>
            </form>
        </DialogContent>
    </Dialog>

    <ConfirmDialog v-model:open="rmDestOpen" :title="t('Remove this destination?')" :description="t('Existing backups stay where they are.')" :confirm-label="t('Remove')" @confirm="delDest" />
    <ConfirmDialog v-model:open="rmJobOpen" :title="t('Delete this job?')" :description="t('Existing backups are kept.')" @confirm="delJob" />
</template>

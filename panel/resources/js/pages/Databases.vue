<script setup lang="ts">
import { computed, reactive, ref } from 'vue';
import { Head, router } from '@inertiajs/vue3';
import { DatabaseIcon, ExternalLinkIcon, KeyRoundIcon, PlusIcon, Trash2Icon } from '@lucide/vue';
import PageHeader from '@/components/app/PageHeader.vue';
import ServiceDown from '@/components/app/ServiceDown.vue';
import ConfirmDialog from '@/components/app/ConfirmDialog.vue';
import SecretDialog, { type SecretRow } from '@/components/app/SecretDialog.vue';
import TaskPanel from '@/components/app/TaskPanel.vue';
import EmptyState from '@/components/app/EmptyState.vue';
import StatusBadge from '@/components/app/StatusBadge.vue';
import { Button } from '@/components/ui/button';
import { Card, CardContent, CardHeader, CardTitle, CardAction } from '@/components/ui/card';
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog';
import { Field, FieldGroup, FieldLabel } from '@/components/ui/field';
import { Input } from '@/components/ui/input';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select';
import { act, useBusy } from '@/composables/useActions';
import { rpc } from '@/composables/useRpc';
import { useTask } from '@/composables/useTask';
import { useI18n } from '@/composables/useI18n';

const { t } = useI18n();

interface Engine { engine: string; installed: boolean; versions: string[] }
interface Db { id: number; engine: string; name: string; user: string; site?: string }
const props = defineProps<{ engines: Engine[]; databases: Db[]; sites: string[]; tls: Record<string, boolean>; pma: { domain: string; version: string }[]; available: boolean }>();

const reload = () => router.reload({ only: ['engines', 'databases', 'pma'] });
const { task, follow, dismiss } = useTask('db', () => reload());
const pmaTask = useTask('core', () => reload());
const pmaSite = ref('');
const pmaUrl = (d: string) => `${props.tls[d] ? 'https' : 'http'}://${d}/`;
const pmaFree = computed(() => props.sites.filter((s) => !props.pma.some((p) => p.domain === s)));
async function installPma() {
    if (!pmaSite.value) return;
    const r = await act(rpc('core', 'installPhpMyAdmin', pmaSite.value));
    if (r.ok) pmaTask.follow(r.data?.id);
}
const pmaRemoving = ref<string | null>(null);
const pmaRemoveOpen = ref(false);
async function removePma() {
    if (pmaRemoving.value && (await act(rpc('core', 'removePhpMyAdmin', pmaRemoving.value), t('phpMyAdmin removed'))).ok) reload();
}
const { busy, run } = useBusy();
const installedEngines = computed(() => props.engines.filter((e) => e.installed).map((e) => e.engine));
const versions = reactive<Record<string, string>>({});

async function install(e: Engine) {
    const r = await act(rpc('db', 'installEngine', e.engine, versions[e.engine] ?? e.versions[0]));
    if (r.ok) follow(r.data?.id);
}

const createOpen = ref(false);
const form = reactive({ engine: '', name: '', site: '' });
function openCreate() {
    Object.assign(form, { engine: installedEngines.value[0] ?? '', name: '', site: '' });
    createOpen.value = true;
}
const secretOpen = ref(false);
const secret = ref<SecretRow[]>([]);
async function create() {
    const r = await run(() => act(rpc('db', 'createDatabase', form.engine, form.name.trim(), form.site || '')));
    if (!r.ok) return;
    createOpen.value = false;
    secret.value = [{ label: t('Database'), value: r.data.name }, { label: t('User'), value: r.data.user }, { label: t('Password'), value: r.extra?.password ?? '' }];
    secretOpen.value = true;
    reload();
}
async function resetPassword(d: Db) {
    const r = await act(rpc('db', 'resetPassword', d.id));
    if (!r.ok) return;
    secret.value = [{ label: t('Database'), value: d.name }, { label: t('User'), value: d.user }, { label: t('Password'), value: r.extra?.password ?? '' }];
    secretOpen.value = true;
}
const resetting = ref<Db | null>(null);
const resetOpen = ref(false);
const deleting = ref<Db | null>(null);
const deleteOpen = ref(false);
async function remove() {
    if (deleting.value && (await act(rpc('db', 'deleteDatabase', deleting.value.id), t('Database deleted'))).ok) reload();
}
</script>

<template>
    <Head :title="t('Databases')" />
    <PageHeader :title="t('Databases')" :description="t('Database servers and the databases on them.')">
        <Button :disabled="!available || !installedEngines.length" @click="openCreate"><PlusIcon /> {{ t('New database') }}</Button>
    </PageHeader>

    <ServiceDown v-if="!available" name="Database" />
    <TaskPanel :task="task" @dismiss="dismiss" />

    <section class="grid gap-3 sm:grid-cols-3" :aria-label="t('Database servers')">
        <Card v-for="e in engines" :key="e.engine" class="gap-3 py-4">
            <CardHeader>
                <CardTitle>{{ e.engine }}</CardTitle>
                <CardAction><StatusBadge :status="e.installed ? 'installed' : 'not installed'" :labels="{ installed: t('Installed') }" /></CardAction>
            </CardHeader>
            <CardContent v-if="!e.installed" class="flex gap-2">
                <Select :model-value="versions[e.engine] ?? e.versions[0]" @update:model-value="(v) => (versions[e.engine] = String(v))">
                    <SelectTrigger class="w-full" :aria-label="`${e.engine} version`"><SelectValue /></SelectTrigger>
                    <SelectContent><SelectItem v-for="v in e.versions" :key="v" :value="v">{{ v }}</SelectItem></SelectContent>
                </Select>
                <Button @click="install(e)">{{ t('Install') }}</Button>
            </CardContent>
        </Card>
    </section>

    <TaskPanel :task="pmaTask.task.value" @dismiss="pmaTask.dismiss" />
    <Card v-if="available && installedEngines.some((e) => e !== 'PostgreSQL')" class="gap-3 py-4">
        <CardHeader>
            <CardTitle>phpMyAdmin</CardTitle>
            <CardAction><StatusBadge :status="pma.length ? 'installed' : 'not installed'" :labels="{ installed: t('Installed') }" /></CardAction>
        </CardHeader>
        <CardContent class="flex flex-col gap-3">
            <p class="text-muted-foreground text-sm">{{ t('Web interface for MariaDB and MySQL. It runs as its own site, so use a dedicated subdomain such as pma.example.com. Log in with a database user from this page; the root account is disabled.') }}</p>
            <ul v-if="pma.length" class="divide-y rounded-md border">
                <li v-for="p in pma" :key="p.domain" class="flex flex-wrap items-center justify-between gap-2 px-3 py-2">
                    <span class="font-mono text-sm">{{ p.domain }} <span class="text-muted-foreground">· v{{ p.version }}</span></span>
                    <span class="flex gap-2">
                        <Button as-child variant="outline" size="sm"><a :href="pmaUrl(p.domain)" target="_blank" rel="noopener"><ExternalLinkIcon /> {{ t('Open') }}</a></Button>
                        <Button variant="outline" size="icon-sm" class="text-destructive" :aria-label="`${t('Remove')} ${p.domain}`" @click="pmaRemoving = p.domain; pmaRemoveOpen = true"><Trash2Icon /></Button>
                    </span>
                </li>
            </ul>
            <div class="flex flex-wrap gap-2">
                <Select :model-value="pmaSite" @update:model-value="(v) => (pmaSite = String(v))">
                    <SelectTrigger class="w-full sm:w-72" :aria-label="t('Site')"><SelectValue :placeholder="t('Choose an empty site')" /></SelectTrigger>
                    <SelectContent><SelectItem v-for="s in pmaFree" :key="s" :value="s">{{ s }}</SelectItem></SelectContent>
                </Select>
                <Button :disabled="!pmaSite" @click="installPma">{{ t('Install phpMyAdmin') }}</Button>
            </div>
            <p v-if="!sites.length" class="text-muted-foreground text-sm">{{ t('Create a site first, ideally pma.example.com, then install phpMyAdmin into it.') }}</p>
        </CardContent>
    </Card>

    <EmptyState v-if="available && !databases.length" :title="t('No databases yet')" :description="installedEngines.length ? 'Create one for a site.' : 'Install a database server first.'"><template #icon><DatabaseIcon /></template></EmptyState>
    <Card v-else-if="databases.length" class="gap-0 py-0">
        <ul class="divide-y">
            <li v-for="d in databases" :key="d.id" class="flex flex-wrap items-center justify-between gap-3 p-4">
                <div>
                    <p class="font-medium">{{ d.name }}</p>
                    <p class="text-muted-foreground text-sm">{{ d.engine }} · {{ t('user') }} {{ d.user }}<template v-if="d.site"> · {{ d.site }}</template></p>
                </div>
                <div class="flex gap-2">
                    <Button variant="outline" size="sm" @click="resetting = d; resetOpen = true"><KeyRoundIcon /> {{ t('New password') }}</Button>
                    <Button variant="outline" size="icon-sm" class="text-destructive" :aria-label="`${t('Delete')} ${d.name}`" @click="deleting = d; deleteOpen = true"><Trash2Icon /></Button>
                </div>
            </li>
        </ul>
    </Card>

    <Dialog v-model:open="createOpen">
        <DialogContent class="sm:max-w-md">
            <form class="flex flex-col gap-4" @submit.prevent="create">
                <DialogHeader><DialogTitle>{{ t('New database') }}</DialogTitle><DialogDescription>{{ t('A dedicated user is created with it.') }}</DialogDescription></DialogHeader>
                <FieldGroup>
                    <Field><FieldLabel>{{ t('Server') }}</FieldLabel><Select v-model="form.engine"><SelectTrigger class="w-full"><SelectValue /></SelectTrigger><SelectContent><SelectItem v-for="e in installedEngines" :key="e" :value="e">{{ e }}</SelectItem></SelectContent></Select></Field>
                    <Field><FieldLabel for="db-name">{{ t('Name') }}</FieldLabel><Input id="db-name" v-model="form.name" autocomplete="off" required /></Field>
                    <Field><FieldLabel>{{ t('Site (optional)') }}</FieldLabel>
                        <Select :model-value="form.site || '_none'" @update:model-value="(v) => (form.site = v === '_none' ? '' : String(v))"><SelectTrigger class="w-full"><SelectValue /></SelectTrigger><SelectContent><SelectItem value="_none">{{ t('None') }}</SelectItem><SelectItem v-for="s in sites" :key="s" :value="s">{{ s }}</SelectItem></SelectContent></Select>
                    </Field>
                </FieldGroup>
                <DialogFooter><Button type="button" variant="outline" @click="createOpen = false">{{ t('Cancel') }}</Button><Button type="submit" :disabled="busy">{{ t('Create') }}</Button></DialogFooter>
            </form>
        </DialogContent>
    </Dialog>

    <SecretDialog v-model:open="secretOpen" :title="t('Save these credentials')" :description="t('The password is shown only once and is not stored.')" :rows="secret" />
    <ConfirmDialog v-model:open="pmaRemoveOpen" :title="t('Remove phpMyAdmin?')" :description="t('Its files are deleted from the site. Your databases are not touched.')" :confirm-label="t('Remove')" @confirm="removePma" />
    <ConfirmDialog v-model:open="resetOpen" :title="t('Generate a new password?')" :description="t('The old password stops working immediately.')" :confirm-label="t('Generate')" @confirm="resetting && resetPassword(resetting)" />
    <ConfirmDialog v-model:open="deleteOpen" :title="t('Delete this database?')" :description="t('All its data is erased permanently. Take a backup first if unsure.')" :type-name="deleting?.name" :confirm-label="t('Delete database')" @confirm="remove" />
</template>

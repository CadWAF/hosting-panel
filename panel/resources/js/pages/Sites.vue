<script setup lang="ts">
import { computed, reactive, ref } from 'vue';
import { Head, Link, router } from '@inertiajs/vue3';
import { toast } from 'vue-sonner';
import { GlobeIcon, PlusIcon, SearchIcon, SettingsIcon, Trash2Icon, XIcon } from '@lucide/vue';
import PageHeader from '@/components/app/PageHeader.vue';
import ServiceDown from '@/components/app/ServiceDown.vue';
import ConfirmDialog from '@/components/app/ConfirmDialog.vue';
import EmptyState from '@/components/app/EmptyState.vue';
import StatusBadge from '@/components/app/StatusBadge.vue';
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { Card, CardContent } from '@/components/ui/card';
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog';
import { Field, FieldDescription, FieldGroup, FieldLabel } from '@/components/ui/field';
import { Input } from '@/components/ui/input';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select';
import { Switch } from '@/components/ui/switch';
import { act, useBusy } from '@/composables/useActions';
import { rpc } from '@/composables/useRpc';
import { useInterval } from '@/composables/useTask';
import { useI18n } from '@/composables/useI18n';

const { t } = useI18n();

interface Site { domain: string; user: string; php_version: string; web_mode?: string; preset?: string; status: string; tls?: string; php_settings?: Record<string, string>; docroot?: string }
const props = defineProps<{ sites: Site[]; installed: string[]; settingKeys: string[]; phpVersions: string[]; available: boolean }>();

useInterval(() => router.reload({ only: ['sites'] }), 15000);

const search = ref('');
const filtered = computed(() => props.sites.filter((s) => s.domain.includes(search.value.trim().toLowerCase())));
const versions = computed(() => (props.installed.length ? props.installed : props.phpVersions));
const stats = computed(() => [
    { label: t('Sites'), value: props.sites.length },
    { label: t('Enabled'), value: props.sites.filter((s) => s.status === 'enabled').length },
    { label: t('Disabled'), value: props.sites.filter((s) => s.status !== 'enabled').length },
    { label: 'PHP versions', value: new Set(props.sites.map((s) => s.php_version)).size },
]);
const reload = () => router.reload({ only: ['sites'] });
const { busy, run } = useBusy();

// create
const createOpen = ref(false);
const form = reactive({ domain: '', php: '', mode: 'nginx', preset: 'medium' });
function openCreate() {
    Object.assign(form, { domain: '', php: versions.value[0] ?? '', mode: 'nginx', preset: 'medium' });
    createOpen.value = true;
}
async function create() {
    const r = await run(() => act(rpc('core', 'createSite', form.domain.trim().toLowerCase(), form.php, form.mode, form.preset), t('Created {name}', { name: form.domain })));
    if (r.ok) { createOpen.value = false; reload(); }
}

// settings
const editing = ref<Site | null>(null);
const editOpen = ref(false);
const edit = reactive({ php: '', mode: 'nginx', preset: 'medium', docroot: '', settings: {} as Record<string, string>, newKey: '' });
function openEdit(s: Site) {
    editing.value = s;
    Object.assign(edit, { php: s.php_version, mode: s.web_mode ?? 'nginx', preset: s.preset ?? 'medium', docroot: s.docroot ?? '', settings: { ...(s.php_settings ?? {}) }, newKey: '' });
    editOpen.value = true;
}
function addSetting() {
    if (edit.newKey && !(edit.newKey in edit.settings)) edit.settings[edit.newKey] = '';
    edit.newKey = '';
}
async function save() {
    if (!editing.value) return;
    const d = editing.value.domain;
    const r = await run(() => act(rpc('core', 'updateSite', d, { php_version: edit.php, web_mode: edit.mode, preset: edit.preset, docroot: edit.docroot, php_settings: edit.settings }), t('Settings saved for {name}', { name: d })));
    if (r.ok) { editOpen.value = false; reload(); }
}

// toggle / delete
async function toggle(s: Site, on: boolean) {
    const r = await act(rpc('core', 'setSiteEnabled', s.domain, on));
    if (r.ok) {
        toast(t(on ? '{name} enabled' : '{name} disabled', { name: s.domain }), { action: { label: t('Undo'), onClick: async () => { await act(rpc('core', 'setSiteEnabled', s.domain, !on)); reload(); } } });
    }
    reload();
}
const deleting = ref<string | null>(null);
const deleteOpen = ref(false);
async function remove() {
    if (!deleting.value) return;
    const r = await act(rpc('core', 'deleteSite', deleting.value), t('Deleted {name}', { name: deleting.value }));
    if (r.ok) reload();
}
</script>

<template>
    <Head :title="t('Sites')" />
    <PageHeader :title="t('Sites')" :description="`${sites.length} total`">
        <div class="relative">
            <SearchIcon class="text-muted-foreground pointer-events-none absolute top-2.5 left-2.5 size-4" />
            <Input v-model="search" type="search" :placeholder="t('Search sites')" :aria-label="t('Search sites')" class="w-52 pl-8" />
        </div>
        <Button :disabled="!available" @click="openCreate"><PlusIcon /> {{ t('New site') }}</Button>
    </PageHeader>

    <ServiceDown v-if="!available" name="Core" />

    <Alert v-if="available && !installed.length">
        <AlertTitle>{{ t('No PHP installed yet') }}</AlertTitle>
        <AlertDescription>
            <p>{{ t('Install a PHP version before creating sites.') }}</p>
            <Button as-child class="mt-2" size="sm"><Link href="/php">{{ t('Install PHP') }}</Link></Button>
        </AlertDescription>
    </Alert>

    <div v-if="available && sites.length" class="grid grid-cols-2 gap-3 lg:grid-cols-4">
        <Card v-for="s in stats" :key="s.label" class="gap-1 py-4">
            <CardContent><p class="text-muted-foreground text-xs font-medium uppercase">{{ s.label }}</p><p class="text-2xl font-semibold tabular-nums">{{ s.value }}</p></CardContent>
        </Card>
    </div>

    <EmptyState v-if="available && !filtered.length" :title="search ? t('No sites match your search') : t('No sites yet')" :description="search ? undefined : t('Create your first site: it gets its own user and PHP version.')">
        <template #icon><GlobeIcon /></template>
        <Button v-if="!search" @click="openCreate"><PlusIcon /> {{ t('New site') }}</Button>
    </EmptyState>

    <Card v-if="filtered.length" class="gap-0 py-0">
        <ul class="divide-y">
            <li v-for="s in filtered" :key="s.domain" class="flex flex-wrap items-center justify-between gap-3 p-4">
                <div class="min-w-0">
                    <Link :href="`/sites/${s.domain}`" class="text-primary block truncate font-medium hover:underline">{{ s.domain }}</Link>
                    <div class="mt-1.5 flex flex-wrap items-center gap-1.5">
                        <Badge variant="secondary">PHP {{ s.php_version }}</Badge>
                        <Badge variant="secondary">{{ s.web_mode === 'apache' ? 'nginx + Apache' : 'nginx' }}</Badge>
                        <Badge v-if="s.tls" variant="outline" class="border-success/30 text-success">{{ t('HTTPS') }}</Badge>
                        <Badge variant="outline" class="font-mono">{{ s.user }}</Badge>
                    </div>
                </div>
                <div class="flex items-center gap-3">
                    <StatusBadge :status="s.status" :labels="{ enabled: t('Enabled'), disabled: t('Disabled') }" />
                    <Switch :model-value="s.status === 'enabled'" :aria-label="`${t(s.status === 'enabled' ? 'Disable' : 'Enable')} ${s.domain}`" @update:model-value="(v: boolean) => toggle(s, v)" />
                    <Button variant="outline" size="icon" :aria-label="`${t('Settings')} ${s.domain}`" @click="openEdit(s)"><SettingsIcon /></Button>
                    <Button variant="outline" size="icon" class="text-destructive" :aria-label="`${t('Delete')} ${s.domain}`" @click="deleting = s.domain; deleteOpen = true"><Trash2Icon /></Button>
                </div>
            </li>
        </ul>
    </Card>

    <Dialog v-model:open="createOpen">
        <DialogContent class="sm:max-w-md">
            <form class="flex flex-col gap-4" @submit.prevent="create">
                <DialogHeader><DialogTitle>{{ t('New site') }}</DialogTitle><DialogDescription>{{ t('It gets its own Linux user and PHP-FPM pool.') }}</DialogDescription></DialogHeader>
                <FieldGroup>
                    <Field><FieldLabel for="c-domain">{{ t('Domain') }}</FieldLabel><Input id="c-domain" v-model="form.domain" placeholder="example.com" autocomplete="off" required /></Field>
                    <Field>
                        <FieldLabel>{{ t('PHP version') }}</FieldLabel>
                        <Select v-model="form.php"><SelectTrigger class="w-full"><SelectValue /></SelectTrigger><SelectContent><SelectItem v-for="v in versions" :key="v" :value="v">PHP {{ v }}</SelectItem></SelectContent></Select>
                    </Field>
                    <div class="grid grid-cols-2 gap-3">
                        <Field>
                            <FieldLabel>{{ t('Web server') }}</FieldLabel>
                            <Select v-model="form.mode"><SelectTrigger class="w-full"><SelectValue /></SelectTrigger><SelectContent><SelectItem value="nginx">nginx</SelectItem><SelectItem value="apache">{{ t('nginx + Apache') }}</SelectItem></SelectContent></Select>
                        </Field>
                        <Field>
                            <FieldLabel>{{ t('Resources') }}</FieldLabel>
                            <Select v-model="form.preset"><SelectTrigger class="w-full"><SelectValue /></SelectTrigger><SelectContent><SelectItem value="small">{{ t('Small') }}</SelectItem><SelectItem value="medium">{{ t('Medium') }}</SelectItem><SelectItem value="large">{{ t('Large') }}</SelectItem></SelectContent></Select>
                        </Field>
                    </div>
                </FieldGroup>
                <DialogFooter><Button type="button" variant="outline" @click="createOpen = false">{{ t('Cancel') }}</Button><Button type="submit" :disabled="busy || !form.php">{{ t('Create') }}</Button></DialogFooter>
            </form>
        </DialogContent>
    </Dialog>

    <Dialog v-model:open="editOpen">
        <DialogContent class="max-h-[90svh] overflow-y-auto sm:max-w-lg">
            <form class="flex flex-col gap-4" @submit.prevent="save">
                <DialogHeader><DialogTitle>{{ t('Settings') }} · {{ editing?.domain }}</DialogTitle><DialogDescription>{{ t('Changes rebuild the site\'s PHP pool and web config.') }}</DialogDescription></DialogHeader>
                <div class="grid grid-cols-3 gap-3">
                    <Field><FieldLabel>{{ t('PHP') }}</FieldLabel><Select v-model="edit.php"><SelectTrigger class="w-full"><SelectValue /></SelectTrigger><SelectContent><SelectItem v-for="v in versions" :key="v" :value="v">{{ v }}</SelectItem></SelectContent></Select></Field>
                    <Field><FieldLabel>{{ t('Web server') }}</FieldLabel><Select v-model="edit.mode"><SelectTrigger class="w-full"><SelectValue /></SelectTrigger><SelectContent><SelectItem value="nginx">nginx</SelectItem><SelectItem value="apache">{{ t('+ Apache') }}</SelectItem></SelectContent></Select></Field>
                    <Field><FieldLabel>{{ t('Resources') }}</FieldLabel><Select v-model="edit.preset"><SelectTrigger class="w-full"><SelectValue /></SelectTrigger><SelectContent><SelectItem value="small">{{ t('Small') }}</SelectItem><SelectItem value="medium">{{ t('Medium') }}</SelectItem><SelectItem value="large">{{ t('Large') }}</SelectItem></SelectContent></Select></Field>
                </div>
                <Field>
                    <FieldLabel for="e-docroot">{{ t('Document root') }}</FieldLabel>
                    <Input id="e-docroot" v-model="edit.docroot" :placeholder="t('public_html (leave empty) or public')" autocomplete="off" />
                    <FieldDescription>{{ t('Folder inside public_html that is served. Laravel sites use') }} <code>public</code>.</FieldDescription>
                </Field>
                <div class="flex flex-col gap-2">
                    <p class="text-sm font-medium">php.ini</p>
                    <p v-if="!Object.keys(edit.settings).length" class="text-muted-foreground text-sm">{{ t('Using the defaults for this size.') }}</p>
                    <div v-for="(_, k) in edit.settings" :key="k" class="flex items-center gap-2">
                        <code class="w-44 shrink-0 truncate text-sm">{{ k }}</code>
                        <Input v-model="edit.settings[k]" :aria-label="String(k)" />
                        <Button type="button" variant="ghost" size="icon" :aria-label="t('Remove setting')" @click="delete edit.settings[k]"><XIcon /></Button>
                    </div>
                    <div class="flex gap-2">
                        <Select v-model="edit.newKey"><SelectTrigger class="w-full"><SelectValue :placeholder="t('Add a setting…')" /></SelectTrigger><SelectContent><SelectItem v-for="k in settingKeys.filter((k) => !(k in edit.settings))" :key="k" :value="k">{{ k }}</SelectItem></SelectContent></Select>
                        <Button type="button" variant="outline" :disabled="!edit.newKey" @click="addSetting">{{ t('Add') }}</Button>
                    </div>
                </div>
                <DialogFooter><Button type="button" variant="outline" @click="editOpen = false">{{ t('Cancel') }}</Button><Button type="submit" :disabled="busy">{{ t('Save') }}</Button></DialogFooter>
            </form>
        </DialogContent>
    </Dialog>

    <ConfirmDialog v-model:open="deleteOpen" :title="t('Delete {name}?', { name: deleting ?? '' })" :description="t('The site stops working and its user is removed. Its files are moved to the trash directory, not erased.')" :type-name="deleting ?? undefined" :confirm-label="t('Delete site')" @confirm="remove" />
</template>

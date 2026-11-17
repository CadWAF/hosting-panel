<script setup lang="ts">
import { reactive, ref } from 'vue';
import { Head, router } from '@inertiajs/vue3';
import { FlameIcon, KeyRoundIcon, PlusIcon, Trash2Icon } from '@lucide/vue';
import ConfirmDialog from '@/components/app/ConfirmDialog.vue';
import EmptyState from '@/components/app/EmptyState.vue';
import InstallPrompt from '@/components/app/InstallPrompt.vue';
import PageHeader from '@/components/app/PageHeader.vue';
import ServiceDown from '@/components/app/ServiceDown.vue';
import TaskPanel from '@/components/app/TaskPanel.vue';
import { Button } from '@/components/ui/button';
import { Card, CardHeader, CardTitle } from '@/components/ui/card';
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog';
import { Input } from '@/components/ui/input';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select';
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table';
import { act, useBusy } from '@/composables/useActions';
import { useI18n } from '@/composables/useI18n';
import { rpc } from '@/composables/useRpc';
import { useTask } from '@/composables/useTask';

interface Account { name: string; uid: number; home: string; user: string }
interface Ftp { installed: boolean; running: boolean; accounts: Account[]; passive_min: number; passive_max: number }
const props = defineProps<{ ftp: Ftp | null; sites: { domain: string; user: string }[]; available: boolean }>();
const { t } = useI18n();
const { busy, run } = useBusy();
const reload = () => router.reload({ only: ['ftp'], preserveScroll: true });
const { task, follow, dismiss } = useTask('system', () => reload());

const createOpen = ref(false);
const form = reactive({ name: '', password: '', site_user: '', dir: '' });
function openCreate() {
    Object.assign(form, { name: '', password: randomPassword(), site_user: props.sites[0]?.user ?? '', dir: '' });
    createOpen.value = true;
}
function randomPassword(): string {
    const a = new Uint8Array(18);
    crypto.getRandomValues(a);
    return btoa(String.fromCharCode(...a)).replace(/[+/=]/g, 'x').slice(0, 20);
}
async function create() {
    const r = await run(() => act(rpc('system', 'addFtp', { ...form, name: form.name.trim(), dir: form.dir.trim().replace(/^\/+/, '') }), t('FTP account created')));
    if (r.ok) { createOpen.value = false; reload(); }
}
const pwOf = ref<Account | null>(null);
const pwOpen = ref(false);
const pw = ref('');
function openPw(a: Account) { pwOf.value = a; pw.value = randomPassword(); pwOpen.value = true; }
async function setPw() {
    if (!pwOf.value) return;
    const r = await run(() => act(rpc('system', 'setFtpPassword', pwOf.value!.name, pw.value), t('Password changed')));
    if (r.ok) pwOpen.value = false;
}
const delOpen = ref(false);
const del = ref<Account | null>(null);
async function remove() {
    if (!del.value) return;
    const r = await act(rpc('system', 'deleteFtp', del.value.name), t('Account deleted'));
    if (r.ok) reload();
}
async function openPorts() {
    const r = await run(() => act(rpc('system', 'openFtpPorts'), t('Firewall rules added')));
    if (r.ok) reload();
}
const copy = (s: string) => navigator.clipboard?.writeText(s);
</script>

<template>
    <Head title="FTP" />
    <PageHeader title="FTP" :description="t('FTP accounts that are locked into a site folder.')">
        <template v-if="ftp?.installed"><Button variant="outline" :disabled="busy" @click="openPorts"><FlameIcon /> {{ t('Open FTP ports in firewall') }}</Button><Button :disabled="!sites.length" @click="openCreate"><PlusIcon /> {{ t('New account') }}</Button></template>
    </PageHeader>
    <ServiceDown v-if="!available" name="System" />
    <TaskPanel :task="task" @dismiss="dismiss" />

    <InstallPrompt v-if="available && ftp && !ftp.installed" id="pure-ftpd" name="Pure-FTPd" :description="t('An FTP server with virtual accounts. Each account can only see its own site folder.')" :disabled="task?.status === 'running'" @started="follow" />
    <Card v-else-if="ftp && !ftp.running" class="border-warning/40"><div class="px-5 text-sm">{{ t('The FTP server is installed but not running. Start it from Software or Services.') }}</div></Card>

    <template v-if="ftp?.installed">
        <Card class="py-4"><div class="text-muted-foreground flex flex-wrap gap-x-8 gap-y-1 px-5 text-sm">
            <span>{{ t('Server') }}: <b class="text-foreground font-mono">{{ '<server-ip>' }}</b></span>
            <span>{{ t('Port') }}: <b class="text-foreground tabular font-mono">21</b></span>
            <span>{{ t('Passive ports') }}: <b class="text-foreground tabular font-mono">{{ ftp.passive_min }}–{{ ftp.passive_max }}</b></span>
            <span>{{ t('Plain FTP is not encrypted. Prefer SFTP (SSH keys on the site) for anything sensitive.') }}</span>
        </div></Card>

        <EmptyState v-if="!ftp.accounts.length" :title="t('No FTP accounts yet')" :description="sites.length ? t('Create an account for a site and give it to your customer or developer.') : t('Create a site first; FTP accounts belong to sites.')" />
        <Card v-else class="gap-0 py-0">
            <CardHeader class="border-b px-5 py-4"><CardTitle>{{ t('Accounts') }}</CardTitle></CardHeader>
            <Table>
                <TableHeader><TableRow><TableHead class="px-5">{{ t('Username') }}</TableHead><TableHead>{{ t('Folder') }}</TableHead><TableHead>{{ t('Runs as') }}</TableHead><TableHead class="px-5 text-right">{{ t('Actions') }}</TableHead></TableRow></TableHeader>
                <TableBody>
                    <TableRow v-for="a in ftp.accounts" :key="a.name">
                        <TableCell class="px-5 font-mono font-medium">{{ a.name }}</TableCell>
                        <TableCell class="text-muted-foreground font-mono text-sm">{{ a.home }}</TableCell>
                        <TableCell class="text-muted-foreground">{{ a.user }}</TableCell>
                        <TableCell class="px-5"><div class="flex justify-end gap-1">
                            <Button variant="outline" size="sm" @click="openPw(a)"><KeyRoundIcon /> {{ t('New password') }}</Button>
                            <Button variant="ghost" size="icon-sm" class="text-muted-foreground hover:text-destructive" :aria-label="t('Delete')" @click="del = a; delOpen = true"><Trash2Icon /></Button>
                        </div></TableCell>
                    </TableRow>
                </TableBody>
            </Table>
        </Card>
    </template>

    <Dialog v-model:open="createOpen">
        <DialogContent class="sm:max-w-lg">
            <DialogHeader><DialogTitle>{{ t('New FTP account') }}</DialogTitle><DialogDescription>{{ t('Files are written as the site user, so permissions always stay correct.') }}</DialogDescription></DialogHeader>
            <form class="grid gap-4" @submit.prevent="create">
                <label class="grid gap-1.5 text-sm font-medium">{{ t('Site') }}
                    <Select v-model="form.site_user"><SelectTrigger class="w-full"><SelectValue /></SelectTrigger>
                        <SelectContent><SelectItem v-for="s in sites" :key="s.user" :value="s.user">{{ s.domain }}</SelectItem></SelectContent></Select></label>
                <div class="grid gap-3 sm:grid-cols-2">
                    <label class="grid gap-1.5 text-sm font-medium">{{ t('Username') }}<Input v-model="form.name" required pattern="[a-z][a-z0-9_.\-]{2,31}" placeholder="shop-dev" autocomplete="off" /></label>
                    <label class="grid gap-1.5 text-sm font-medium">{{ t('Folder inside the site (optional)') }}<Input v-model="form.dir" placeholder="public_html" autocomplete="off" /></label>
                </div>
                <label class="grid gap-1.5 text-sm font-medium">{{ t('Password') }}
                    <div class="flex gap-2"><Input v-model="form.password" required minlength="10" class="font-mono" autocomplete="off" /><Button type="button" variant="outline" @click="copy(form.password)">{{ t('Copy') }}</Button></div></label>
                <DialogFooter><Button type="button" variant="outline" @click="createOpen = false">{{ t('Cancel') }}</Button><Button type="submit" :disabled="busy">{{ t('Create') }}</Button></DialogFooter>
            </form>
        </DialogContent>
    </Dialog>

    <Dialog v-model:open="pwOpen">
        <DialogContent class="sm:max-w-md">
            <DialogHeader><DialogTitle>{{ t('New password') }}</DialogTitle><DialogDescription>{{ pwOf?.name }}</DialogDescription></DialogHeader>
            <form class="grid gap-4" @submit.prevent="setPw">
                <div class="flex gap-2"><Input v-model="pw" required minlength="10" class="font-mono" autocomplete="off" :aria-label="t('Password')" /><Button type="button" variant="outline" @click="copy(pw)">{{ t('Copy') }}</Button></div>
                <DialogFooter><Button type="button" variant="outline" @click="pwOpen = false">{{ t('Cancel') }}</Button><Button type="submit" :disabled="busy">{{ t('Save') }}</Button></DialogFooter>
            </form>
        </DialogContent>
    </Dialog>

    <ConfirmDialog v-model:open="delOpen" :title="t('Delete {name}?', { name: del?.name ?? '' })" :description="t('The account stops working at once. Site files are not touched.')" @confirm="remove" />
</template>

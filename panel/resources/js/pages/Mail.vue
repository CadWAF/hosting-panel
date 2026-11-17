<script setup lang="ts">
import { reactive, ref } from 'vue';
import { Head, router } from '@inertiajs/vue3';
import { ExternalLinkIcon, MailPlusIcon, PlusIcon, RefreshCwIcon, SettingsIcon, Trash2Icon } from '@lucide/vue';
import { toast } from 'vue-sonner';
import PageHeader from '@/components/app/PageHeader.vue';
import ServiceDown from '@/components/app/ServiceDown.vue';
import ConfirmDialog from '@/components/app/ConfirmDialog.vue';
import SecretDialog, { type SecretRow } from '@/components/app/SecretDialog.vue';
import TaskPanel from '@/components/app/TaskPanel.vue';
import StatusBadge from '@/components/app/StatusBadge.vue';
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert';
import { Button } from '@/components/ui/button';
import { Card } from '@/components/ui/card';
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog';
import { Field, FieldGroup, FieldLabel } from '@/components/ui/field';
import { Input } from '@/components/ui/input';
import { act, useBusy } from '@/composables/useActions';
import { rpc } from '@/composables/useRpc';
import { useInterval, useTask } from '@/composables/useTask';
import { useI18n } from '@/composables/useI18n';

const { t } = useI18n();

interface Settings { installed?: boolean; hostname?: string; server_ipv4?: string; server_ipv6?: string; webmail_url?: string }
interface Domain { name: string; mailboxes: number }
interface Mailbox { id: number; email: string; domain: string }
interface Rec { purpose: string; type: string; name: string; value: string; status: string; where: string; found?: string }
const props = defineProps<{ settings: Settings; domains: Domain[]; mailboxes: Record<string, Mailbox[]>; available: boolean }>();

const reload = () => router.reload({ only: ['settings', 'domains', 'mailboxes'] });
useInterval(reload, 10000);
const { task, follow, dismiss } = useTask('mail', () => reload());
const { busy, run } = useBusy();

// settings
const setOpen = ref(false);
const s = reactive({ hostname: '', ipv4: '', ipv6: '', webmail: '' });
function openSettings() {
    Object.assign(s, { hostname: props.settings.hostname ?? '', ipv4: props.settings.server_ipv4 ?? '', ipv6: props.settings.server_ipv6 ?? '', webmail: props.settings.webmail_url ?? '' });
    setOpen.value = true;
}
async function saveSettings() {
    const r = await run(() => act(rpc('mail', 'saveSettings', { hostname: s.hostname, server_ipv4: s.ipv4, server_ipv6: s.ipv6, webmail_url: s.webmail }), t('Mail settings saved.')));
    if (r.ok) { setOpen.value = false; reload(); }
}
async function install() { const r = await act(rpc('mail', 'install')); if (r.ok) follow(r.data?.id); }

// domains
const newDomain = ref('');
const dnsFor = ref<string | null>(null);
const dnsOpen = ref(false);
const records = ref<Rec[]>([]);
async function showDns(name: string) {
    const r = await act(rpc<Rec[]>('mail', 'dns', name));
    if (r.ok) { dnsFor.value = name; records.value = r.data; dnsOpen.value = true; }
}
async function addDomain() {
    const name = newDomain.value.trim().toLowerCase();
    if (!name) return;
    const r = await run(() => act(rpc('mail', 'addDomain', name)));
    if (r.ok) { newDomain.value = ''; reload(); showDns(name); }
}
const rmDomain = ref<string | null>(null);
const rmDomainOpen = ref(false);
async function delDomain() { if (rmDomain.value && (await act(rpc('mail', 'deleteDomain', rmDomain.value))).ok) reload(); }
async function copy(v: string) { try { await navigator.clipboard.writeText(v); toast.success(t('Copied')); } catch { /* clipboard blocked */ } }
const tone = { ok: 'ok', wrong: 'wrong' } as Record<string, string>;
const dnsLabels = { ok: '✓ OK', wrong: t('Wrong value'), missing: t('Not found yet') };

// mailboxes
const boxOpen = ref(false);
const box = reactive({ domain: '', local: '', password: '' });
const secretOpen = ref(false);
const secret = ref<SecretRow[]>([]);
function genPassword() {
    const a = 'abcdefghijkmnopqrstuvwxyzABCDEFGHJKLMNPQRSTUVWXYZ23456789';
    box.password = Array.from(crypto.getRandomValues(new Uint32Array(16)), (n) => a[n % a.length]).join('');
}
function openBox(domain: string) { Object.assign(box, { domain, local: '', password: '' }); genPassword(); boxOpen.value = true; }
async function addMailbox() {
    const email = `${box.local.trim().toLowerCase()}@${box.domain}`;
    const r = await run(() => act(rpc('mail', 'addMailbox', email, box.password)));
    if (!r.ok) return;
    boxOpen.value = false;
    secret.value = [{ label: t('Address'), value: email }, { label: t('Password'), value: box.password }];
    secretOpen.value = true;
    reload();
}
const rmBox = ref<Mailbox | null>(null);
const rmBoxOpen = ref(false);
async function delBox() { if (rmBox.value && (await act(rpc('mail', 'deleteMailbox', rmBox.value.id))).ok) reload(); }
async function webmail(id: number) { const r = await act(rpc('mail', 'webmail', id)); if (r.ok && r.data?.url) window.location.href = r.data.url; }
</script>

<template>
    <Head :title="t('Mail')" />
    <PageHeader :title="t('Mail')" :description="t('Mail domains, mailboxes and webmail. Each domain gets its own DKIM key.')">
        <Button variant="outline" :disabled="!available" @click="openSettings"><SettingsIcon /> {{ t('Mail server settings') }}</Button>
    </PageHeader>

    <ServiceDown v-if="!available" name="Mail" />

    <Alert v-if="available && !settings.installed">
        <AlertTitle>{{ t('The mail server is not installed yet') }}</AlertTitle>
        <AlertDescription>
            <p>{{ settings.hostname ? t('Installs Postfix, Dovecot, Rspamd and Roundcube and configures them.') : t('Set the mail hostname and webmail address first.') }}</p>
            <div class="mt-2 flex gap-2">
                <Button variant="outline" size="sm" @click="openSettings">{{ t('Settings') }}</Button>
                <Button size="sm" :disabled="!settings.hostname" @click="install">{{ t('Install mail server') }}</Button>
            </div>
        </AlertDescription>
    </Alert>
    <TaskPanel :task="task" @dismiss="dismiss" />

    <form v-if="available" class="flex gap-2" @submit.prevent="addDomain">
        <Input v-model="newDomain" :placeholder="t('example.com or mail.sub.example.com')" :aria-label="t('Mail domain')" />
        <Button type="submit" class="shrink-0" :disabled="busy"><PlusIcon /> {{ t('Add mail domain') }}</Button>
    </form>

    <Card v-for="d in domains" :key="d.name" class="gap-3 p-4">
        <div class="flex flex-wrap items-center justify-between gap-3">
            <div><p class="font-medium">{{ d.name }}</p><p class="text-muted-foreground text-sm">{{ d.mailboxes }} {{ t('mailboxes') }}</p></div>
            <div class="flex gap-2">
                <Button variant="outline" size="sm" @click="showDns(d.name)">{{ t('DNS records') }}</Button>
                <Button variant="outline" size="sm" @click="openBox(d.name)"><MailPlusIcon /> {{ t('New mailbox') }}</Button>
                <Button variant="outline" size="icon-sm" class="text-destructive" :aria-label="`${t('Delete')} ${d.name}`" @click="rmDomain = d.name; rmDomainOpen = true"><Trash2Icon /></Button>
            </div>
        </div>
        <ul v-if="mailboxes[d.name]?.length" class="divide-y border-t">
            <li v-for="m in mailboxes[d.name]" :key="m.id" class="flex flex-wrap items-center justify-between gap-2 py-2 text-sm">
                <span>{{ m.email }}</span>
                <span class="flex gap-2">
                    <Button size="xs" @click="webmail(m.id)"><ExternalLinkIcon /> {{ t('Open webmail') }}</Button>
                    <Button variant="outline" size="icon-xs" class="text-destructive" :aria-label="`${t('Delete')} ${m.email}`" @click="rmBox = m; rmBoxOpen = true"><Trash2Icon /></Button>
                </span>
            </li>
        </ul>
    </Card>

    <Dialog v-model:open="dnsOpen">
        <DialogContent class="max-h-[90svh] overflow-y-auto sm:max-w-3xl">
            <DialogHeader>
                <DialogTitle>{{ t('DNS records for {name}', { name: dnsFor ?? '' }) }}</DialogTitle>
                <DialogDescription>{{ t('Add these at your DNS provider. Changes can take a while to appear. Click a value to copy it.') }}</DialogDescription>
            </DialogHeader>
            <ul class="flex flex-col gap-2">
                <li v-for="(r, i) in records" :key="i" class="flex flex-col gap-1.5 rounded-lg border p-3 text-sm">
                    <div class="flex items-center justify-between gap-2"><b>{{ r.purpose }}</b><StatusBadge :status="tone[r.status] ?? r.status" :labels="{ ok: dnsLabels.ok, wrong: dnsLabels.wrong, missing: dnsLabels.missing }" /></div>
                    <div class="grid gap-1 sm:grid-cols-[4rem_1fr]">
                        <span class="text-muted-foreground">{{ r.type }}</span><button class="text-left font-mono break-all" :title="t('Click to copy')" @click="copy(r.name)">{{ r.name }}</button>
                        <span class="text-muted-foreground">{{ t('Value') }}</span><button class="text-left font-mono break-all" :title="t('Click to copy')" @click="copy(r.value)">{{ r.value }}</button>
                    </div>
                    <p v-if="r.where !== 'dns'" class="text-muted-foreground text-xs">{{ t('Set this with your hosting provider, not in DNS.') }}</p>
                    <p v-if="r.found && r.status !== 'ok'" class="text-warning text-xs">{{ t('Currently found') }}: {{ r.found }}</p>
                </li>
            </ul>
            <DialogFooter>
                <Button variant="outline" @click="dnsFor && showDns(dnsFor)"><RefreshCwIcon /> {{ t('Check again') }}</Button>
                <Button @click="dnsOpen = false">{{ t('Done') }}</Button>
            </DialogFooter>
        </DialogContent>
    </Dialog>

    <Dialog v-model:open="boxOpen">
        <DialogContent class="sm:max-w-md">
            <form class="flex flex-col gap-4" @submit.prevent="addMailbox">
                <DialogHeader><DialogTitle>{{ t('New mailbox') }}</DialogTitle><DialogDescription>On {{ box.domain }}</DialogDescription></DialogHeader>
                <FieldGroup>
                    <Field><FieldLabel for="b-local">{{ t('Address') }}</FieldLabel><div class="flex items-center gap-2"><Input id="b-local" v-model="box.local" autocomplete="off" required /><span class="text-muted-foreground shrink-0 text-sm">@{{ box.domain }}</span></div></Field>
                    <Field><FieldLabel for="b-pw">{{ t('Password (10+ characters)') }}</FieldLabel><div class="flex gap-2"><Input id="b-pw" v-model="box.password" autocomplete="off" class="font-mono" required /><Button type="button" variant="outline" class="shrink-0" @click="genPassword">{{ t('Generate') }}</Button></div></Field>
                </FieldGroup>
                <DialogFooter><Button type="button" variant="outline" @click="boxOpen = false">{{ t('Cancel') }}</Button><Button type="submit" :disabled="busy">{{ t('Create') }}</Button></DialogFooter>
            </form>
        </DialogContent>
    </Dialog>

    <Dialog v-model:open="setOpen">
        <DialogContent class="sm:max-w-md">
            <form class="flex flex-col gap-4" @submit.prevent="saveSettings">
                <DialogHeader><DialogTitle>{{ t('Mail server settings') }}</DialogTitle><DialogDescription>{{ t('Used for the server\'s DNS checklist and webmail.') }}</DialogDescription></DialogHeader>
                <FieldGroup>
                    <Field><FieldLabel for="s-host">{{ t('Mail server hostname') }}</FieldLabel><Input id="s-host" v-model="s.hostname" placeholder="mail.example.com" required /></Field>
                    <Field><FieldLabel for="s-v4">{{ t('Server IPv4') }}</FieldLabel><Input id="s-v4" v-model="s.ipv4" placeholder="203.0.113.10" /></Field>
                    <Field><FieldLabel for="s-v6">{{ t('Server IPv6 (optional)') }}</FieldLabel><Input id="s-v6" v-model="s.ipv6" /></Field>
                    <Field><FieldLabel for="s-web">{{ t('Webmail address') }}</FieldLabel><Input id="s-web" v-model="s.webmail" placeholder="https://webmail.example.com" required /></Field>
                </FieldGroup>
                <DialogFooter><Button type="button" variant="outline" @click="setOpen = false">{{ t('Cancel') }}</Button><Button type="submit" :disabled="busy">{{ t('Save') }}</Button></DialogFooter>
            </form>
        </DialogContent>
    </Dialog>

    <SecretDialog v-model:open="secretOpen" :title="t('Mailbox created')" :description="t('The password is shown only now and is not stored in readable form.')" :rows="secret" />
    <ConfirmDialog v-model:open="rmDomainOpen" :title="t('Delete this mail domain?')" :description="`${rmDomain} and its mailboxes are removed.`" @confirm="delDomain" />
    <ConfirmDialog v-model:open="rmBoxOpen" :title="t('Delete this mailbox?')" :description="t('Its mail is moved aside, not erased.')" @confirm="delBox" />
</template>

<script setup lang="ts">
import { reactive, ref } from 'vue';
import { router } from '@inertiajs/vue3';
import { PlusIcon, PowerIcon, Trash2Icon } from '@lucide/vue';
import ConfirmDialog from '@/components/app/ConfirmDialog.vue';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card';
import { Input } from '@/components/ui/input';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select';
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table';
import { act, useBusy } from '@/composables/useActions';
import { useI18n } from '@/composables/useI18n';
import { rpc } from '@/composables/useRpc';

interface Rule { n: number; to: string; action: string; from: string; comment: string; v6: boolean }
interface Listener { proto: string; addr: string; port: number; process: string; public: boolean }
interface Firewall { installed: boolean; active: boolean; default_in: string; default_out: string; rules: Rule[]; listening: Listener[]; ssh_port: number }
const props = defineProps<{ firewall: Firewall }>();
const { t } = useI18n();
const { busy, run } = useBusy();
const reload = () => router.reload({ only: ['firewall'], preserveScroll: true });

const form = reactive({ action: 'allow', port: '', proto: 'tcp', from: '', comment: '' });
async function add() {
    const r = await run(() => act(rpc('system', 'addRule', { ...form, port: form.port.trim(), from: form.from.trim() }), t('Rule added')));
    if (r.ok) { Object.assign(form, { port: '', from: '', comment: '' }); reload(); }
}
const delOpen = ref(false);
const del = ref<Rule | null>(null);
async function remove() {
    if (!del.value) return;
    const r = await act(rpc('system', 'deleteRule', del.value.n, `${del.value.to}|${del.value.from}`), t('Rule removed'));
    if (r.ok) reload(); else reload();
}
const toggleOpen = ref(false);
async function toggle() {
    const r = await run(() => act(rpc('system', props.firewall.active ? 'disableFirewall' : 'enableFirewall'), props.firewall.active ? t('Firewall disabled') : t('Firewall enabled')));
    if (r.ok) reload();
}
const tone = (a: string) => (a === 'ALLOW' ? 'border-success/30 bg-success/10 text-success' : a === 'LIMIT' ? 'border-warning/30 bg-warning/10 text-warning' : 'border-destructive/30 bg-destructive/10 text-destructive');
const covered = (l: Listener) => props.firewall.rules.some((r) => !r.v6 && r.to.split('/')[0] === String(l.port));
</script>

<template>
    <div class="flex flex-col gap-4">
        <Card class="py-4"><CardContent class="flex flex-wrap items-center justify-between gap-4 px-5">
            <div class="flex items-center gap-3">
                <span class="size-2.5 rounded-full" :class="firewall.active ? 'bg-success' : 'bg-warning'" aria-hidden="true" />
                <div>
                    <p class="font-semibold">{{ firewall.active ? t('Firewall is on') : t('Firewall is off') }}</p>
                    <p class="text-muted-foreground text-sm">{{ t('Incoming') }}: <b class="text-foreground">{{ t(firewall.default_in) }}</b> · {{ t('Outgoing') }}: <b class="text-foreground">{{ t(firewall.default_out) }}</b> · SSH {{ firewall.ssh_port }}</p>
                </div>
            </div>
            <Button :variant="firewall.active ? 'outline' : 'default'" :disabled="busy" @click="firewall.active ? (toggleOpen = true) : toggle()"><PowerIcon /> {{ firewall.active ? t('Turn off') : t('Turn on') }}</Button>
        </CardContent></Card>
        <p v-if="!firewall.active" class="text-muted-foreground -mt-1 text-sm">{{ t('Turning it on first allows SSH, HTTP and HTTPS so you cannot lock yourself out. Everything else incoming is blocked unless you add a rule.') }}</p>

        <Card>
            <CardHeader class="px-5"><CardTitle>{{ t('Add a rule') }}</CardTitle><CardDescription>{{ t('Open a port for everyone, or only for one address or network.') }}</CardDescription></CardHeader>
            <CardContent class="px-5">
                <form class="grid gap-3 sm:grid-cols-2 lg:grid-cols-[8rem_1fr_7rem_1.2fr_1.2fr_auto] lg:items-end" @submit.prevent="add">
                    <label class="grid gap-1.5 text-sm font-medium">{{ t('Action') }}
                        <Select v-model="form.action"><SelectTrigger class="w-full"><SelectValue /></SelectTrigger>
                            <SelectContent><SelectItem value="allow">{{ t('Allow') }}</SelectItem><SelectItem value="deny">{{ t('Deny') }}</SelectItem><SelectItem value="reject">{{ t('Reject') }}</SelectItem><SelectItem value="limit">{{ t('Rate limit') }}</SelectItem></SelectContent></Select>
                    </label>
                    <label class="grid gap-1.5 text-sm font-medium">{{ t('Port or range') }}<Input v-model="form.port" required inputmode="numeric" placeholder="25565 / 8000:8100" autocomplete="off" /></label>
                    <label class="grid gap-1.5 text-sm font-medium">{{ t('Protocol') }}
                        <Select v-model="form.proto"><SelectTrigger class="w-full"><SelectValue /></SelectTrigger>
                            <SelectContent><SelectItem value="tcp">TCP</SelectItem><SelectItem value="udp">UDP</SelectItem><SelectItem value="any">{{ t('Both') }}</SelectItem></SelectContent></Select>
                    </label>
                    <label class="grid gap-1.5 text-sm font-medium">{{ t('From (optional)') }}<Input v-model="form.from" placeholder="203.0.113.7 / 10.0.0.0/8" autocomplete="off" /></label>
                    <label class="grid gap-1.5 text-sm font-medium">{{ t('Note (optional)') }}<Input v-model="form.comment" maxlength="60" :placeholder="t('Minecraft server')" autocomplete="off" /></label>
                    <Button type="submit" :disabled="busy || !form.port"><PlusIcon /> {{ t('Add') }}</Button>
                </form>
            </CardContent>
        </Card>

        <Card class="gap-0 py-0">
            <CardHeader class="border-b px-5 py-4"><CardTitle>{{ t('Rules') }}</CardTitle></CardHeader>
            <p v-if="!firewall.rules.length" class="text-muted-foreground px-5 py-8 text-center text-sm">{{ t('No rules yet.') }}</p>
            <Table v-else>
                <TableHeader><TableRow><TableHead class="w-12 px-5">#</TableHead><TableHead>{{ t('To') }}</TableHead><TableHead>{{ t('Action') }}</TableHead><TableHead>{{ t('From') }}</TableHead><TableHead>{{ t('Note') }}</TableHead><TableHead class="w-12" /></TableRow></TableHeader>
                <TableBody>
                    <TableRow v-for="r in firewall.rules" :key="r.n">
                        <TableCell class="text-muted-foreground tabular px-5">{{ r.n }}</TableCell>
                        <TableCell class="font-mono text-sm">{{ r.to }}<Badge v-if="r.v6" variant="outline" class="text-muted-foreground ml-2">IPv6</Badge></TableCell>
                        <TableCell><Badge variant="outline" :class="tone(r.action)">{{ t(r.action) }}</Badge></TableCell>
                        <TableCell class="text-muted-foreground">{{ r.from === 'Anywhere' ? t('Anywhere') : r.from }}</TableCell>
                        <TableCell class="text-muted-foreground">{{ r.comment }}</TableCell>
                        <TableCell><Button variant="ghost" size="icon-sm" class="text-muted-foreground hover:text-destructive" :aria-label="t('Remove')" @click="del = r; delOpen = true"><Trash2Icon /></Button></TableCell>
                    </TableRow>
                </TableBody>
            </Table>
        </Card>

        <Card class="gap-0 py-0">
            <CardHeader class="border-b px-5 py-4"><CardTitle>{{ t('Listening on the network') }}</CardTitle><CardDescription>{{ t('Programs that accept connections. Public ones can be reached from the internet unless the firewall blocks them.') }}</CardDescription></CardHeader>
            <Table>
                <TableHeader><TableRow><TableHead class="px-5">{{ t('Port') }}</TableHead><TableHead>{{ t('Protocol') }}</TableHead><TableHead>{{ t('Program') }}</TableHead><TableHead>{{ t('Address') }}</TableHead><TableHead class="px-5 text-right">{{ t('Access') }}</TableHead></TableRow></TableHeader>
                <TableBody>
                    <TableRow v-for="l in firewall.listening" :key="l.proto + l.addr + l.port">
                        <TableCell class="tabular px-5 font-mono font-medium">{{ l.port }}</TableCell>
                        <TableCell class="uppercase">{{ l.proto }}</TableCell>
                        <TableCell>{{ l.process || '–' }}</TableCell>
                        <TableCell class="text-muted-foreground font-mono text-sm">{{ l.addr }}</TableCell>
                        <TableCell class="px-5 text-right">
                            <Badge v-if="!l.public" variant="outline" class="text-muted-foreground">{{ t('Local only') }}</Badge>
                            <Badge v-else-if="firewall.active && !covered(l)" variant="outline" class="border-success/30 bg-success/10 text-success">{{ t('Blocked by firewall') }}</Badge>
                            <Badge v-else-if="firewall.active" variant="outline" class="border-primary/30 bg-primary/10 text-primary">{{ t('Open') }}</Badge>
                            <Badge v-else variant="outline" class="border-warning/30 bg-warning/10 text-warning">{{ t('Public') }}</Badge>
                        </TableCell>
                    </TableRow>
                </TableBody>
            </Table>
        </Card>
    </div>

    <ConfirmDialog v-model:open="delOpen" :title="t('Remove this rule?')" :description="del ? `${del.action} ${del.to} ← ${del.from}` : ''" :confirm-label="t('Remove')" @confirm="remove" />
    <ConfirmDialog v-model:open="toggleOpen" :title="t('Turn the firewall off?')" :description="t('All ports will be reachable from the internet again.')" :confirm-label="t('Turn off')" @confirm="toggle" />
</template>

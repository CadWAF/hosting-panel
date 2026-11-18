<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch } from 'vue';
import { router } from '@inertiajs/vue3';
import { ActivityIcon, DownloadIcon, ExternalLinkIcon, PlusIcon, RefreshCwIcon, ShieldAlertIcon, Trash2Icon } from '@lucide/vue';
import ConfirmDialog from '@/components/app/ConfirmDialog.vue';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card';
import { Field, FieldLabel } from '@/components/ui/field';
import { Input } from '@/components/ui/input';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select';
import { Switch } from '@/components/ui/switch';
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table';
import { act, useBusy } from '@/composables/useActions';
import { useI18n } from '@/composables/useI18n';
import { rpc } from '@/composables/useRpc';
import { compact } from '@/lib/format';

interface Cw {
    bundled: boolean; installed: boolean; active: boolean; attached: boolean; status: string; version: string; iface: string; egress: boolean;
    ports: { tcp_in: number[]; udp_in: number[]; tcp_out: number[]; udp_out: number[]; allow_localhost: boolean };
    whitelist: string[]; blocklist: string[]; rules: { id: number; rule: string; action: string; file: string }[];
    stats: { xdp_pass: number; xdp_drop: number; tc_pass: number; tc_drop: number };
    drops: { ip: string; reason: string; proto: string; packets: number; last_dst_port: number; last_seen: string }[];
    ufw_active: boolean; ssh_port: number; error?: string;
}
const props = defineProps<{ cadwaf: Cw; clientIp: string; busyTask: boolean }>();
const emit = defineEmits<{ started: [taskId: string] }>();
const { t } = useI18n();
const { busy, run } = useBusy();
const reload = () => router.reload({ only: ['cadwaf'], preserveScroll: true });
const c = computed(() => props.cadwaf);

// ---- install ----
const allowMe = ref(true);
const installOpen = ref(false);
async function install() {
    const r = await run(() => act(rpc('system', 'installCadwaf', allowMe.value ? props.clientIp : '')));
    if (r.ok) emit('started', r.data?.id);
}
// ---- updates (published releases of the open-source firewall) ----
interface Rel { current: string; latest: string; available: boolean; notes: string; url: string; error?: string }
const rel = ref<Rel | null>(null);
const checking = ref(false);
const updateOpen = ref(false);
async function checkRelease(refresh = false) {
    checking.value = true;
    const r = await rpc('system', 'cadwafRelease', refresh);
    checking.value = false;
    if (r.ok) rel.value = r.data as Rel;
}
onMounted(() => { if (props.cadwaf.installed) checkRelease(); });
async function update() {
    const r = await run(() => act(rpc('system', 'updateCadwaf')));
    if (r.ok) emit('started', r.data?.id);
}
const removeOpen = ref(false);
async function remove() {
    const r = await act(rpc('system', 'removeCadwaf'));
    if (r.ok) emit('started', r.data?.id);
}

// ---- service ----
const detachOpen = ref(false);
async function service(a: string, msg: string) {
    if ((await run(() => act(rpc('system', 'cadwafService', a), msg))).ok) reload();
}
async function doReload() {
    if ((await run(() => act(rpc('system', 'reloadCadwaf'), t('Rules reloaded')))).ok) reload();
}
const egressOpen = ref(false);
const egressNext = ref(false);
async function setEgress() {
    if ((await run(() => act(rpc('system', 'setCadwafEgress', egressNext.value), egressNext.value ? t('Outbound filtering on') : t('Outbound filtering off')))).ok) reload();
}

// ---- ports ----
const fields = reactive({ tcp_in: '', udp_in: '', tcp_out: '', udp_out: '', allow_localhost: true });
watch(() => props.cadwaf.ports, (p) => {
    if (!p) return;
    Object.assign(fields, { tcp_in: compact(p.tcp_in), udp_in: compact(p.udp_in), tcp_out: compact(p.tcp_out), udp_out: compact(p.udp_out), allow_localhost: p.allow_localhost });
}, { immediate: true, deep: true });

/** "22, 80-443, 2000_3000" -> sorted unique port numbers (throws on garbage). */
function parse(s: string): number[] {
    const out = new Set<number>();
    for (const tok of s.split(/[\s,]+/).filter(Boolean)) {
        const m = tok.match(/^(\d{1,5})(?:[-_](\d{1,5}))?$/);
        if (!m) throw new Error(tok);
        const a = Number(m[1]), b = m[2] ? Number(m[2]) : a;
        if (a < 1 || b > 65535 || b < a) throw new Error(tok);
        for (let p = a; p <= b; p++) out.add(p);
        if (out.size > 4096) throw new Error(t('too many ports'));
    }
    return [...out].sort((x, y) => x - y);
}
const portErr = ref('');
async function savePorts() {
    portErr.value = '';
    let body;
    try {
        body = { tcp_in: parse(fields.tcp_in), udp_in: parse(fields.udp_in), tcp_out: parse(fields.tcp_out), udp_out: parse(fields.udp_out), allow_localhost: fields.allow_localhost };
    } catch (e: any) {
        portErr.value = t('Not a valid port or range: {v}', { v: e.message });
        return;
    }
    if ((await run(() => act(rpc('system', 'setCadwafPorts', body), t('Ports saved')))).ok) reload();
}

// ---- allow / block ----
const ip = reactive({ allow: '', block: '' });
async function addIp(list: 'allow' | 'block') {
    const v = ip[list].trim();
    if (v && (await run(() => act(rpc('system', 'addCadwafIp', list, v), t('Address added')))).ok) { ip[list] = ''; reload(); }
}
async function delIp(list: 'allow' | 'block', v: string) {
    if ((await act(rpc('system', 'deleteCadwafIp', list, v), t('Address removed'))).ok) reload();
}
async function blockDrop(v: string) {
    if ((await act(rpc('system', 'addCadwafIp', 'block', v), t('Address blocked'))).ok) reload();
}

// ---- complex rules ----
const rf = reactive({ file: 'allow', proto: 'tcp', dir: 'in', port: '', ip: '' });
function ruleText() {
    const parts = [rf.proto, rf.dir];
    if (rf.port.trim()) parts.push('d=' + rf.port.trim().replace(/\s+/g, ''));
    if (rf.ip.trim()) parts.push('s=' + rf.ip.trim());
    return parts.join('|');
}
async function addRule() {
    if (!rf.port.trim() && !rf.ip.trim()) return;
    if ((await run(() => act(rpc('system', 'addCadwafRule', ruleText(), rf.file), t('Rule added')))).ok) { Object.assign(rf, { port: '', ip: '' }); reload(); }
}
async function delRule(r: Cw['rules'][number]) {
    if ((await act(rpc('system', 'deleteCadwafRule', r.id, r.file), t('Rule removed'))).ok) reload();
}

const when = (s: string) => new Date(s).toLocaleString();
const reasons: Record<string, string> = { port_not_allowed: 'Port not allowed', blocklist: 'Blocked', not_whitelisted: 'Not allow-listed' };
const num = (n: number) => n.toLocaleString();
</script>

<template>
    <!-- Not installed -->
    <Card v-if="!c.installed" class="border-dashed">
        <CardHeader>
            <CardTitle class="flex items-center gap-2"><ShieldAlertIcon class="size-4" /> {{ t('CadWAF Firewall is not installed') }}</CardTitle>
            <CardDescription>{{ t('A kernel-level (eBPF/XDP) firewall that drops unwanted traffic before it reaches your services: default-deny, port rules, allow and block lists, and a record of what was refused.') }}</CardDescription>
        </CardHeader>
        <CardContent class="flex flex-col gap-4">
            <ul class="text-muted-foreground list-disc space-y-1 pl-5 text-sm">
                <li>{{ t('Ports already in use (SSH {ssh}, 80, 443 and anything listening publicly) stay open.', { ssh: c.ssh_port }) }}</li>
                <li>{{ t('Outbound filtering starts off, so updates and mail keep working.') }}</li>
                <li>{{ t('Everything else from the internet is dropped. Docker ports you publish later must be opened here.') }}</li>
            </ul>
            <Field v-if="clientIp" orientation="horizontal"><Switch id="cw-me" v-model="allowMe" /><FieldLabel for="cw-me">{{ t('Always allow my address') }} ({{ clientIp }})</FieldLabel></Field>
            <p v-if="c.ufw_active" class="text-warning text-sm" role="alert">{{ t('UFW is switched on. Both firewalls would filter. Turn UFW off once you have checked that CadWAF works.') }}</p>
            <p class="text-muted-foreground text-xs">{{ t('The newest published release is downloaded from github.com/CadWAF/firewall and its checksum is verified.') }}</p>
            <div><Button :disabled="busyTask || busy" @click="installOpen = true"><DownloadIcon /> {{ t('Install CadWAF Firewall') }}</Button></div>
        </CardContent>
    </Card>

    <div v-else class="flex flex-col gap-4">
        <Card class="py-4"><CardContent class="flex flex-wrap items-center justify-between gap-4 px-5">
            <div class="flex items-center gap-3">
                <span class="size-2.5 rounded-full" :class="c.active && c.attached && c.status === 'ok' ? 'bg-success' : c.active ? 'bg-warning' : 'bg-destructive'" aria-hidden="true" />
                <div>
                    <p class="font-semibold">{{ c.active ? (c.attached ? t('Filtering on {iface}', { iface: c.iface }) : t('Running, not attached')) : t('Stopped: nothing is filtered') }}</p>
                    <p class="text-muted-foreground text-sm">CadWAF {{ c.version }} · {{ t('inbound') }} {{ num(c.stats.xdp_pass) }} {{ t('passed') }} / {{ num(c.stats.xdp_drop) }} {{ t('dropped') }}<template v-if="c.egress"> · {{ t('outbound') }} {{ num(c.stats.tc_pass) }} / {{ num(c.stats.tc_drop) }}</template></p>
                </div>
            </div>
            <div class="flex flex-wrap gap-2">
                <Button v-if="!c.active" :disabled="busy" @click="service('start', t('Firewall started'))">{{ t('Start') }}</Button>
                <Button variant="outline" :disabled="busy || !c.active" @click="doReload"><RefreshCwIcon /> {{ t('Reload rules') }}</Button>
                <Button variant="outline" :disabled="busy" @click="service('restart', t('Firewall restarted'))">{{ t('Restart') }}</Button>
                <Button variant="outline" class="text-destructive" :disabled="busy || !c.active" @click="detachOpen = true">{{ t('Emergency off') }}</Button>
                <Button variant="outline" size="icon" class="text-destructive" :aria-label="t('Remove')" :disabled="busy || busyTask" @click="removeOpen = true"><Trash2Icon /></Button>
            </div>
        </CardContent></Card>
        <Card v-if="rel?.available" class="border-primary/40 py-4"><CardContent class="flex flex-wrap items-center justify-between gap-3 px-5">
            <div>
                <p class="font-semibold">{{ t('Version {version} is available', { version: rel.latest }) }}</p>
                <p class="text-muted-foreground text-sm">{{ t('Installed: {version}. The old version is restored automatically if the new one does not start.', { version: rel.current || '?' }) }}</p>
            </div>
            <div class="flex flex-wrap gap-2">
                <Button v-if="rel.url" variant="outline" as="a" :href="rel.url" target="_blank" rel="noopener noreferrer"><ExternalLinkIcon /> {{ t('Release notes') }}</Button>
                <Button :disabled="busy || busyTask" @click="updateOpen = true"><DownloadIcon /> {{ t('Update') }}</Button>
            </div>
        </CardContent></Card>
        <p v-else-if="rel && !rel.error" class="text-muted-foreground flex flex-wrap items-center gap-2 text-sm">{{ t('CadWAF is up to date ({version}).', { version: rel.current || rel.latest }) }} <Button variant="link" size="sm" class="h-auto p-0" :disabled="checking" @click="checkRelease(true)">{{ t('Check again') }}</Button></p>
        <p v-else-if="rel?.error" class="text-muted-foreground text-sm">{{ rel.error }} <Button variant="link" size="sm" class="h-auto p-0" :disabled="checking" @click="checkRelease(true)">{{ t('Check again') }}</Button></p>
        <p v-if="c.error" class="text-destructive text-sm" role="alert">{{ c.error }}</p>
        <p v-if="c.status === 'degraded'" class="text-warning text-sm" role="alert">{{ t('The firewall reports its rule tables are out of step. Use Reload rules.') }}</p>
        <p v-if="c.ufw_active" class="text-warning text-sm" role="alert">{{ t('UFW is also switched on. Both firewalls filter traffic; turn one off in its tab.') }}</p>

        <Card>
            <CardHeader>
                <CardTitle>{{ t('Ports') }}</CardTitle>
                <CardDescription>{{ t('Comma-separated ports or ranges such as 80,443,40000-40100. Anything not listed is dropped.') }}</CardDescription>
            </CardHeader>
            <CardContent>
                <form class="grid gap-4 sm:grid-cols-2" @submit.prevent="savePorts">
                    <Field><FieldLabel for="cw-ti">{{ t('Inbound TCP') }}</FieldLabel><Input id="cw-ti" v-model="fields.tcp_in" class="font-mono" /></Field>
                    <Field><FieldLabel for="cw-ui">{{ t('Inbound UDP') }}</FieldLabel><Input id="cw-ui" v-model="fields.udp_in" class="font-mono" /></Field>
                    <Field><FieldLabel for="cw-to">{{ t('Outbound TCP') }}</FieldLabel><Input id="cw-to" v-model="fields.tcp_out" class="font-mono" :disabled="!c.egress" /></Field>
                    <Field><FieldLabel for="cw-uo">{{ t('Outbound UDP') }}</FieldLabel><Input id="cw-uo" v-model="fields.udp_out" class="font-mono" :disabled="!c.egress" /></Field>
                    <Field orientation="horizontal"><Switch id="cw-lo" v-model="fields.allow_localhost" /><FieldLabel for="cw-lo">{{ t('Always allow loopback') }}</FieldLabel></Field>
                    <Field orientation="horizontal"><Switch id="cw-eg" :model-value="c.egress" @update:model-value="(v) => { egressNext = !!v; egressOpen = true; }" /><FieldLabel for="cw-eg">{{ t('Filter outbound traffic too') }}</FieldLabel></Field>
                    <p v-if="portErr" role="alert" class="text-destructive text-sm sm:col-span-2">{{ portErr }}</p>
                    <div class="sm:col-span-2"><Button type="submit" :disabled="busy">{{ t('Save ports') }}</Button></div>
                </form>
            </CardContent>
        </Card>

        <div class="grid gap-4 lg:grid-cols-2">
            <Card v-for="l in (['allow', 'block'] as const)" :key="l">
                <CardHeader>
                    <CardTitle>{{ l === 'allow' ? t('Allow list') : t('Block list') }}</CardTitle>
                    <CardDescription>{{ l === 'allow' ? t('These addresses bypass every rule.') : t('These addresses are always dropped.') }}</CardDescription>
                </CardHeader>
                <CardContent class="flex flex-col gap-3">
                    <form class="flex gap-2" @submit.prevent="addIp(l)">
                        <Input v-model="ip[l]" class="font-mono" placeholder="203.0.113.7 or 198.51.100.0/24" :aria-label="l === 'allow' ? t('Allow list') : t('Block list')" />
                        <Button type="submit" variant="outline" :disabled="busy || !ip[l].trim()"><PlusIcon /> {{ t('Add') }}</Button>
                    </form>
                    <ul v-if="(l === 'allow' ? c.whitelist : c.blocklist).length" class="divide-y rounded-md border">
                        <li v-for="a in (l === 'allow' ? c.whitelist : c.blocklist)" :key="a" class="flex items-center justify-between px-3 py-1.5">
                            <span class="font-mono text-sm">{{ a }}<Badge v-if="a === clientIp" variant="outline" class="ml-2">{{ t('you') }}</Badge></span>
                            <Button variant="ghost" size="icon-sm" class="text-destructive" :aria-label="`${t('Remove')} ${a}`" @click="delIp(l, a)"><Trash2Icon /></Button>
                        </li>
                    </ul>
                    <p v-else class="text-muted-foreground text-sm">{{ t('Empty') }}</p>
                </CardContent>
            </Card>
        </div>

        <Card>
            <CardHeader>
                <CardTitle>{{ t('Advanced rules') }}</CardTitle>
                <CardDescription>{{ t('Match a protocol, port and source together, for example allow MySQL only from one address. Up to 64 rules.') }}</CardDescription>
            </CardHeader>
            <CardContent class="flex flex-col gap-4">
                <form class="grid gap-3 sm:grid-cols-[7rem_6rem_6rem_1fr_1fr_auto]" @submit.prevent="addRule">
                    <Field><FieldLabel>{{ t('Action') }}</FieldLabel><Select v-model="rf.file"><SelectTrigger class="w-full"><SelectValue /></SelectTrigger><SelectContent><SelectItem value="allow">{{ t('Allow') }}</SelectItem><SelectItem value="deny">{{ t('Drop') }}</SelectItem></SelectContent></Select></Field>
                    <Field><FieldLabel>{{ t('Protocol') }}</FieldLabel><Select v-model="rf.proto"><SelectTrigger class="w-full"><SelectValue /></SelectTrigger><SelectContent><SelectItem value="tcp">TCP</SelectItem><SelectItem value="udp">UDP</SelectItem><SelectItem value="icmp">ICMP</SelectItem></SelectContent></Select></Field>
                    <Field><FieldLabel>{{ t('Direction') }}</FieldLabel><Select v-model="rf.dir"><SelectTrigger class="w-full"><SelectValue /></SelectTrigger><SelectContent><SelectItem value="in">{{ t('In') }}</SelectItem><SelectItem value="out">{{ t('Out') }}</SelectItem></SelectContent></Select></Field>
                    <Field><FieldLabel for="rf-port">{{ t('Port') }}</FieldLabel><Input id="rf-port" v-model="rf.port" class="font-mono" placeholder="3306 or 22,80,443" /></Field>
                    <Field><FieldLabel for="rf-ip">{{ t('Address') }}</FieldLabel><Input id="rf-ip" v-model="rf.ip" class="font-mono" placeholder="203.0.113.5" /></Field>
                    <Field class="justify-end"><Button type="submit" variant="outline" :disabled="busy || (!rf.port.trim() && !rf.ip.trim())"><PlusIcon /> {{ t('Add') }}</Button></Field>
                </form>
                <p class="text-muted-foreground font-mono text-xs">{{ ruleText() }}</p>
                <Table v-if="c.rules.length">
                    <TableHeader><TableRow><TableHead>{{ t('Rule') }}</TableHead><TableHead>{{ t('Action') }}</TableHead><TableHead class="text-right">{{ t('Actions') }}</TableHead></TableRow></TableHeader>
                    <TableBody>
                        <TableRow v-for="r in c.rules" :key="r.file + r.id">
                            <TableCell class="font-mono text-sm">{{ r.rule }}</TableCell>
                            <TableCell><Badge :variant="r.file === 'deny' ? 'destructive' : 'secondary'">{{ r.file === 'deny' ? t('Drop') : t('Allow') }}</Badge></TableCell>
                            <TableCell class="text-right"><Button variant="ghost" size="icon-sm" class="text-destructive" :aria-label="t('Remove')" @click="delRule(r)"><Trash2Icon /></Button></TableCell>
                        </TableRow>
                    </TableBody>
                </Table>
            </CardContent>
        </Card>

        <Card class="gap-0 py-0">
            <CardHeader class="border-b py-4">
                <CardTitle class="flex items-center gap-2"><ActivityIcon class="size-4" /> {{ t('Recently refused (24 h)') }}</CardTitle>
            </CardHeader>
            <Table v-if="c.drops.length">
                <TableHeader><TableRow><TableHead class="px-5">{{ t('Address') }}</TableHead><TableHead>{{ t('Reason') }}</TableHead><TableHead>{{ t('Port') }}</TableHead><TableHead class="text-right">{{ t('Packets') }}</TableHead><TableHead>{{ t('Last seen') }}</TableHead><TableHead class="px-5 text-right" /></TableRow></TableHeader>
                <TableBody>
                    <TableRow v-for="d in c.drops" :key="d.ip + d.proto">
                        <TableCell class="px-5 font-mono text-sm">{{ d.ip }}</TableCell>
                        <TableCell>{{ t(reasons[d.reason] ?? d.reason) }}</TableCell>
                        <TableCell class="font-mono text-sm">{{ d.proto }}/{{ d.last_dst_port }}</TableCell>
                        <TableCell class="tabular text-right">{{ num(d.packets) }}</TableCell>
                        <TableCell class="text-muted-foreground text-sm">{{ when(d.last_seen) }}</TableCell>
                        <TableCell class="px-5 text-right"><Button v-if="!c.blocklist.includes(d.ip)" variant="outline" size="sm" @click="blockDrop(d.ip)">{{ t('Block') }}</Button></TableCell>
                    </TableRow>
                </TableBody>
            </Table>
            <p v-else class="text-muted-foreground p-5 text-sm">{{ t('Nothing refused recently.') }}</p>
        </Card>
    </div>

    <ConfirmDialog v-model:open="updateOpen" :title="t('Update CadWAF Firewall?')" :description="t('The firewall restarts for a moment. Keep a second SSH session open. The previous version is put back if the new one fails to start.')" :confirm-label="t('Update')" @confirm="update" />
    <ConfirmDialog v-model:open="installOpen" :title="t('Install CadWAF Firewall?')" :description="t('It starts filtering right away. Keep a second SSH session open the first time. If you get locked out, the hosting provider console can run the recovery script.')" :confirm-label="t('Install')" @confirm="install" />
    <ConfirmDialog v-model:open="detachOpen" :title="t('Switch the firewall off?')" :description="t('The firewall stops and its filters are removed, so all ports are reachable until you start it again.')" :confirm-label="t('Switch off')" @confirm="service('detach', t('Firewall switched off'))" />
    <ConfirmDialog v-model:open="removeOpen" :title="t('Remove CadWAF Firewall?')" :description="t('The program is removed and filtering stops. Your allow, block and port lists are kept.')" :confirm-label="t('Remove')" @confirm="remove" />
    <ConfirmDialog v-model:open="egressOpen" :title="egressNext ? t('Filter outbound traffic?') : t('Stop filtering outbound traffic?')" :description="egressNext ? t('Only the outbound ports listed will work: updates, mail, backups to remote servers and API calls can break if their ports are missing.') : t('Outbound connections are no longer restricted.')" :confirm-label="t('Confirm')" @confirm="setEgress" />
</template>

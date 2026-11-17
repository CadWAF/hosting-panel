<script setup lang="ts">
import { reactive, ref, watch } from 'vue';
import { router } from '@inertiajs/vue3';
import { BanIcon, ChevronDownIcon, XIcon } from '@lucide/vue';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card';
import { Input } from '@/components/ui/input';
import { Switch } from '@/components/ui/switch';
import { act, useBusy } from '@/composables/useActions';
import { useI18n } from '@/composables/useI18n';
import { rpc } from '@/composables/useRpc';

interface Jail { name: string; description: string; enabled: boolean; failed: number; total_failed: number; banned: number; total_banned: number; ips: string[]; bantime: number; findtime: number; maxretry: number }
interface F2b { installed: boolean; running: boolean; jails: Jail[]; defaults: { bantime: number; findtime: number; maxretry: number }; ignoreip: string[] }
const props = defineProps<{ fail2ban: F2b }>();
const { t } = useI18n();
const { busy, run } = useBusy();
const reload = () => router.reload({ only: ['fail2ban'], preserveScroll: true });

// Durations are shown in minutes and stored in seconds.
const defs = reactive({ bantime: 0, findtime: 0, maxretry: 0, ignore: '' });
watch(() => props.fail2ban, (f) => {
    defs.bantime = Math.round(f.defaults.bantime / 60);
    defs.findtime = Math.round(f.defaults.findtime / 60);
    defs.maxretry = f.defaults.maxretry;
    defs.ignore = f.ignoreip.join(', ');
}, { immediate: true });
async function saveDefaults() {
    const ips = defs.ignore.split(/[\s,]+/).filter(Boolean);
    const r = await run(() => act(rpc('system', 'setF2bDefaults', { bantime: defs.bantime * 60, findtime: defs.findtime * 60, maxretry: defs.maxretry, ignoreip: ips }), t('Saved')));
    if (r.ok) reload();
}

const open = ref<string | null>(null);
const edit = reactive<Record<string, { bantime: number; findtime: number; maxretry: number }>>({});
function toggleOpen(j: Jail) {
    if (open.value === j.name) { open.value = null; return; }
    edit[j.name] = { bantime: Math.round(j.bantime / 60), findtime: Math.round(j.findtime / 60), maxretry: j.maxretry };
    open.value = j.name;
}
async function setJail(j: Jail, enabled: boolean, e = { bantime: Math.round(j.bantime / 60), findtime: Math.round(j.findtime / 60), maxretry: j.maxretry }) {
    const r = await run(() => act(rpc('system', 'setJail', j.name, { enabled, bantime: e.bantime * 60, findtime: e.findtime * 60, maxretry: e.maxretry }), t('Saved')));
    if (r.ok) reload();
}
const banIp = reactive<Record<string, string>>({});
async function ban(j: Jail) {
    const ip = (banIp[j.name] ?? '').trim();
    if (!ip) return;
    const r = await run(() => act(rpc('system', 'ban', j.name, ip), t('Banned {ip}', { ip })));
    if (r.ok) { banIp[j.name] = ''; reload(); }
}
async function unban(j: Jail, ip: string) {
    const r = await run(() => act(rpc('system', 'unban', j.name, ip), t('Unbanned {ip}', { ip })));
    if (r.ok) reload();
}
const mins = (m: number) => (m >= 1440 && m % 1440 === 0 ? `${m / 1440} ${t('d')}` : m >= 60 && m % 60 === 0 ? `${m / 60} ${t('h')}` : `${m} ${t('min')}`);
</script>

<template>
    <div class="flex flex-col gap-4">
        <Card v-if="!fail2ban.running" class="border-warning/40"><CardContent class="px-5 text-sm">{{ t('Fail2ban is installed but not running. Start it from Software or Services.') }}</CardContent></Card>

        <Card>
            <CardHeader class="px-5"><CardTitle>{{ t('Defaults for all jails') }}</CardTitle><CardDescription>{{ t('An address that fails {n} times within the window is banned for the ban time.', { n: defs.maxretry }) }}</CardDescription></CardHeader>
            <CardContent class="px-5">
                <form class="grid gap-3 sm:grid-cols-3 lg:grid-cols-[8rem_8rem_8rem_1fr_auto] lg:items-end" @submit.prevent="saveDefaults">
                    <label class="grid gap-1.5 text-sm font-medium">{{ t('Ban time (min)') }}<Input v-model.number="defs.bantime" type="number" min="1" /></label>
                    <label class="grid gap-1.5 text-sm font-medium">{{ t('Window (min)') }}<Input v-model.number="defs.findtime" type="number" min="1" /></label>
                    <label class="grid gap-1.5 text-sm font-medium">{{ t('Max failures') }}<Input v-model.number="defs.maxretry" type="number" min="1" max="100" /></label>
                    <label class="grid gap-1.5 text-sm font-medium sm:col-span-3 lg:col-span-1">{{ t('Never ban') }}<Input v-model="defs.ignore" placeholder="203.0.113.7, 10.0.0.0/8" autocomplete="off" /></label>
                    <Button type="submit" :disabled="busy">{{ t('Save') }}</Button>
                </form>
            </CardContent>
        </Card>

        <ul class="flex flex-col gap-3">
            <li v-for="j in fail2ban.jails" :key="j.name">
                <Card class="gap-0 py-0">
                    <div class="flex flex-wrap items-center gap-3 px-5 py-4">
                        <Switch :model-value="j.enabled" :disabled="busy" :aria-label="j.name" @update:model-value="(v: boolean) => setJail(j, v)" />
                        <div class="min-w-0 flex-1">
                            <p class="font-semibold">{{ j.name }}</p>
                            <p class="text-muted-foreground text-sm">{{ t(j.description) }}</p>
                        </div>
                        <div v-if="j.enabled" class="tabular text-muted-foreground flex gap-5 text-sm">
                            <span><b class="text-foreground">{{ j.banned }}</b> {{ t('banned now') }}</span>
                            <span class="hidden sm:inline"><b class="text-foreground">{{ j.total_banned }}</b> {{ t('banned in total') }}</span>
                            <span class="hidden md:inline"><b class="text-foreground">{{ j.total_failed }}</b> {{ t('failed attempts') }}</span>
                        </div>
                        <Button variant="ghost" size="sm" :aria-expanded="open === j.name" @click="toggleOpen(j)">{{ t('Settings') }}<ChevronDownIcon class="transition-transform" :class="{ 'rotate-180': open === j.name }" /></Button>
                    </div>
                    <div v-if="open === j.name" class="bg-muted/40 grid gap-3 border-t px-5 py-4 sm:grid-cols-4 sm:items-end">
                        <label class="grid gap-1.5 text-sm font-medium">{{ t('Ban time (min)') }}<Input v-model.number="edit[j.name].bantime" type="number" min="1" /></label>
                        <label class="grid gap-1.5 text-sm font-medium">{{ t('Window (min)') }}<Input v-model.number="edit[j.name].findtime" type="number" min="1" /></label>
                        <label class="grid gap-1.5 text-sm font-medium">{{ t('Max failures') }}<Input v-model.number="edit[j.name].maxretry" type="number" min="1" max="100" /></label>
                        <Button :disabled="busy" @click="setJail(j, j.enabled, edit[j.name])">{{ t('Apply') }}</Button>
                    </div>
                    <div v-if="j.enabled" class="border-t px-5 py-3">
                        <div class="flex flex-wrap items-center gap-2">
                            <span class="text-muted-foreground text-xs">{{ mins(Math.round(j.bantime / 60)) }} · {{ j.maxretry }}× / {{ mins(Math.round(j.findtime / 60)) }}</span>
                            <Badge v-for="ip in j.ips" :key="ip" variant="outline" class="gap-1 pr-1 font-mono">
                                {{ ip }}
                                <button type="button" class="hover:bg-destructive/15 hover:text-destructive grid size-4 place-items-center rounded-sm" :aria-label="t('Unban {ip}', { ip })" :disabled="busy" @click="unban(j, ip)"><XIcon class="size-3" /></button>
                            </Badge>
                            <span v-if="!j.ips.length" class="text-muted-foreground text-sm">{{ t('No banned addresses.') }}</span>
                            <form class="ml-auto flex items-center gap-2" @submit.prevent="ban(j)">
                                <Input v-model="banIp[j.name]" class="h-8 w-44 font-mono" placeholder="198.51.100.4" :aria-label="t('Address to ban')" autocomplete="off" />
                                <Button type="submit" size="sm" variant="outline" :disabled="busy || !banIp[j.name]"><BanIcon /> {{ t('Ban') }}</Button>
                            </form>
                        </div>
                    </div>
                </Card>
            </li>
        </ul>
    </div>
</template>

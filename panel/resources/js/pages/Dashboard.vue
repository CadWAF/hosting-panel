<script setup lang="ts">
import { computed } from 'vue';
import { Head, Link, router } from '@inertiajs/vue3';
import { ArrowDownIcon, ArrowUpIcon, CpuIcon, DatabaseIcon, GlobeIcon, HardDriveIcon, MailIcon, MemoryStickIcon } from '@lucide/vue';
import AreaChart from '@/components/app/AreaChart.vue';
import Meter from '@/components/app/Meter.vue';
import PageHeader from '@/components/app/PageHeader.vue';
import ServiceDown from '@/components/app/ServiceDown.vue';
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card';
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table';
import { useI18n } from '@/composables/useI18n';
import { useInterval } from '@/composables/useTask';
import { bytes, duration, rate } from '@/lib/format';

interface Point { t: number; cpu: number; mem: number; load1: number; net_rx: number; net_tx: number; disk_rd: number; disk_wr: number }
interface Stats {
    hostname: string; os: string; kernel: string; cores: number; uptime: number; load: number[]; now: Point;
    mem_total: number; mem_used: number; swap_total: number; swap_used: number;
    disks: { mount: string; fs: string; total: number; used: number }[];
    procs: { pid: number; name: string; user: string; cpu: number; mem: number }[];
    history: Point[];
}
const props = defineProps<{ stats: Stats | null; counts: { sites: number | null; databases: number | null; domains: number | null }; available: boolean }>();
const { t } = useI18n();
useInterval(() => router.reload({ only: ['stats'], preserveScroll: true, preserveState: true }), 5000);

const h = computed(() => props.stats?.history ?? []);
const times = computed(() => h.value.map((p) => p.t));
const memPct = computed(() => (props.stats?.mem_total ? (props.stats.mem_used / props.stats.mem_total) * 100 : 0));
const root = computed(() => props.stats?.disks.find((d) => d.mount === '/') ?? props.stats?.disks[0]);
const rootPct = computed(() => (root.value?.total ? (root.value.used / root.value.total) * 100 : 0));
const pct = (v: number) => `${v.toFixed(1)}%`;
const tiles = computed(() => [
    { label: 'Sites', n: props.counts.sites, href: '/sites', icon: GlobeIcon },
    { label: 'Databases', n: props.counts.databases, href: '/databases', icon: DatabaseIcon },
    { label: 'Mail domains', n: props.counts.domains, href: '/mail', icon: MailIcon },
]);
</script>

<template>
    <Head :title="t('Dashboard')" />
    <PageHeader :title="stats?.hostname || t('Dashboard')" :description="stats ? `${stats.os} · Linux ${stats.kernel}` : t('Server overview')">
        <div v-if="stats" class="text-muted-foreground tabular flex flex-wrap gap-x-4 gap-y-1 text-sm">
            <span>{{ t('Uptime') }} <b class="text-foreground font-medium">{{ duration(stats.uptime) }}</b></span>
            <span>{{ t('Load') }} <b class="text-foreground font-medium">{{ stats.load.map((l) => l.toFixed(2)).join(' · ') }}</b></span>
        </div>
    </PageHeader>

    <ServiceDown v-if="!available" name="System" />

    <template v-if="stats">
        <section class="grid gap-4 sm:grid-cols-2 xl:grid-cols-4" :aria-label="t('Resources')">
            <Card class="py-4"><CardContent class="flex items-center gap-4 px-4">
                <Meter :value="stats.now.cpu" :label="t('CPU')" />
                <div class="min-w-0"><p class="text-muted-foreground flex items-center gap-1.5 text-sm"><CpuIcon class="size-3.5" /> {{ t('CPU') }}</p>
                    <p class="tabular text-xl font-semibold">{{ pct(stats.now.cpu) }}</p><p class="text-muted-foreground text-xs">{{ stats.cores }} {{ t('cores') }}</p></div>
            </CardContent></Card>
            <Card class="py-4"><CardContent class="flex items-center gap-4 px-4">
                <Meter :value="memPct" :label="t('Memory')" />
                <div class="min-w-0"><p class="text-muted-foreground flex items-center gap-1.5 text-sm"><MemoryStickIcon class="size-3.5" /> {{ t('Memory') }}</p>
                    <p class="tabular text-xl font-semibold">{{ bytes(stats.mem_used) }}</p>
                    <p class="text-muted-foreground tabular text-xs">{{ t('of') }} {{ bytes(stats.mem_total) }}<template v-if="stats.swap_total"> · swap {{ bytes(stats.swap_used) }}</template></p></div>
            </CardContent></Card>
            <Card class="py-4"><CardContent class="flex items-center gap-4 px-4">
                <Meter :value="rootPct" :label="t('Disk')" />
                <div class="min-w-0"><p class="text-muted-foreground flex items-center gap-1.5 text-sm"><HardDriveIcon class="size-3.5" /> {{ t('Disk') }} {{ root?.mount }}</p>
                    <p class="tabular text-xl font-semibold">{{ bytes(root?.used ?? 0) }}</p>
                    <p class="text-muted-foreground tabular text-xs">{{ t('of') }} {{ bytes(root?.total ?? 0) }}</p></div>
            </CardContent></Card>
            <Card class="py-4"><CardContent class="flex flex-col justify-center gap-1.5 px-4">
                <p class="text-muted-foreground text-sm">{{ t('Network') }}</p>
                <p class="tabular flex items-center gap-1.5 font-semibold"><ArrowDownIcon class="text-chart-2 size-4" /> {{ rate(stats.now.net_rx) }}</p>
                <p class="tabular flex items-center gap-1.5 font-semibold"><ArrowUpIcon class="text-chart-1 size-4" /> {{ rate(stats.now.net_tx) }}</p>
            </CardContent></Card>
        </section>

        <section class="grid gap-4 lg:grid-cols-2">
            <Card><CardHeader class="px-5"><CardTitle>{{ t('CPU and memory') }}</CardTitle><CardDescription>{{ t('Last hour, percent') }}</CardDescription></CardHeader>
                <CardContent class="px-5"><AreaChart :times="times" :max="100" :format="pct" :series="[{ label: 'CPU', values: h.map((p) => p.cpu), color: 'var(--chart-1)' }, { label: t('Memory'), values: h.map((p) => p.mem), color: 'var(--chart-4)' }]" /></CardContent></Card>
            <Card><CardHeader class="px-5"><CardTitle>{{ t('Network traffic') }}</CardTitle><CardDescription>{{ t('Last hour') }}</CardDescription></CardHeader>
                <CardContent class="px-5"><AreaChart :times="times" :format="rate" :series="[{ label: t('In'), values: h.map((p) => p.net_rx), color: 'var(--chart-2)' }, { label: t('Out'), values: h.map((p) => p.net_tx), color: 'var(--chart-1)' }]" /></CardContent></Card>
        </section>

        <section class="grid gap-4 lg:grid-cols-3">
            <Card class="lg:col-span-2 gap-0 py-0">
                <CardHeader class="border-b px-5 py-4"><CardTitle>{{ t('Busiest processes') }}</CardTitle></CardHeader>
                <Table>
                    <TableHeader><TableRow><TableHead class="px-5">{{ t('Process') }}</TableHead><TableHead>{{ t('User') }}</TableHead><TableHead class="text-right">CPU</TableHead><TableHead class="px-5 text-right">{{ t('Memory') }}</TableHead></TableRow></TableHeader>
                    <TableBody>
                        <TableRow v-for="p in stats.procs" :key="p.pid">
                            <TableCell class="px-5 font-medium">{{ p.name }} <span class="text-muted-foreground tabular text-xs">{{ p.pid }}</span></TableCell>
                            <TableCell class="text-muted-foreground">{{ p.user }}</TableCell>
                            <TableCell class="tabular text-right">{{ pct(p.cpu) }}</TableCell>
                            <TableCell class="tabular px-5 text-right">{{ bytes(p.mem) }}</TableCell>
                        </TableRow>
                    </TableBody>
                </Table>
            </Card>
            <div class="flex flex-col gap-4">
                <Link v-for="x in tiles" :key="x.label" :href="x.href" class="group">
                    <Card class="group-hover:border-primary/40 py-4 transition-colors"><CardContent class="flex items-center gap-3 px-4">
                        <span class="bg-accent text-accent-foreground grid size-9 place-items-center rounded-md"><component :is="x.icon" class="size-4" /></span>
                        <div><p class="tabular text-xl leading-none font-semibold">{{ x.n ?? '–' }}</p><p class="text-muted-foreground text-sm">{{ t(x.label) }}</p></div>
                    </CardContent></Card>
                </Link>
            </div>
        </section>

        <Card v-if="stats.disks.length > 1" class="gap-0 py-0">
            <CardHeader class="border-b px-5 py-4"><CardTitle>{{ t('Disks') }}</CardTitle></CardHeader>
            <ul class="divide-y">
                <li v-for="d in stats.disks" :key="d.mount" class="flex items-center gap-4 px-5 py-3">
                    <span class="w-40 min-w-0 truncate font-medium">{{ d.mount }}</span>
                    <div class="bg-muted h-2 flex-1 overflow-hidden rounded-full"><div class="bg-primary h-full rounded-full" :style="{ width: `${(d.used / d.total) * 100}%` }" /></div>
                    <span class="text-muted-foreground tabular w-40 text-right text-sm">{{ bytes(d.used) }} / {{ bytes(d.total) }}</span>
                </li>
            </ul>
        </Card>
    </template>
</template>

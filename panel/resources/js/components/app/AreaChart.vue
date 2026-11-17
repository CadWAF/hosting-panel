<script setup lang="ts">
import { computed, ref } from 'vue';

export interface Series { label: string; values: number[]; color: string }
const props = withDefaults(defineProps<{
    series: Series[];
    times?: number[];
    height?: number;
    max?: number; // fixed y maximum (e.g. 100 for percent); otherwise auto
    format?: (v: number) => string;
}>(), { height: 96 });

const W = 600;
const H = computed(() => props.height);
const yMax = computed(() => {
    if (props.max) return props.max;
    const m = Math.max(0, ...props.series.flatMap((s) => s.values));
    return m > 0 ? m * 1.15 : 1;
});
const n = computed(() => Math.max(0, ...props.series.map((s) => s.values.length)));
const x = (i: number) => (n.value <= 1 ? W : (i / (n.value - 1)) * W);
const y = (v: number) => H.value - (Math.min(v, yMax.value) / yMax.value) * (H.value - 4) - 2;
const line = (s: Series) => s.values.map((v, i) => `${i ? 'L' : 'M'}${x(i).toFixed(1)},${y(v).toFixed(1)}`).join(' ');
const area = (s: Series) => (s.values.length ? `${line(s)} L${x(s.values.length - 1)},${H.value} L0,${H.value} Z` : '');

const hover = ref<number | null>(null);
function move(e: PointerEvent) {
    const r = (e.currentTarget as SVGElement).getBoundingClientRect();
    if (!n.value) return;
    hover.value = Math.round(((e.clientX - r.left) / r.width) * (n.value - 1));
}
const fmt = (v: number) => (props.format ? props.format(v) : v.toFixed(1));
const stamp = computed(() => {
    const t = hover.value != null ? props.times?.[hover.value] : undefined;
    return t ? new Date(t * 1000).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit', second: '2-digit' }) : '';
});
const gid = `g${Math.random().toString(36).slice(2, 8)}`;
</script>

<template>
    <div class="relative">
        <svg
            :viewBox="`0 0 ${W} ${H}`" preserveAspectRatio="none" class="block w-full touch-none overflow-visible" :style="{ height: `${H}px` }"
            role="img" :aria-label="series.map((s) => s.label).join(', ')" @pointermove="move" @pointerleave="hover = null"
        >
            <defs>
                <linearGradient v-for="(s, i) in series" :id="`${gid}-${i}`" :key="i" x1="0" y1="0" x2="0" y2="1">
                    <stop offset="0" :stop-color="s.color" stop-opacity="0.28" /><stop offset="1" :stop-color="s.color" stop-opacity="0" />
                </linearGradient>
            </defs>
            <line v-for="g in [0.25, 0.5, 0.75]" :key="g" x1="0" :x2="W" :y1="H * g" :y2="H * g" class="stroke-border" stroke-width="1" vector-effect="non-scaling-stroke" stroke-dasharray="3 4" />
            <template v-for="(s, i) in series" :key="s.label">
                <path :d="area(s)" :fill="`url(#${gid}-${i})`" />
                <path :d="line(s)" fill="none" :stroke="s.color" stroke-width="1.75" stroke-linejoin="round" stroke-linecap="round" vector-effect="non-scaling-stroke" />
            </template>
            <line v-if="hover != null" :x1="x(hover)" :x2="x(hover)" y1="0" :y2="H" class="stroke-muted-foreground/50" stroke-width="1" vector-effect="non-scaling-stroke" />
        </svg>
        <div
            v-if="hover != null && series.some((s) => s.values[hover!] != null)"
            class="bg-popover text-popover-foreground pointer-events-none absolute top-0 z-10 rounded-md border px-2 py-1 text-xs shadow-md"
            :style="{ left: `${(x(hover) / W) * 100}%`, transform: hover > n / 2 ? 'translateX(calc(-100% - 8px))' : 'translateX(8px)' }"
        >
            <p v-if="stamp" class="text-muted-foreground tabular">{{ stamp }}</p>
            <p v-for="s in series" :key="s.label" class="tabular flex items-center gap-1.5 whitespace-nowrap">
                <span class="size-2 rounded-full" :style="{ background: s.color }" />{{ s.label }} <b class="font-semibold">{{ fmt(s.values[hover!] ?? 0) }}</b>
            </p>
        </div>
    </div>
</template>

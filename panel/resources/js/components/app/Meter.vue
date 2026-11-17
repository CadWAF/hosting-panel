<script setup lang="ts">
import { computed } from 'vue';

const props = withDefaults(defineProps<{ value: number; size?: number; label?: string }>(), { size: 72 });
const r = 15.9155;
const pct = computed(() => Math.max(0, Math.min(100, props.value || 0)));
const tone = computed(() => (pct.value >= 90 ? 'var(--destructive)' : pct.value >= 75 ? 'var(--warning)' : 'var(--primary)'));
</script>

<template>
    <div class="relative shrink-0" :style="{ width: `${size}px`, height: `${size}px` }" role="meter" :aria-valuenow="Math.round(pct)" aria-valuemin="0" aria-valuemax="100" :aria-label="label">
        <svg viewBox="0 0 36 36" class="size-full -rotate-90">
            <circle cx="18" cy="18" :r="r" fill="none" class="stroke-muted" stroke-width="3.2" />
            <circle
                cx="18" cy="18" :r="r" fill="none" :stroke="tone" stroke-width="3.2" stroke-linecap="round"
                :stroke-dasharray="`${pct} ${100 - pct}`" class="transition-[stroke-dasharray] duration-500"
            />
        </svg>
        <span class="tabular absolute inset-0 flex items-center justify-center text-sm font-semibold whitespace-nowrap">{{ Math.round(pct) }}<small class="text-muted-foreground ml-px text-[0.625rem]">%</small></span>
    </div>
</template>

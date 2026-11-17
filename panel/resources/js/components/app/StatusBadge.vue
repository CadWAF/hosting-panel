<script setup lang="ts">
import { computed } from 'vue';
import { Badge } from '@/components/ui/badge';

const props = defineProps<{ status: string; labels?: Record<string, string> }>();
const ok = ['enabled', 'ok', 'done', 'up', 'installed', 'running'];
const bad = ['failed', 'wrong', 'error', 'down'];
const tone = computed(() => (ok.includes(props.status) ? 'ok' : bad.includes(props.status) ? 'bad' : 'off'));
</script>

<template>
    <Badge
        variant="outline"
        class="gap-1.5"
        :class="{ 'border-success/30 bg-success/10 text-success': tone === 'ok', 'border-destructive/30 bg-destructive/10 text-destructive': tone === 'bad', 'text-muted-foreground': tone === 'off' }"
    >
        <span class="size-1.5 rounded-full bg-current" aria-hidden="true" />
        {{ labels?.[status] ?? status }}
    </Badge>
</template>

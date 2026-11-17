<script setup lang="ts">
import { computed, ref } from 'vue';
import { Head, router } from '@inertiajs/vue3';
import { ListChecksIcon } from '@lucide/vue';
import PageHeader from '@/components/app/PageHeader.vue';
import ServiceDown from '@/components/app/ServiceDown.vue';
import EmptyState from '@/components/app/EmptyState.vue';
import StatusBadge from '@/components/app/StatusBadge.vue';
import { Card } from '@/components/ui/card';
import { ago } from '@/lib/format';
import { useInterval } from '@/composables/useTask';

interface T { id: string; title: string; status: string; log: string; created_at: string }
const props = defineProps<{ tasks: T[]; available: boolean }>();
const open = ref<string | null>(null);
const running = computed(() => props.tasks.some((t) => t.status === 'running'));
useInterval(() => running.value && router.reload({ only: ['tasks'] }), 3000);
</script>

<template>
    <Head title="Tasks" />
    <PageHeader title="Tasks" description="Long operations keep running when you close this page." />
    <ServiceDown v-if="!available" name="Core" />
    <EmptyState v-if="available && !tasks.length" title="Nothing has run yet"><template #icon><ListChecksIcon /></template></EmptyState>
    <Card v-if="tasks.length" class="gap-0 py-0">
        <ul class="divide-y">
            <li v-for="t in tasks" :key="t.id">
                <button class="hover:bg-muted/50 flex w-full items-center justify-between gap-3 p-4 text-left" :aria-expanded="open === t.id" @click="open = open === t.id ? null : t.id">
                    <span><span class="font-medium">{{ t.title }}</span><br><span class="text-muted-foreground text-sm">{{ ago(t.created_at) }}</span></span>
                    <StatusBadge :status="t.status" />
                </button>
                <pre v-if="open === t.id" class="bg-muted mx-4 mb-4 max-h-80 overflow-auto rounded-md p-3 text-xs whitespace-pre-wrap">{{ t.log || 'No output.' }}</pre>
            </li>
        </ul>
    </Card>
</template>

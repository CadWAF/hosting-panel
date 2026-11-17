<script setup lang="ts">
import { Head, router } from '@inertiajs/vue3';
import PageHeader from '@/components/app/PageHeader.vue';
import StatusBadge from '@/components/app/StatusBadge.vue';
import { Card, CardContent, CardHeader, CardTitle, CardAction, CardDescription } from '@/components/ui/card';
import { useInterval } from '@/composables/useTask';

defineProps<{ services: { name: string; up: boolean; note: string }[]; checks: { name: string; up: boolean; last_error?: string }[] }>();
useInterval(() => router.reload({ only: ['services', 'checks'] }), 10000);
</script>

<template>
    <Head title="Services" />
    <PageHeader title="Services" description="Each service runs on its own. One failing does not stop the others." />
    <ul class="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
        <li v-for="s in services" :key="s.name">
            <Card class="h-full gap-1 py-4">
                <CardHeader>
                    <CardTitle>{{ s.name }}</CardTitle>
                    <CardAction><StatusBadge :status="s.up ? 'running' : 'down'" :labels="{ running: 'Running', down: 'Unavailable' }" /></CardAction>
                    <CardDescription>{{ s.note }}</CardDescription>
                </CardHeader>
            </Card>
        </li>
    </ul>
    <template v-if="checks.length">
        <h2 class="text-lg font-semibold">Monitored by Notifier</h2>
        <Card class="gap-0 py-0">
            <ul class="divide-y">
                <li v-for="c in checks" :key="c.name" class="flex items-center justify-between gap-3 p-3 px-4">
                    <span>{{ c.name }}<span v-if="!c.up && c.last_error" class="text-muted-foreground text-sm"> · {{ c.last_error }}</span></span>
                    <StatusBadge :status="c.up ? 'up' : 'down'" :labels="{ up: 'Up', down: 'Down' }" />
                </li>
            </ul>
        </Card>
    </template>
</template>

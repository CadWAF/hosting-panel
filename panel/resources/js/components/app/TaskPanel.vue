<script setup lang="ts">
import { nextTick, ref, watch } from 'vue';
import { XIcon } from '@lucide/vue';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { Card, CardAction, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { Skeleton } from '@/components/ui/skeleton';
import type { TaskInfo } from '@/composables/useTask';

const props = defineProps<{ task: TaskInfo | null; pending?: boolean }>();
defineEmits<{ dismiss: [] }>();

const log = ref<HTMLElement | null>(null);
watch(() => props.task?.log, async () => {
    await nextTick();
    if (log.value) log.value.scrollTop = log.value.scrollHeight;
});
const tone = { running: 'secondary', done: 'default', failed: 'destructive', interrupted: 'outline' } as const;
</script>

<template>
    <Card v-if="task || pending" role="status" aria-live="polite">
        <template v-if="task">
            <CardHeader>
                <CardTitle>{{ task.title }}</CardTitle>
                <CardAction class="flex items-center gap-2">
                    <Badge :variant="tone[task.status]">{{ task.status }}</Badge>
                    <Button v-if="task.status !== 'running'" variant="ghost" size="icon-sm" aria-label="Dismiss" @click="$emit('dismiss')"><XIcon /></Button>
                </CardAction>
            </CardHeader>
            <CardContent>
                <pre ref="log" class="bg-muted max-h-64 overflow-auto rounded-md p-3 text-xs whitespace-pre-wrap">{{ task.log || 'Starting…' }}</pre>
            </CardContent>
        </template>
        <CardContent v-else class="flex flex-col gap-3">
            <Skeleton class="h-5 w-1/3" />
            <Skeleton class="h-24 w-full" />
        </CardContent>
    </Card>
</template>

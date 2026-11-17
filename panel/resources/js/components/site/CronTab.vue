<script setup lang="ts">
import { onMounted, ref } from 'vue';
import { ClockIcon, Trash2Icon } from '@lucide/vue';
import EmptyState from '@/components/app/EmptyState.vue';
import { Button } from '@/components/ui/button';
import { Card } from '@/components/ui/card';
import { Field, FieldDescription, FieldLabel } from '@/components/ui/field';
import { Input } from '@/components/ui/input';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select';
import { act, useBusy } from '@/composables/useActions';
import { rpc } from '@/composables/useRpc';

interface Job { id: number; schedule: string; command: string }
const props = defineProps<{ domain: string }>();
const jobs = ref<Job[]>([]);
const schedule = ref('*/5 * * * *');
const command = ref('');
const { busy, run } = useBusy();
const presets = [
    ['* * * * *', 'Every minute'], ['*/5 * * * *', 'Every 5 minutes'], ['*/15 * * * *', 'Every 15 minutes'],
    ['0 * * * *', 'Every hour'], ['0 3 * * *', 'Every day, 03:00'], ['0 3 * * 0', 'Every Sunday, 03:00'],
];
const load = async () => { const r = await rpc<Job[]>('core', 'cron', props.domain); jobs.value = r.ok ? r.data ?? [] : []; };
onMounted(load);
async function add() {
    if ((await run(() => act(rpc('core', 'addCron', props.domain, schedule.value, command.value), 'Job added'))).ok) { command.value = ''; load(); }
}
async function remove(j: Job) { if ((await act(rpc('core', 'removeCron', props.domain, j.id))).ok) load(); }
</script>

<template>
    <div class="flex flex-col gap-4">
        <Card class="p-4">
            <form class="flex flex-col gap-3" @submit.prevent="add">
                <div class="grid gap-3 sm:grid-cols-[14rem_1fr_auto] sm:items-end">
                    <Field>
                        <FieldLabel for="c-sched">Schedule</FieldLabel>
                        <div class="flex gap-2">
                            <Input id="c-sched" v-model="schedule" class="font-mono" required />
                            <Select @update:model-value="(v) => (schedule = String(v))">
                                <SelectTrigger class="w-10 shrink-0 px-2" aria-label="Schedule presets"><SelectValue placeholder="" /></SelectTrigger>
                                <SelectContent><SelectItem v-for="[v, l] in presets" :key="v" :value="v">{{ l }}</SelectItem></SelectContent>
                            </Select>
                        </div>
                    </Field>
                    <Field><FieldLabel for="c-cmd">Command</FieldLabel><Input id="c-cmd" v-model="command" class="font-mono" placeholder="php artisan schedule:run" required /></Field>
                    <Button type="submit" :disabled="busy">Add job</Button>
                </div>
                <FieldDescription>Jobs run as the site user. They pause while the site is disabled.</FieldDescription>
            </form>
        </Card>
        <EmptyState v-if="!jobs.length" title="No scheduled jobs"><template #icon><ClockIcon /></template></EmptyState>
        <Card v-else class="gap-0 py-0">
            <ul class="divide-y">
                <li v-for="j in jobs" :key="j.id" class="flex flex-wrap items-center justify-between gap-2 p-3 px-4">
                    <code class="min-w-0 truncate text-sm">{{ j.schedule }} {{ j.command }}</code>
                    <Button variant="outline" size="icon-sm" class="text-destructive" aria-label="Remove job" @click="remove(j)"><Trash2Icon /></Button>
                </li>
            </ul>
        </Card>
    </div>
</template>

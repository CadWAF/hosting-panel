<script setup lang="ts">
import { ref } from 'vue';
import { Head, router } from '@inertiajs/vue3';
import { PuzzleIcon, ServerIcon } from '@lucide/vue';
import PageHeader from '@/components/app/PageHeader.vue';
import ServiceDown from '@/components/app/ServiceDown.vue';
import ConfirmDialog from '@/components/app/ConfirmDialog.vue';
import TaskPanel from '@/components/app/TaskPanel.vue';
import StatusBadge from '@/components/app/StatusBadge.vue';
import { Button } from '@/components/ui/button';
import { Card, CardContent } from '@/components/ui/card';
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from '@/components/ui/dialog';
import { Toggle } from '@/components/ui/toggle';
import { act } from '@/composables/useActions';
import { rpc } from '@/composables/useRpc';
import { useTask } from '@/composables/useTask';

interface Runtime { version: string; installed: boolean; sites: string[]; extensions: string[] }
defineProps<{ runtimes: Runtime[]; extensions: string[]; available: boolean }>();

const reload = () => router.reload({ only: ['runtimes', 'extensions'] });
const { task, follow, dismiss } = useTask('core', () => reload());
async function start(method: string, ...args: string[]) {
    const r = await act(rpc('core', method, ...args));
    if (r.ok) follow(r.data?.id);
}
const extOf = ref<string | null>(null);
const extOpen = ref(false);
const removing = ref<string | null>(null);
const removeOpen = ref(false);
</script>

<template>
    <Head title="PHP" />
    <PageHeader title="PHP" description="Install several versions side by side. Each site picks its own.">
        <Button variant="outline" @click="start('installApache')"><ServerIcon /> Install Apache backend</Button>
    </PageHeader>

    <ServiceDown v-if="!available" name="Core" />
    <TaskPanel :task="task" @dismiss="dismiss" />

    <Card class="gap-0 py-0">
        <ul class="divide-y">
            <li v-for="r in runtimes" :key="r.version" class="flex flex-wrap items-center justify-between gap-3 p-4">
                <div>
                    <p class="font-medium">PHP {{ r.version }}</p>
                    <p class="text-muted-foreground text-sm">{{ r.installed ? `${r.sites.length} ${r.sites.length === 1 ? 'site' : 'sites'}` : 'Not installed' }}</p>
                </div>
                <div class="flex items-center gap-2">
                    <template v-if="r.installed">
                        <StatusBadge status="installed" :labels="{ installed: 'Installed' }" />
                        <Button variant="outline" size="sm" @click="extOf = r.version; extOpen = true"><PuzzleIcon /> Extensions</Button>
                        <Button v-if="!r.sites.length" variant="outline" size="sm" class="text-destructive" @click="removing = r.version; removeOpen = true">Remove</Button>
                        <Button v-else variant="outline" size="sm" disabled :title="r.sites.join(', ')">In use</Button>
                    </template>
                    <Button v-else size="sm" @click="start('installPhp', r.version)">Install</Button>
                </div>
            </li>
        </ul>
    </Card>

    <Dialog v-model:open="extOpen">
        <DialogContent class="sm:max-w-lg">
            <DialogHeader><DialogTitle>PHP {{ extOf }} extensions</DialogTitle><DialogDescription>Click to install or remove. Sites using this version are reloaded.</DialogDescription></DialogHeader>
            <div class="flex flex-wrap gap-2">
                <Toggle
                    v-for="e in extensions" :key="e" variant="outline" size="sm"
                    :model-value="!!runtimes.find((r) => r.version === extOf)?.extensions.includes(e)"
                    @update:model-value="(on: boolean) => { start(on ? 'installExtension' : 'removeExtension', extOf!, e); extOpen = false; }"
                >{{ e }}</Toggle>
            </div>
        </DialogContent>
    </Dialog>
    <ConfirmDialog v-model:open="removeOpen" :title="`Remove PHP ${removing}?`" description="No site uses it. You can install it again later." confirm-label="Remove" @confirm="removing && start('removePhp', removing)" />
</template>

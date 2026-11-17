<script setup lang="ts">
import { ref } from 'vue';
import { CheckIcon, CopyIcon } from '@lucide/vue';
import { Button } from '@/components/ui/button';
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog';

export interface SecretRow {
    label: string;
    value: string;
}
defineProps<{ title: string; description?: string; rows: SecretRow[]; multiline?: boolean }>();
const open = defineModel<boolean>('open', { default: false });

const copied = ref<string | null>(null);
async function copy(v: string) {
    try {
        await navigator.clipboard.writeText(v);
        copied.value = v;
        setTimeout(() => (copied.value = null), 1500);
    } catch {
        /* clipboard blocked: the value stays selectable */
    }
}
</script>

<template>
    <Dialog v-model:open="open">
        <DialogContent class="sm:max-w-lg">
            <DialogHeader>
                <DialogTitle>{{ title }}</DialogTitle>
                <DialogDescription>{{ description ?? 'This is shown only once and cannot be retrieved later.' }}</DialogDescription>
            </DialogHeader>
            <dl class="flex flex-col gap-3">
                <div v-for="r in rows" :key="r.label" class="flex flex-col gap-1">
                    <dt class="text-muted-foreground text-xs font-medium uppercase">{{ r.label }}</dt>
                    <dd class="flex items-start gap-2">
                        <pre v-if="multiline" class="bg-muted max-h-48 min-w-0 flex-1 overflow-auto rounded-md p-3 text-xs break-all whitespace-pre-wrap select-all">{{ r.value }}</pre>
                        <code v-else class="bg-muted min-w-0 flex-1 rounded-md px-3 py-2 text-sm break-all select-all">{{ r.value }}</code>
                        <Button variant="outline" size="icon" :aria-label="`Copy ${r.label}`" @click="copy(r.value)">
                            <CheckIcon v-if="copied === r.value" />
                            <CopyIcon v-else />
                        </Button>
                    </dd>
                </div>
            </dl>
            <DialogFooter><Button @click="open = false">I saved it</Button></DialogFooter>
        </DialogContent>
    </Dialog>
</template>

<script setup lang="ts">
import { ref } from 'vue';
import { DownloadIcon } from '@lucide/vue';
import { Button } from '@/components/ui/button';
import { Card, CardContent } from '@/components/ui/card';
import { act } from '@/composables/useActions';
import { useI18n } from '@/composables/useI18n';
import { rpc } from '@/composables/useRpc';

/** Shown where a feature needs software that is not installed yet; installs it from the catalog as a Task. */
const props = defineProps<{ id: string; name: string; description: string; disabled?: boolean }>();
const emit = defineEmits<{ started: [taskId: string] }>();
const { t } = useI18n();
const busy = ref(false);
async function install() {
    busy.value = true;
    const r = await act(rpc('system', 'installSoftware', props.id));
    busy.value = false;
    if (r.ok) emit('started', r.data?.id);
}
</script>

<template>
    <Card class="border-dashed">
        <CardContent class="flex flex-wrap items-center justify-between gap-4 px-5">
            <div class="min-w-0">
                <p class="font-semibold">{{ t('{name} is not installed', { name }) }}</p>
                <p class="text-muted-foreground text-sm">{{ description }}</p>
            </div>
            <Button :disabled="busy || disabled" @click="install"><DownloadIcon /> {{ t('Install {name}', { name }) }}</Button>
        </CardContent>
    </Card>
</template>

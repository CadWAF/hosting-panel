<script setup lang="ts">
import { computed, ref } from 'vue';
import { Head, Link, router } from '@inertiajs/vue3';
import { CheckIcon, DownloadIcon, PlayIcon, RotateCwIcon, SearchIcon, SettingsIcon, SquareIcon, Trash2Icon } from '@lucide/vue';
import ConfirmDialog from '@/components/app/ConfirmDialog.vue';
import PageHeader from '@/components/app/PageHeader.vue';
import ServiceDown from '@/components/app/ServiceDown.vue';
import TaskPanel from '@/components/app/TaskPanel.vue';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { Card } from '@/components/ui/card';
import { Input } from '@/components/ui/input';
import { act } from '@/composables/useActions';
import { useI18n } from '@/composables/useI18n';
import { rpc } from '@/composables/useRpc';
import { useTask } from '@/composables/useTask';

interface Item {
    id: string; name: string; category: string; description: string; detail: string; manage: string; units: string[];
    installed: boolean; version: string; active: boolean; enabled: boolean;
}
const props = defineProps<{ items: Item[]; categories: string[]; available: boolean }>();
const { t } = useI18n();

const reload = () => router.reload({ only: ['items'], preserveScroll: true });
const { task, follow, dismiss } = useTask('system', () => reload());
const q = ref('');
const cat = ref('all');
const shown = computed(() => props.items.filter((i) => (cat.value === 'all' || i.category === cat.value)
    && (!q.value || `${i.name} ${i.description}`.toLowerCase().includes(q.value.toLowerCase()))));
const installedCount = computed(() => props.items.filter((i) => i.installed).length);

const busy = ref<string | null>(null);
async function install(i: Item) {
    busy.value = i.id;
    const r = await act(rpc('system', 'installSoftware', i.id));
    busy.value = null;
    if (r.ok) follow(r.data?.id);
}
const removing = ref<Item | null>(null);
const removeOpen = ref(false);
async function remove() {
    if (!removing.value) return;
    const r = await act(rpc('system', 'removeSoftware', removing.value.id));
    if (r.ok) follow(r.data?.id);
}
async function unit(i: Item, action: string) {
    busy.value = i.id;
    const results = await Promise.all(i.units.map((u) => act(rpc('system', 'serviceAction', u, action))));
    busy.value = null;
    if (results.every((r) => r.ok)) reload();
}
</script>

<template>
    <Head :title="t('Software')" />
    <PageHeader :title="t('Software')" :description="t('Install and manage server software in one click. Everything installs from the official Ubuntu repositories.')">
        <span class="text-muted-foreground tabular text-sm">{{ installedCount }} {{ t('installed') }}</span>
    </PageHeader>

    <ServiceDown v-if="!available" name="System" />
    <TaskPanel :task="task" @dismiss="dismiss" />

    <div class="flex flex-wrap items-center gap-2">
        <div class="relative w-full sm:w-64">
            <SearchIcon class="text-muted-foreground pointer-events-none absolute top-1/2 left-2.5 size-4 -translate-y-1/2" />
            <Input v-model="q" :placeholder="t('Search software')" class="pl-8" :aria-label="t('Search software')" />
        </div>
        <div class="flex flex-wrap gap-1.5" role="group" :aria-label="t('Category')">
            <Button size="sm" :variant="cat === 'all' ? 'default' : 'outline'" @click="cat = 'all'">{{ t('All') }}</Button>
            <Button v-for="c in categories" :key="c" size="sm" :variant="cat === c ? 'default' : 'outline'" @click="cat = c">{{ t(`cat.${c}`) }}</Button>
        </div>
    </div>

    <ul class="grid gap-3 sm:grid-cols-2 xl:grid-cols-3">
        <li v-for="i in shown" :key="i.id">
            <Card class="h-full gap-3 px-4 py-4">
                <div class="flex items-start justify-between gap-3">
                    <div class="min-w-0">
                        <h2 class="truncate font-semibold">{{ i.name }}</h2>
                        <p class="text-muted-foreground mt-0.5 text-xs">{{ t(`cat.${i.category}`) }}<template v-if="i.installed && i.version"> · <span class="tabular">{{ i.version.split('-')[0] }}</span></template></p>
                    </div>
                    <Badge v-if="i.installed" variant="outline" class="gap-1" :class="i.units.length && !i.active ? 'text-warning border-warning/30 bg-warning/10' : 'border-success/30 bg-success/10 text-success'">
                        <CheckIcon v-if="!i.units.length || i.active" /> {{ !i.units.length ? t('Installed') : i.active ? t('Running') : t('Stopped') }}
                    </Badge>
                </div>
                <p class="text-muted-foreground flex-1 text-sm text-pretty">{{ t(i.description) }}<span v-if="i.detail && !i.installed" class="mt-1 block text-xs">{{ t(i.detail) }}</span></p>
                <div class="flex flex-wrap items-center gap-2">
                    <Button v-if="!i.installed" size="sm" :disabled="busy === i.id || task?.status === 'running'" @click="install(i)"><DownloadIcon /> {{ t('Install') }}</Button>
                    <template v-else>
                        <Button v-if="i.manage" as-child size="sm"><Link :href="i.manage"><SettingsIcon /> {{ t('Manage') }}</Link></Button>
                        <template v-if="i.units.length">
                            <Button v-if="i.active" variant="outline" size="icon-sm" :aria-label="t('Stop')" :title="t('Stop')" :disabled="busy === i.id" @click="unit(i, 'stop')"><SquareIcon /></Button>
                            <Button v-else variant="outline" size="icon-sm" :aria-label="t('Start')" :title="t('Start')" :disabled="busy === i.id" @click="unit(i, 'start')"><PlayIcon /></Button>
                            <Button variant="outline" size="icon-sm" :aria-label="t('Restart')" :title="t('Restart')" :disabled="busy === i.id || !i.active" @click="unit(i, 'restart')"><RotateCwIcon /></Button>
                        </template>
                        <Button variant="ghost" size="icon-sm" class="text-muted-foreground hover:text-destructive ml-auto" :aria-label="t('Remove')" :title="t('Remove')" :disabled="task?.status === 'running'" @click="removing = i; removeOpen = true"><Trash2Icon /></Button>
                    </template>
                </div>
            </Card>
        </li>
    </ul>
    <p v-if="!shown.length && available" class="text-muted-foreground py-8 text-center">{{ t('Nothing matches your search.') }}</p>

    <ConfirmDialog
        v-model:open="removeOpen" :title="t('Remove {name}?', { name: removing?.name ?? '' })"
        :description="t('The packages are removed. Configuration files and data are kept, so installing it again restores your setup.')"
        :confirm-label="t('Remove')" @confirm="remove"
    />
</template>

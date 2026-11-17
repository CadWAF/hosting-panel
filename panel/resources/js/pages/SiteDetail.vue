<script setup lang="ts">
import { ref } from 'vue';
import { Head, Link } from '@inertiajs/vue3';
import { ArrowLeftIcon } from '@lucide/vue';
import PageHeader from '@/components/app/PageHeader.vue';
import ServiceDown from '@/components/app/ServiceDown.vue';
import FilesTab from '@/components/site/FilesTab.vue';
import SshTab from '@/components/site/SshTab.vue';
import CronTab from '@/components/site/CronTab.vue';
import LogsTab from '@/components/site/LogsTab.vue';
import HttpsTab from '@/components/site/HttpsTab.vue';
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs';
import { useI18n } from '@/composables/useI18n';

const { t } = useI18n();

const props = defineProps<{ domain: string; site: { tls?: string }; available: boolean }>();
const tab = ref(new URLSearchParams(location.search).get('tab') ?? 'files');
function onTab(v: string | number) {
    tab.value = String(v);
    const u = new URL(location.href);
    u.searchParams.set('tab', tab.value);
    history.replaceState(history.state, '', u);
}
</script>

<template>
    <Head :title="domain" />
    <div>
        <Link href="/sites" class="text-muted-foreground hover:text-foreground mb-1 inline-flex items-center gap-1 text-sm"><ArrowLeftIcon class="size-3.5" /> {{ t('Sites') }}</Link>
        <PageHeader :title="domain" />
    </div>
    <ServiceDown v-if="!available" name="Core" />
    <Tabs v-else :model-value="tab" @update:model-value="onTab">
        <TabsList class="flex-wrap">
            <TabsTrigger value="files">{{ t('Files') }}</TabsTrigger>
            <TabsTrigger value="ssh">{{ t('SSH') }}</TabsTrigger>
            <TabsTrigger value="cron">{{ t('Cron') }}</TabsTrigger>
            <TabsTrigger value="logs">{{ t('Logs') }}</TabsTrigger>
            <TabsTrigger value="https">{{ t('HTTPS') }}</TabsTrigger>
        </TabsList>
        <TabsContent value="files" class="mt-4"><FilesTab :domain="domain" /></TabsContent>
        <TabsContent value="ssh" class="mt-4"><SshTab :domain="domain" /></TabsContent>
        <TabsContent value="cron" class="mt-4"><CronTab :domain="domain" /></TabsContent>
        <TabsContent value="logs" class="mt-4"><LogsTab :domain="domain" /></TabsContent>
        <TabsContent value="https" class="mt-4"><HttpsTab :domain="domain" /></TabsContent>
    </Tabs>
</template>

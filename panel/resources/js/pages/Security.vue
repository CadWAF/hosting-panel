<script setup lang="ts">
import { computed } from 'vue';
import { Head, router } from '@inertiajs/vue3';
import { FlameIcon, KeyRoundIcon, ShieldBanIcon } from '@lucide/vue';
import InstallPrompt from '@/components/app/InstallPrompt.vue';
import PageHeader from '@/components/app/PageHeader.vue';
import ServiceDown from '@/components/app/ServiceDown.vue';
import TaskPanel from '@/components/app/TaskPanel.vue';
import Fail2banTab from '@/components/security/Fail2banTab.vue';
import FirewallTab from '@/components/security/FirewallTab.vue';
import SshTab from '@/components/security/SshTab.vue';
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs';
import { useI18n } from '@/composables/useI18n';
import { useTask } from '@/composables/useTask';

const props = defineProps<{ firewall: any; fail2ban: any; ssh: any; available: boolean }>();
const { t } = useI18n();
const { task, follow, dismiss } = useTask('system', () => router.reload({ only: ['firewall', 'fail2ban', 'ssh'], preserveScroll: true }));
const banned = computed(() => (props.fail2ban?.jails ?? []).reduce((n: number, j: any) => n + (j.banned || 0), 0));
</script>

<template>
    <Head :title="t('Security')" />
    <PageHeader :title="t('Security')" :description="t('Firewall, brute-force protection and SSH access.')" />
    <ServiceDown v-if="!available" name="System" />
    <TaskPanel :task="task" @dismiss="dismiss" />

    <Tabs v-if="available" default-value="firewall" class="gap-4">
        <TabsList class="h-10">
            <TabsTrigger value="firewall" class="px-3"><FlameIcon /> {{ t('Firewall') }}</TabsTrigger>
            <TabsTrigger value="fail2ban" class="px-3"><ShieldBanIcon /> Fail2ban <span v-if="banned" class="bg-destructive/15 text-destructive tabular ml-1 rounded px-1.5 text-xs">{{ banned }}</span></TabsTrigger>
            <TabsTrigger value="ssh" class="px-3"><KeyRoundIcon /> SSH</TabsTrigger>
        </TabsList>
        <TabsContent value="firewall">
            <FirewallTab v-if="firewall.installed" :firewall="firewall" />
            <InstallPrompt v-else id="ufw" name="UFW" :description="t('A simple firewall. It is installed switched off; you decide when to turn it on.')" :disabled="task?.status === 'running'" @started="follow" />
        </TabsContent>
        <TabsContent value="fail2ban">
            <Fail2banTab v-if="fail2ban.installed" :fail2ban="fail2ban" />
            <InstallPrompt v-else id="fail2ban" name="Fail2ban" :description="t('Automatically bans addresses that keep guessing passwords. SSH protection is switched on right away.')" :disabled="task?.status === 'running'" @started="follow" />
        </TabsContent>
        <TabsContent value="ssh"><SshTab :ssh="ssh" /></TabsContent>
    </Tabs>
</template>

<script setup lang="ts">
import { reactive, watch } from 'vue';
import { router } from '@inertiajs/vue3';
import { TriangleAlertIcon } from '@lucide/vue';
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert';
import { Button } from '@/components/ui/button';
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select';
import { Switch } from '@/components/ui/switch';
import { act, useBusy } from '@/composables/useActions';
import { useI18n } from '@/composables/useI18n';
import { rpc } from '@/composables/useRpc';

interface Ssh { port: number; root_login: string; password_auth: boolean; has_root_key: boolean }
const props = defineProps<{ ssh: Ssh }>();
const { t } = useI18n();
const { busy, run } = useBusy();
const form = reactive({ root_login: 'prohibit-password', password_auth: true });
watch(() => props.ssh, (s) => { form.root_login = s.root_login; form.password_auth = s.password_auth; }, { immediate: true });
async function save() {
    const r = await run(() => act(rpc('system', 'setSsh', { root_login: form.root_login, password_auth: form.password_auth }), t('SSH settings saved')));
    if (r.ok) router.reload({ only: ['ssh'], preserveScroll: true });
}
</script>

<template>
    <Card class="max-w-2xl">
        <CardHeader class="px-5"><CardTitle>{{ t('SSH access') }}</CardTitle><CardDescription>{{ t('How administrators log in to this server. Site users always use keys only and are not affected.') }}</CardDescription></CardHeader>
        <CardContent class="flex flex-col gap-5 px-5">
            <div class="flex items-center justify-between gap-4"><span class="text-sm font-medium">{{ t('Port') }}</span><span class="tabular font-mono">{{ ssh.port }}</span></div>
            <label class="grid gap-1.5 text-sm font-medium">{{ t('Root login') }}
                <Select v-model="form.root_login"><SelectTrigger class="w-full"><SelectValue /></SelectTrigger>
                    <SelectContent>
                        <SelectItem value="prohibit-password">{{ t('Key only (recommended)') }}</SelectItem>
                        <SelectItem value="no">{{ t('Not allowed') }}</SelectItem>
                        <SelectItem value="yes">{{ t('Password or key') }}</SelectItem>
                    </SelectContent></Select>
            </label>
            <div class="flex items-start justify-between gap-4">
                <div><p class="text-sm font-medium">{{ t('Password logins') }}</p><p class="text-muted-foreground text-sm">{{ t('Turn off to require SSH keys for every administrator account.') }}</p></div>
                <Switch v-model="form.password_auth" :aria-label="t('Password logins')" />
            </div>
            <Alert v-if="!form.password_auth && !ssh.has_root_key" variant="destructive"><TriangleAlertIcon /><AlertTitle>{{ t('No SSH key for root') }}</AlertTitle><AlertDescription>{{ t('Add a public key to /root/.ssh/authorized_keys first, otherwise you could lock yourself out.') }}</AlertDescription></Alert>
            <div><Button :disabled="busy" @click="save">{{ t('Save') }}</Button></div>
        </CardContent>
    </Card>
</template>

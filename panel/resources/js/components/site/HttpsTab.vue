<script setup lang="ts">
import { onMounted, ref } from 'vue';
import { LockIcon, LockOpenIcon } from '@lucide/vue';
import ConfirmDialog from '@/components/app/ConfirmDialog.vue';
import TaskPanel from '@/components/app/TaskPanel.vue';
import StatusBadge from '@/components/app/StatusBadge.vue';
import { Button } from '@/components/ui/button';
import { Card, CardContent, CardDescription, CardHeader, CardTitle, CardAction } from '@/components/ui/card';
import { Field, FieldLabel } from '@/components/ui/field';
import { Input } from '@/components/ui/input';
import { Switch } from '@/components/ui/switch';
import { act, useBusy } from '@/composables/useActions';
import { rpc } from '@/composables/useRpc';
import { useTask } from '@/composables/useTask';
import { useI18n } from '@/composables/useI18n';

const { t } = useI18n();

const props = defineProps<{ domain: string }>();
const tls = ref<string | null>(null);
const loaded = ref(false);
const www = ref(true);
const email = ref('');
const { busy, run } = useBusy();
async function load() { const r = await rpc('core', 'site', props.domain); tls.value = r.ok ? r.data?.tls || '' : ''; loaded.value = true; }
const { task, follow, dismiss } = useTask('core', () => load());
onMounted(load);
async function issue() { const r = await run(() => act(rpc('core', 'issueTls', props.domain, www.value, email.value))); if (r.ok) follow(r.data?.id); }
const offOpen = ref(false);
async function turnOff() { if ((await act(rpc('core', 'removeTls', props.domain), 'HTTPS turned off. The certificate is kept and can be re-enabled.')).ok) load(); }
</script>

<template>
    <div v-if="loaded" class="flex flex-col gap-4">
        <Card>
            <template v-if="tls">
                <CardHeader>
                    <CardTitle class="flex items-center gap-2"><LockIcon class="text-success size-4" /> {{ t('HTTPS is on') }} <StatusBadge status="ok" :labels="{ ok: tls === 'www' ? `${domain} + www` : domain }" /></CardTitle>
                    <CardDescription>{{ t('Plain HTTP redirects to HTTPS. The certificate renews automatically.') }}</CardDescription>
                    <CardAction><Button variant="outline" @click="offOpen = true">{{ t('Turn off') }}</Button></CardAction>
                </CardHeader>
            </template>
            <template v-else>
                <CardHeader>
                    <CardTitle class="flex items-center gap-2"><LockOpenIcon class="text-muted-foreground size-4" /> {{ t('HTTPS is off') }}</CardTitle>
                    <CardDescription>{{ t('Issues a free Let\'s Encrypt certificate. The domain must already point to this server.') }}</CardDescription>
                </CardHeader>
                <CardContent>
                    <form class="flex max-w-md flex-col gap-4" @submit.prevent="issue">
                        <Field orientation="horizontal"><Switch id="tls-www" v-model="www" /><FieldLabel for="tls-www">{{ t('Also secure') }} www.{{ domain }}</FieldLabel></Field>
                        <Field><FieldLabel for="tls-email">{{ t('Email for expiry notices (optional)') }}</FieldLabel><Input id="tls-email" v-model="email" type="email" /></Field>
                        <div><Button type="submit" :disabled="busy || task?.status === 'running'">{{ t('Get certificate') }}</Button></div>
                    </form>
                </CardContent>
            </template>
        </Card>
        <TaskPanel :task="task" @dismiss="dismiss" />
    </div>
    <ConfirmDialog v-model:open="offOpen" :title="t('Turn HTTPS off for this site?')" :description="t('The site will be served over plain HTTP. The certificate is kept.')" :confirm-label="t('Turn off')" @confirm="turnOff" />
</template>

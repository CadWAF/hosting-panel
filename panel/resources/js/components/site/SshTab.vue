<script setup lang="ts">
import { onMounted, ref } from 'vue';
import { KeyRoundIcon, Trash2Icon } from '@lucide/vue';
import EmptyState from '@/components/app/EmptyState.vue';
import ConfirmDialog from '@/components/app/ConfirmDialog.vue';
import SecretDialog from '@/components/app/SecretDialog.vue';
import { Button } from '@/components/ui/button';
import { Card } from '@/components/ui/card';
import { Field, FieldDescription, FieldLabel } from '@/components/ui/field';
import { Input } from '@/components/ui/input';
import { Textarea } from '@/components/ui/textarea';
import { act, useBusy } from '@/composables/useActions';
import { rpc } from '@/composables/useRpc';
import { useI18n } from '@/composables/useI18n';

const { t } = useI18n();

interface Key { id: number; label: string; fingerprint: string }
const props = defineProps<{ domain: string }>();
const keys = ref<Key[]>([]);
const label = ref('');
const pub = ref('');
const { busy, run } = useBusy();
const load = async () => { const r = await rpc<Key[]>('core', 'sshKeys', props.domain); keys.value = r.ok ? r.data ?? [] : []; };
onMounted(load);

async function add() {
    if ((await run(() => act(rpc('core', 'addSshKey', props.domain, label.value, pub.value), t('Key added')))).ok) { label.value = ''; pub.value = ''; load(); }
}
const secretOpen = ref(false);
const priv = ref('');
async function generate() {
    const r = await run(() => act(rpc('core', 'generateSshKey', props.domain, label.value || 'generated')));
    if (!r.ok) return;
    priv.value = r.extra?.private_key ?? '';
    label.value = '';
    secretOpen.value = true;
    load();
}
const rm = ref<Key | null>(null);
const rmOpen = ref(false);
async function remove() { if (rm.value && (await act(rpc('core', 'removeSshKey', props.domain, rm.value.id))).ok) load(); }
async function fromFile(ev: Event) {
    const f = (ev.target as HTMLInputElement).files?.[0];
    if (f) pub.value = (await f.text()).trim();
}
</script>

<template>
    <div class="flex flex-col gap-4">
        <p class="text-muted-foreground text-sm">{{ t('Login is by key only. Passwords are disabled for site users.') }}</p>
        <Card class="p-4">
            <form class="flex flex-col gap-3" @submit.prevent="add">
                <Field>
                    <FieldLabel for="k-pub">{{ t('Public key') }}</FieldLabel>
                    <Textarea id="k-pub" v-model="pub" rows="3" class="font-mono text-xs" :placeholder="t('ssh-ed25519 AAAA… you@laptop')" spellcheck="false" />
                    <FieldDescription>{{ t('Paste it, or') }} <label class="text-primary cursor-pointer underline">{{ t('choose a .pub file') }}<input type="file" accept=".pub,text/plain" class="sr-only" @change="fromFile" /></label>.</FieldDescription>
                </Field>
                <div class="flex flex-wrap items-end gap-2">
                    <Field class="min-w-40 flex-1"><FieldLabel for="k-label">{{ t('Label (optional)') }}</FieldLabel><Input id="k-label" v-model="label" /></Field>
                    <Button type="submit" :disabled="busy || !pub.trim()">{{ t('Add key') }}</Button>
                    <Button type="button" variant="outline" :disabled="busy" @click="generate">{{ t('Generate a pair') }}</Button>
                </div>
            </form>
        </Card>
        <EmptyState v-if="!keys.length" :title="t('No keys yet')" :description="t('Paste your public key to log in.')"><template #icon><KeyRoundIcon /></template></EmptyState>
        <Card v-else class="gap-0 py-0">
            <ul class="divide-y">
                <li v-for="k in keys" :key="k.id" class="flex flex-wrap items-center justify-between gap-2 p-3 px-4">
                    <div class="min-w-0"><p class="font-medium">{{ k.label }}</p><p class="text-muted-foreground truncate font-mono text-xs">{{ k.fingerprint }}</p></div>
                    <Button variant="outline" size="icon-sm" class="text-destructive" :aria-label="`${t('Remove')} ${k.label}`" @click="rm = k; rmOpen = true"><Trash2Icon /></Button>
                </li>
            </ul>
        </Card>
    </div>
    <SecretDialog v-model:open="secretOpen" :title="t('Save the private key now')" :description="t('It is shown only once and is not stored on the server.')" multiline :rows="[{ label: 'Private key', value: priv }]" />
    <ConfirmDialog v-model:open="rmOpen" :title="t('Remove this key?')" :description="t('It will no longer be able to log in.')" :confirm-label="t('Remove')" @confirm="remove" />
</template>

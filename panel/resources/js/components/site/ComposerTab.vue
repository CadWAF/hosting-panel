<script setup lang="ts">
import { computed, onMounted, ref } from 'vue';
import { PackageIcon } from '@lucide/vue';
import TaskPanel from '@/components/app/TaskPanel.vue';
import { Button } from '@/components/ui/button';
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card';
import { Field, FieldLabel } from '@/components/ui/field';
import { Input } from '@/components/ui/input';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select';
import { Skeleton } from '@/components/ui/skeleton';
import { Switch } from '@/components/ui/switch';
import { act, useBusy } from '@/composables/useActions';
import { rpc } from '@/composables/useRpc';
import { useTask } from '@/composables/useTask';
import { useI18n } from '@/composables/useI18n';

const { t } = useI18n();
const props = defineProps<{ domain: string }>();

const state = ref<{ enabled: boolean; installed: boolean; projects: string[] } | null>(null);
const dir = ref('public_html');
const pkg = ref('');
const noDev = ref(false);
const { busy, run } = useBusy();
const { task, follow, dismiss } = useTask('core', () => load());

async function load() {
    const r = await rpc('core', 'composer', props.domain);
    if (r.ok) {
        state.value = r.data;
        const ps: string[] = r.data.projects;
        if (ps.length && !ps.includes(dir.value === 'public_html' ? 'public_html' : dir.value)) dir.value = ps[0] || 'public_html';
    }
}
onMounted(load);
const projects = computed(() => (state.value?.projects?.length ? state.value.projects.map((p) => p || '.') : ['public_html']));

async function toggle(on: boolean) {
    const r = await act(rpc('core', 'updateSite', props.domain, { composer: on }), on ? t('Composer enabled') : t('Composer disabled'));
    if (r.ok) load();
}
async function go(command: string, withPkg = false) {
    const r = await run(() => act(rpc('core', 'runComposer', props.domain, command, withPkg ? pkg.value.trim() : '', dir.value === '.' ? '' : dir.value, noDev.value)));
    if (r.ok) { follow(r.data?.id); if (withPkg) pkg.value = ''; }
}
const running = computed(() => task.value?.status === 'running');
</script>

<template>
    <div v-if="state" class="flex flex-col gap-4">
        <Card>
            <CardHeader>
                <CardTitle class="flex items-center gap-2"><PackageIcon class="size-4" /> Composer</CardTitle>
                <CardDescription>{{ t('Composer runs as this site\'s own user and executes package scripts, so enable it only for sites you trust.') }}</CardDescription>
            </CardHeader>
            <CardContent class="flex flex-col gap-4">
                <Field orientation="horizontal"><Switch id="cp-on" :model-value="state.enabled" @update:model-value="(v) => toggle(!!v)" /><FieldLabel for="cp-on">{{ t('Allow Composer for this site') }}</FieldLabel></Field>
                <p v-if="!state.installed" role="alert" class="text-destructive text-sm">{{ t('Composer is not installed on the server. Install it from Software.') }}</p>

                <template v-if="state.enabled">
                    <div class="grid gap-3 sm:grid-cols-2">
                        <Field><FieldLabel>{{ t('Project folder') }}</FieldLabel>
                            <Select v-model="dir"><SelectTrigger class="w-full font-mono"><SelectValue /></SelectTrigger><SelectContent><SelectItem v-for="p in projects" :key="p" :value="p" class="font-mono">~/{{ p === '.' ? '' : p }}</SelectItem></SelectContent></Select>
                        </Field>
                        <Field orientation="horizontal" class="items-end"><Switch id="cp-nodev" v-model="noDev" /><FieldLabel for="cp-nodev">{{ t('Skip dev packages (--no-dev)') }}</FieldLabel></Field>
                    </div>
                    <div class="flex flex-wrap gap-2">
                        <Button :disabled="busy || running" @click="go('install')">composer install</Button>
                        <Button variant="outline" :disabled="busy || running" @click="go('update')">composer update</Button>
                        <Button variant="outline" :disabled="busy || running" @click="go('dump-autoload')">dump-autoload</Button>
                        <Button variant="outline" :disabled="busy || running" @click="go('validate')">validate</Button>
                    </div>
                    <form class="flex flex-wrap items-end gap-2" @submit.prevent="go('require', true)">
                        <Field class="min-w-64 flex-1"><FieldLabel for="cp-pkg">{{ t('Package') }}</FieldLabel><Input id="cp-pkg" v-model="pkg" class="font-mono" placeholder="monolog/monolog:^3.0" autocomplete="off" /></Field>
                        <Button type="submit" :disabled="busy || running || !pkg.trim()">require</Button>
                        <Button type="button" variant="outline" :disabled="busy || running || !pkg.trim()" @click="go('remove', true)">remove</Button>
                    </form>
                </template>
            </CardContent>
        </Card>
        <TaskPanel :task="task" @dismiss="dismiss" />
    </div>
    <Skeleton v-else class="h-48 w-full" />
</template>

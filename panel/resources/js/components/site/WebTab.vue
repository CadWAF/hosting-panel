<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue';
import { ArrowRightIcon, BoxIcon, PlusIcon, Trash2Icon } from '@lucide/vue';
import { Button } from '@/components/ui/button';
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card';
import { Field, FieldLabel } from '@/components/ui/field';
import { Input } from '@/components/ui/input';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select';
import { Skeleton } from '@/components/ui/skeleton';
import { Switch } from '@/components/ui/switch';
import { act, useBusy } from '@/composables/useActions';
import { rpc } from '@/composables/useRpc';
import { useI18n } from '@/composables/useI18n';

const { t } = useI18n();
const props = defineProps<{ domain: string }>();

interface Proxy { path: string; upstream: string; websocket: boolean }
interface Redirect { from: string; to: string; code: number }
interface Container { id: string; name: string; image: string; state: string; ports: string }

const loaded = ref(false);
const { busy, run } = useBusy();
const form = reactive({ max_body_mb: 128, gzip: false, static_cache: false, auth_user: '', auth_password: '' });
const proxies = ref<Proxy[]>([]);
const redirects = ref<Redirect[]>([]);
const containers = ref<Container[]>([]);

async function load() {
    const r = await rpc('core', 'site', props.domain);
    if (r.ok) {
        const w = r.data?.web ?? {};
        Object.assign(form, { max_body_mb: w.max_body_mb || 128, gzip: !!w.gzip, static_cache: !!w.static_cache, auth_user: w.auth_user ?? '', auth_password: '' });
        proxies.value = (w.proxies ?? []).map((p: Proxy) => ({ ...p }));
        redirects.value = (w.redirects ?? []).map((p: Redirect) => ({ ...p }));
    }
    loaded.value = true;
    const d = await rpc('system', 'docker');
    containers.value = d.ok ? (d.data?.containers ?? []).filter((c: Container) => c.state === 'running') : [];
}
onMounted(load);

/** A host port published by a container ("0.0.0.0:8080->80/tcp") becomes a local upstream. */
function upstreamOf(c: Container): string | null {
    const m = c.ports.match(/(?:\d+\.\d+\.\d+\.\d+|\[::\]):(\d+)->/);
    return m ? `http://127.0.0.1:${m[1]}` : null;
}
const usable = computed(() => containers.value.map((c) => ({ c, up: upstreamOf(c) })).filter((x) => x.up));

const newProxy = reactive<Proxy>({ path: '/', upstream: '', websocket: true });
function addProxy() {
    if (!newProxy.upstream.trim()) return;
    proxies.value.push({ path: newProxy.path.trim() || '/', upstream: newProxy.upstream.trim(), websocket: newProxy.websocket });
    Object.assign(newProxy, { path: '/app', upstream: '', websocket: true });
}
const newRedirect = reactive<Redirect>({ from: '', to: '', code: 301 });
function addRedirect() {
    if (!newRedirect.from.trim() || !newRedirect.to.trim()) return;
    redirects.value.push({ from: newRedirect.from.trim(), to: newRedirect.to.trim(), code: newRedirect.code });
    Object.assign(newRedirect, { from: '', to: '', code: 301 });
}

async function save() {
    const web: Record<string, unknown> = {
        max_body_mb: Number(form.max_body_mb) || 128, gzip: form.gzip, static_cache: form.static_cache,
        proxies: proxies.value, redirects: redirects.value, auth_user: form.auth_user.trim(),
    };
    if (form.auth_password) web.auth_password = form.auth_password;
    const r = await run(() => act(rpc('core', 'updateSite', props.domain, { web }), t('Web settings saved.')));
    if (r.ok) load();
}
</script>

<template>
    <div v-if="loaded" class="flex flex-col gap-4">
        <Card>
            <CardHeader>
                <CardTitle>{{ t('Reverse proxy') }}</CardTitle>
                <CardDescription>{{ t('Send a path, or the whole site, to an application such as a Docker container. HTTPS and the domain stay managed here.') }}</CardDescription>
            </CardHeader>
            <CardContent class="flex flex-col gap-4">
                <ul v-if="proxies.length" class="divide-y rounded-md border">
                    <li v-for="(p, i) in proxies" :key="i" class="flex flex-wrap items-center justify-between gap-2 px-3 py-2">
                        <span class="flex flex-wrap items-center gap-2 font-mono text-sm"><span>{{ p.path }}</span><ArrowRightIcon class="text-muted-foreground size-3.5" /><span>{{ p.upstream }}</span><span v-if="p.websocket" class="text-muted-foreground text-xs">WebSocket</span></span>
                        <Button variant="ghost" size="icon-sm" class="text-destructive" :aria-label="t('Remove')" @click="proxies.splice(i, 1)"><Trash2Icon /></Button>
                    </li>
                </ul>
                <p v-else class="text-muted-foreground text-sm">{{ t('No proxies yet. The site is served by PHP and static files.') }}</p>

                <div class="grid gap-3 sm:grid-cols-[10rem_1fr_auto]">
                    <Field><FieldLabel for="px-path">{{ t('Path') }}</FieldLabel><Input id="px-path" v-model="newProxy.path" class="font-mono" placeholder="/" /></Field>
                    <Field><FieldLabel for="px-up">{{ t('Upstream') }}</FieldLabel><Input id="px-up" v-model="newProxy.upstream" class="font-mono" placeholder="http://127.0.0.1:8080" /></Field>
                    <Field class="justify-end"><Button type="button" variant="outline" :disabled="!newProxy.upstream.trim()" @click="addProxy"><PlusIcon /> {{ t('Add') }}</Button></Field>
                </div>
                <div class="flex flex-wrap items-center justify-between gap-3">
                    <Field orientation="horizontal" class="w-auto"><Switch id="px-ws" v-model="newProxy.websocket" /><FieldLabel for="px-ws">{{ t('WebSocket support') }}</FieldLabel></Field>
                    <Select v-if="usable.length" :model-value="''" @update:model-value="(v) => (newProxy.upstream = String(v))">
                        <SelectTrigger class="w-full sm:w-72" :aria-label="t('Use a Docker container')"><BoxIcon class="size-4" /><SelectValue :placeholder="t('Use a Docker container')" /></SelectTrigger>
                        <SelectContent><SelectItem v-for="x in usable" :key="x.c.id" :value="x.up!">{{ x.c.name }} · {{ x.up }}</SelectItem></SelectContent>
                    </Select>
                </div>
                <p class="text-muted-foreground text-xs">{{ t('Publish container ports on 127.0.0.1 only (for example 127.0.0.1:8080:80) so the container is reachable through this site, not directly from the internet.') }}</p>
                <p class="text-muted-foreground text-xs">{{ t('Press Save below to apply changes to proxies and redirects.') }}</p>
            </CardContent>
        </Card>

        <Card>
            <CardHeader>
                <CardTitle>{{ t('Redirects') }}</CardTitle>
                <CardDescription>{{ t('A path such as /old matches exactly; /old/ matches everything below it.') }}</CardDescription>
            </CardHeader>
            <CardContent class="flex flex-col gap-4">
                <ul v-if="redirects.length" class="divide-y rounded-md border">
                    <li v-for="(r, i) in redirects" :key="i" class="flex flex-wrap items-center justify-between gap-2 px-3 py-2">
                        <span class="flex flex-wrap items-center gap-2 font-mono text-sm"><span>{{ r.from }}</span><ArrowRightIcon class="text-muted-foreground size-3.5" /><span>{{ r.to }}</span><span class="text-muted-foreground text-xs">{{ r.code }}</span></span>
                        <Button variant="ghost" size="icon-sm" class="text-destructive" :aria-label="t('Remove')" @click="redirects.splice(i, 1)"><Trash2Icon /></Button>
                    </li>
                </ul>
                <div class="grid gap-3 sm:grid-cols-[1fr_1fr_8rem_auto]">
                    <Field><FieldLabel for="rd-from">{{ t('From') }}</FieldLabel><Input id="rd-from" v-model="newRedirect.from" class="font-mono" placeholder="/old-page" /></Field>
                    <Field><FieldLabel for="rd-to">{{ t('To') }}</FieldLabel><Input id="rd-to" v-model="newRedirect.to" class="font-mono" placeholder="https://example.com/new" /></Field>
                    <Field><FieldLabel>{{ t('Redirect type') }}</FieldLabel>
                        <Select :model-value="String(newRedirect.code)" @update:model-value="(v) => (newRedirect.code = Number(v))"><SelectTrigger class="w-full"><SelectValue /></SelectTrigger><SelectContent><SelectItem value="301">301 {{ t('Permanent') }}</SelectItem><SelectItem value="302">302 {{ t('Temporary') }}</SelectItem></SelectContent></Select>
                    </Field>
                    <Field class="justify-end"><Button type="button" variant="outline" :disabled="!newRedirect.from.trim() || !newRedirect.to.trim()" @click="addRedirect"><PlusIcon /> {{ t('Add') }}</Button></Field>
                </div>
            </CardContent>
        </Card>

        <Card>
            <CardHeader>
                <CardTitle>{{ t('Performance and access') }}</CardTitle>
                <CardDescription>{{ t('Nothing is saved until you press Save.') }}</CardDescription>
            </CardHeader>
            <CardContent>
                <form class="flex max-w-lg flex-col gap-4" @submit.prevent="save">
                    <Field><FieldLabel for="w-body">{{ t('Maximum upload size (MB)') }}</FieldLabel><Input id="w-body" v-model="form.max_body_mb" type="number" min="1" max="10240" class="w-32" /></Field>
                    <Field orientation="horizontal"><Switch id="w-gzip" v-model="form.gzip" /><FieldLabel for="w-gzip">{{ t('Compress text responses (gzip)') }}</FieldLabel></Field>
                    <Field orientation="horizontal"><Switch id="w-cache" v-model="form.static_cache" /><FieldLabel for="w-cache">{{ t('Cache images, CSS and JS in the browser for 30 days') }}</FieldLabel></Field>
                    <div class="grid gap-3 sm:grid-cols-2">
                        <Field><FieldLabel for="w-user">{{ t('Password protection: user') }}</FieldLabel><Input id="w-user" v-model="form.auth_user" autocomplete="off" :placeholder="t('Leave empty for none')" /></Field>
                        <Field><FieldLabel for="w-pass">{{ t('Password (8+ characters)') }}</FieldLabel><Input id="w-pass" v-model="form.auth_password" type="password" autocomplete="new-password" :disabled="!form.auth_user.trim()" :placeholder="form.auth_user ? t('Unchanged') : ''" /></Field>
                    </div>
                    <div><Button type="submit" :disabled="busy">{{ t('Save') }}</Button></div>
                </form>
            </CardContent>
        </Card>
    </div>
    <div v-else class="flex flex-col gap-3"><Skeleton class="h-40 w-full" /><Skeleton class="h-32 w-full" /></div>
</template>

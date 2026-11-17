<script setup lang="ts">
import { reactive, ref } from 'vue';
import { Head, router } from '@inertiajs/vue3';
import { DownloadIcon, PlayIcon, PlusIcon, RotateCwIcon, ScrollTextIcon, SquareIcon, Trash2Icon } from '@lucide/vue';
import ConfirmDialog from '@/components/app/ConfirmDialog.vue';
import EmptyState from '@/components/app/EmptyState.vue';
import InstallPrompt from '@/components/app/InstallPrompt.vue';
import PageHeader from '@/components/app/PageHeader.vue';
import ServiceDown from '@/components/app/ServiceDown.vue';
import TaskPanel from '@/components/app/TaskPanel.vue';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { Card, CardHeader, CardTitle } from '@/components/ui/card';
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog';
import { Input } from '@/components/ui/input';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select';
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table';
import { Textarea } from '@/components/ui/textarea';
import { act, useBusy } from '@/composables/useActions';
import { useI18n } from '@/composables/useI18n';
import { rpc } from '@/composables/useRpc';
import { useInterval, useTask } from '@/composables/useTask';

interface Container { id: string; name: string; image: string; state: string; status: string; ports: string }
interface Image { id: string; repo: string; tag: string; size: string; created: string }
interface Docker { installed: boolean; running: boolean; version: string; containers: Container[]; images: Image[] }
const props = defineProps<{ docker: Docker | null; available: boolean }>();
const { t } = useI18n();
const { busy, run } = useBusy();
const reload = () => router.reload({ only: ['docker'], preserveScroll: true });
const { task, follow, dismiss } = useTask('system', () => reload());
useInterval(() => props.docker?.running && router.reload({ only: ['docker'], preserveScroll: true }), 8000);

async function action(c: Container, a: string) {
    const r = await run(() => act(rpc('system', 'containerAction', c.id, a)));
    if (r.ok) reload();
}
const removeOpen = ref(false);
const removing = ref<Container | null>(null);
const logOpen = ref(false);
const logOf = ref<Container | null>(null);
const logText = ref('');
async function showLog(c: Container) {
    logOf.value = c;
    logText.value = '…';
    logOpen.value = true;
    const r = await act(rpc('system', 'containerLog', c.id, 300));
    logText.value = r.ok ? (r.data || t('No output.')) : '';
}

const runOpen = ref(false);
const form = reactive({ image: '', name: '', ports: '', env: '', volumes: '', restart: 'unless-stopped', memory: '' });
const lines = (s: string) => s.split('\n').map((x) => x.trim()).filter(Boolean);
async function create() {
    const r = await run(() => act(rpc('system', 'runContainer', {
        image: form.image.trim(), name: form.name.trim(), ports: lines(form.ports), env: lines(form.env), volumes: lines(form.volumes), restart: form.restart, memory: form.memory.trim(),
    })));
    if (r.ok) { runOpen.value = false; follow(r.data?.id); }
}
const pullOpen = ref(false);
const pullName = ref('');
async function pull() {
    const r = await run(() => act(rpc('system', 'pullImage', pullName.value.trim())));
    if (r.ok) { pullOpen.value = false; pullName.value = ''; follow(r.data?.id); }
}
async function rmImage(i: Image) {
    const r = await act(rpc('system', 'removeImage', i.id), t('Image removed'));
    if (r.ok) reload();
}
const tone = (s: string) => (s === 'running' ? 'border-success/30 bg-success/10 text-success' : s === 'exited' || s === 'dead' ? 'text-muted-foreground' : 'border-warning/30 bg-warning/10 text-warning');
</script>

<template>
    <Head title="Docker" />
    <PageHeader title="Docker" :description="t('Run applications and game servers in containers.')">
        <template v-if="docker?.running">
            <Button variant="outline" @click="pullOpen = true"><DownloadIcon /> {{ t('Pull image') }}</Button>
            <Button @click="runOpen = true"><PlusIcon /> {{ t('New container') }}</Button>
        </template>
    </PageHeader>
    <ServiceDown v-if="!available" name="System" />
    <TaskPanel :task="task" @dismiss="dismiss" />

    <InstallPrompt v-if="available && docker && !docker.installed" id="docker" name="Docker" :description="t('Run applications in containers.')" :disabled="task?.status === 'running'" @started="follow" />
    <Card v-else-if="docker && !docker.running" class="border-warning/40"><div class="px-5 text-sm">{{ t('Docker is installed but not running. Start it from Software or Services.') }}</div></Card>

    <template v-if="docker?.running">
        <Card class="gap-0 py-0">
            <CardHeader class="border-b px-5 py-4"><CardTitle>{{ t('Containers') }} <span class="text-muted-foreground tabular ml-1 text-sm font-normal">{{ docker.containers.length }}</span></CardTitle></CardHeader>
            <EmptyState v-if="!docker.containers.length" class="m-4" :title="t('No containers yet')" :description="t('Start one from an image such as redis or nginx.')" />
            <Table v-else>
                <TableHeader><TableRow><TableHead class="px-5">{{ t('Name') }}</TableHead><TableHead>{{ t('Image') }}</TableHead><TableHead>{{ t('Status') }}</TableHead><TableHead>{{ t('Ports') }}</TableHead><TableHead class="px-5 text-right">{{ t('Actions') }}</TableHead></TableRow></TableHeader>
                <TableBody>
                    <TableRow v-for="c in docker.containers" :key="c.id">
                        <TableCell class="px-5"><p class="font-medium">{{ c.name }}</p><p class="text-muted-foreground font-mono text-xs">{{ c.id }}</p></TableCell>
                        <TableCell class="font-mono text-sm">{{ c.image }}</TableCell>
                        <TableCell><Badge variant="outline" :class="tone(c.state)">{{ c.state }}</Badge><p class="text-muted-foreground mt-0.5 text-xs">{{ c.status }}</p></TableCell>
                        <TableCell class="text-muted-foreground font-mono text-xs">{{ c.ports || '–' }}</TableCell>
                        <TableCell class="px-5">
                            <div class="flex justify-end gap-1">
                                <Button v-if="c.state !== 'running'" variant="outline" size="icon-sm" :aria-label="t('Start')" :title="t('Start')" :disabled="busy" @click="action(c, 'start')"><PlayIcon /></Button>
                                <Button v-else variant="outline" size="icon-sm" :aria-label="t('Stop')" :title="t('Stop')" :disabled="busy" @click="action(c, 'stop')"><SquareIcon /></Button>
                                <Button variant="outline" size="icon-sm" :aria-label="t('Restart')" :title="t('Restart')" :disabled="busy" @click="action(c, 'restart')"><RotateCwIcon /></Button>
                                <Button variant="outline" size="icon-sm" :aria-label="t('Logs')" :title="t('Logs')" @click="showLog(c)"><ScrollTextIcon /></Button>
                                <Button variant="ghost" size="icon-sm" class="text-muted-foreground hover:text-destructive" :aria-label="t('Remove')" :title="t('Remove')" @click="removing = c; removeOpen = true"><Trash2Icon /></Button>
                            </div>
                        </TableCell>
                    </TableRow>
                </TableBody>
            </Table>
        </Card>

        <Card class="gap-0 py-0">
            <CardHeader class="border-b px-5 py-4"><CardTitle>{{ t('Images') }} <span class="text-muted-foreground tabular ml-1 text-sm font-normal">{{ docker.images.length }}</span></CardTitle></CardHeader>
            <p v-if="!docker.images.length" class="text-muted-foreground px-5 py-6 text-center text-sm">{{ t('No images downloaded.') }}</p>
            <Table v-else>
                <TableHeader><TableRow><TableHead class="px-5">{{ t('Image') }}</TableHead><TableHead>{{ t('Tag') }}</TableHead><TableHead>{{ t('Size') }}</TableHead><TableHead>{{ t('Created') }}</TableHead><TableHead class="w-12" /></TableRow></TableHeader>
                <TableBody>
                    <TableRow v-for="i in docker.images" :key="i.id">
                        <TableCell class="px-5 font-mono text-sm font-medium">{{ i.repo }}</TableCell><TableCell class="font-mono text-sm">{{ i.tag }}</TableCell>
                        <TableCell class="tabular text-muted-foreground">{{ i.size }}</TableCell><TableCell class="text-muted-foreground">{{ i.created }}</TableCell>
                        <TableCell><Button variant="ghost" size="icon-sm" class="text-muted-foreground hover:text-destructive" :aria-label="t('Remove')" @click="rmImage(i)"><Trash2Icon /></Button></TableCell>
                    </TableRow>
                </TableBody>
            </Table>
        </Card>
    </template>

    <Dialog v-model:open="runOpen">
        <DialogContent class="sm:max-w-xl">
            <DialogHeader><DialogTitle>{{ t('New container') }}</DialogTitle><DialogDescription>{{ t('The image is downloaded first if it is not on the server yet.') }}</DialogDescription></DialogHeader>
            <form class="grid gap-4" @submit.prevent="create">
                <div class="grid gap-3 sm:grid-cols-2">
                    <label class="grid gap-1.5 text-sm font-medium">{{ t('Image') }}<Input v-model="form.image" required placeholder="redis:7-alpine" autocomplete="off" /></label>
                    <label class="grid gap-1.5 text-sm font-medium">{{ t('Name') }}<Input v-model="form.name" placeholder="cache" autocomplete="off" /></label>
                </div>
                <label class="grid gap-1.5 text-sm font-medium">{{ t('Ports (host:container, one per line)') }}<Textarea v-model="form.ports" rows="2" class="font-mono text-sm" placeholder="127.0.0.1:6379:6379" /></label>
                <label class="grid gap-1.5 text-sm font-medium">{{ t('Environment (NAME=value, one per line)') }}<Textarea v-model="form.env" rows="2" class="font-mono text-sm" /></label>
                <label class="grid gap-1.5 text-sm font-medium">{{ t('Volumes (/host/path:/container/path, one per line)') }}<Textarea v-model="form.volumes" rows="2" class="font-mono text-sm" placeholder="/srv/data/redis:/data" /></label>
                <div class="grid gap-3 sm:grid-cols-2">
                    <label class="grid gap-1.5 text-sm font-medium">{{ t('Restart policy') }}
                        <Select v-model="form.restart"><SelectTrigger class="w-full"><SelectValue /></SelectTrigger>
                            <SelectContent><SelectItem value="unless-stopped">{{ t('Unless stopped') }}</SelectItem><SelectItem value="always">{{ t('Always') }}</SelectItem><SelectItem value="on-failure">{{ t('On failure') }}</SelectItem><SelectItem value="no">{{ t('Never') }}</SelectItem></SelectContent></Select></label>
                    <label class="grid gap-1.5 text-sm font-medium">{{ t('Memory limit') }}<Input v-model="form.memory" placeholder="512m / 2g" autocomplete="off" /></label>
                </div>
                <DialogFooter><Button type="button" variant="outline" @click="runOpen = false">{{ t('Cancel') }}</Button><Button type="submit" :disabled="busy || !form.image">{{ t('Start') }}</Button></DialogFooter>
            </form>
        </DialogContent>
    </Dialog>

    <Dialog v-model:open="pullOpen">
        <DialogContent class="sm:max-w-md">
            <DialogHeader><DialogTitle>{{ t('Pull image') }}</DialogTitle><DialogDescription>{{ t('Download an image from Docker Hub.') }}</DialogDescription></DialogHeader>
            <form class="grid gap-4" @submit.prevent="pull">
                <Input v-model="pullName" required placeholder="nginx:alpine" autocomplete="off" :aria-label="t('Image')" />
                <DialogFooter><Button type="button" variant="outline" @click="pullOpen = false">{{ t('Cancel') }}</Button><Button type="submit" :disabled="busy || !pullName">{{ t('Pull') }}</Button></DialogFooter>
            </form>
        </DialogContent>
    </Dialog>

    <Dialog v-model:open="logOpen">
        <DialogContent class="sm:max-w-3xl">
            <DialogHeader><DialogTitle>{{ logOf?.name }}</DialogTitle><DialogDescription>{{ t('Last 300 lines') }}</DialogDescription></DialogHeader>
            <pre class="bg-muted max-h-96 overflow-auto rounded-md p-3 text-xs whitespace-pre-wrap">{{ logText }}</pre>
        </DialogContent>
    </Dialog>

    <ConfirmDialog v-model:open="removeOpen" :title="t('Remove {name}?', { name: removing?.name ?? '' })" :description="t('The container is stopped and deleted. Data stored in volumes on the host stays.')" :confirm-label="t('Remove')" @confirm="removing && action(removing, 'remove')" />
</template>

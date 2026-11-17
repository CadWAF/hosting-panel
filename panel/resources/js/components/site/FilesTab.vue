<script setup lang="ts">
import { computed, onMounted, ref } from 'vue';
import { DownloadIcon, FileIcon, FilePlusIcon, FolderIcon, FolderPlusIcon, PackageOpenIcon, PencilIcon, TextCursorInputIcon, Trash2Icon, UploadIcon, CornerLeftUpIcon } from '@lucide/vue';
import { toast } from 'vue-sonner';
import EmptyState from '@/components/app/EmptyState.vue';
import { Button } from '@/components/ui/button';
import { Card } from '@/components/ui/card';
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog';
import { Input } from '@/components/ui/input';
import { Skeleton } from '@/components/ui/skeleton';
import { Textarea } from '@/components/ui/textarea';
import { act, useBusy } from '@/composables/useActions';
import { rpc, upload } from '@/composables/useRpc';

interface Entry { name: string; dir: boolean; mode: string; size: number }
const props = defineProps<{ domain: string }>();

const path = ref(new URLSearchParams(location.search).get('dir') ?? '');
const entries = ref<Entry[] | null>(null);
const failed = ref<string | null>(null);
const { busy, run } = useBusy();
const join = (n: string) => [path.value, n].filter(Boolean).join('/');
const crumbs = computed(() => path.value.split('/').filter(Boolean).map((name, i, a) => ({ name, path: a.slice(0, i + 1).join('/') })));

async function load() {
    const r = await rpc<Entry[]>('core', 'files', props.domain, path.value);
    failed.value = r.ok ? null : r.error;
    entries.value = r.ok ? (r.data ?? []).slice().sort((a, b) => Number(b.dir) - Number(a.dir) || a.name.localeCompare(b.name)) : [];
}
onMounted(load);
function go(p: string) {
    path.value = p.replace(/^\/|\/$/g, '');
    const u = new URL(location.href);
    path.value ? u.searchParams.set('dir', path.value) : u.searchParams.delete('dir');
    history.replaceState(history.state, '', u);
    entries.value = null;
    load();
}
const up = () => go(path.value.split('/').slice(0, -1).join('/'));

// create
const newName = ref('');
async function newFile() {
    const n = newName.value.trim();
    if (n && (await act(rpc('core', 'writeFile', props.domain, join(n), ''))).ok) { newName.value = ''; load(); }
}
async function newFolder() {
    const n = newName.value.trim();
    if (n && (await act(rpc('core', 'fileOp', props.domain, 'mkdir', { path: join(n) }))).ok) { newName.value = ''; load(); }
}
async function onUpload(ev: Event) {
    const input = ev.target as HTMLInputElement;
    const f = input.files?.[0];
    if (!f) return;
    const r = await run(() => upload(props.domain, path.value, f));
    r.ok ? toast.success(`Uploaded ${f.name}`) : toast.error(r.error ?? 'Upload failed');
    input.value = '';
    load();
}

// rename
const renaming = ref<Entry | null>(null);
const renameOpen = ref(false);
const renameTo = ref('');
function startRename(e: Entry) { renaming.value = e; renameTo.value = e.name; renameOpen.value = true; }
async function rename() {
    if (!renaming.value) return;
    const r = await act(rpc('core', 'fileOp', props.domain, 'rename', { from: join(renaming.value.name), to: join(renameTo.value.trim()) }));
    if (r.ok) { renameOpen.value = false; load(); }
}

async function trash(e: Entry) {
    if ((await act(rpc('core', 'fileOp', props.domain, 'trash', { path: join(e.name) }), `${e.name} moved to trash`)).ok) load();
}
async function extract(e: Entry) {
    if ((await act(rpc('core', 'fileOp', props.domain, 'extract', { path: join(e.name), dest: path.value }), `Extracted ${e.name}`)).ok) load();
}

// editor
const editPath = ref<string | null>(null);
const editOpen = ref(false);
const content = ref('');
async function edit(e: Entry) {
    const r = await act(rpc<string>('core', 'readFile', props.domain, join(e.name)));
    if (r.ok) { editPath.value = join(e.name); content.value = String(r.data ?? ''); editOpen.value = true; }
}
async function save() {
    if (!editPath.value) return;
    const r = await run(() => act(rpc('core', 'writeFile', props.domain, editPath.value, content.value), `Saved ${editPath.value!.split('/').pop()}`));
    if (r.ok) { editOpen.value = false; load(); }
}
const dl = (e: Entry) => `/sites/${encodeURIComponent(props.domain)}/download?path=${encodeURIComponent(join(e.name))}`;
const kb = (n: number) => (n / 1024).toFixed(1) + ' KB';
</script>

<template>
    <div class="flex flex-col gap-4">
        <div class="flex flex-wrap items-center justify-between gap-3">
            <nav class="flex flex-wrap items-center gap-1 text-sm" aria-label="Path">
                <Button variant="ghost" size="sm" @click="go('')">~</Button>
                <template v-for="c in crumbs" :key="c.path"><span class="text-muted-foreground">/</span><Button variant="ghost" size="sm" @click="go(c.path)">{{ c.name }}</Button></template>
            </nav>
            <div class="flex flex-wrap items-center gap-2">
                <Input v-model="newName" placeholder="Name" aria-label="Name for new file or folder" class="w-40" @keydown.enter.prevent="newFile" />
                <Button variant="outline" size="sm" :disabled="!newName.trim()" @click="newFile"><FilePlusIcon /> File</Button>
                <Button variant="outline" size="sm" :disabled="!newName.trim()" @click="newFolder"><FolderPlusIcon /> Folder</Button>
                <Button as-child size="sm" :disabled="busy"><label class="cursor-pointer"><UploadIcon /> {{ busy ? 'Uploading…' : 'Upload' }}<input type="file" class="sr-only" @change="onUpload" /></label></Button>
            </div>
        </div>

        <p v-if="failed" role="alert" class="text-destructive text-sm">{{ failed }}</p>
        <Card v-if="entries === null" class="gap-2 p-4"><Skeleton v-for="i in 4" :key="i" class="h-8 w-full" /></Card>
        <Card v-else class="gap-0 py-0">
            <ul class="divide-y">
                <li v-if="path" class="p-2"><Button variant="ghost" size="sm" @click="up"><CornerLeftUpIcon /> ..</Button></li>
                <li v-for="e in entries" :key="e.name" class="flex flex-wrap items-center justify-between gap-2 px-4 py-2">
                    <div class="flex min-w-0 items-center gap-2">
                        <component :is="e.dir ? FolderIcon : FileIcon" class="text-muted-foreground size-4 shrink-0" />
                        <button v-if="e.dir" class="truncate font-medium hover:underline" @click="go(join(e.name))">{{ e.name }}</button>
                        <span v-else class="truncate">{{ e.name }}</span>
                        <span class="text-muted-foreground shrink-0 font-mono text-xs">{{ e.mode }}<template v-if="!e.dir"> · {{ kb(e.size) }}</template></span>
                    </div>
                    <div class="flex gap-1">
                        <Button v-if="!e.dir" variant="ghost" size="icon-sm" :aria-label="`Edit ${e.name}`" @click="edit(e)"><PencilIcon /></Button>
                        <Button v-if="!e.dir" as-child variant="ghost" size="icon-sm"><a :href="dl(e)" :aria-label="`Download ${e.name}`"><DownloadIcon /></a></Button>
                        <Button v-if="e.name.toLowerCase().endsWith('.zip')" variant="ghost" size="icon-sm" :aria-label="`Extract ${e.name}`" @click="extract(e)"><PackageOpenIcon /></Button>
                        <Button variant="ghost" size="icon-sm" :aria-label="`Rename ${e.name}`" @click="startRename(e)"><TextCursorInputIcon /></Button>
                        <Button variant="ghost" size="icon-sm" class="text-destructive" :aria-label="`Move ${e.name} to trash`" @click="trash(e)"><Trash2Icon /></Button>
                    </div>
                </li>
            </ul>
            <EmptyState v-if="!entries.length && !path" title="This folder is empty" class="border-0" />
            <p v-else-if="!entries.length" class="text-muted-foreground p-6 text-center text-sm">This folder is empty</p>
        </Card>
    </div>

    <Dialog v-model:open="renameOpen">
        <DialogContent class="sm:max-w-sm">
            <form class="flex flex-col gap-4" @submit.prevent="rename">
                <DialogHeader><DialogTitle>Rename</DialogTitle><DialogDescription>{{ renaming?.name }}</DialogDescription></DialogHeader>
                <Input v-model="renameTo" aria-label="New name" required />
                <DialogFooter><Button type="button" variant="outline" @click="renameOpen = false">Cancel</Button><Button type="submit">Rename</Button></DialogFooter>
            </form>
        </DialogContent>
    </Dialog>

    <Dialog v-model:open="editOpen">
        <DialogContent class="flex h-[85svh] flex-col sm:max-w-4xl">
            <DialogHeader><DialogTitle class="truncate font-mono text-base">{{ editPath }}</DialogTitle><DialogDescription>Edit the file's contents.</DialogDescription></DialogHeader>
            <Textarea v-model="content" class="min-h-0 flex-1 resize-none font-mono text-sm" spellcheck="false" aria-label="File contents" />
            <DialogFooter><Button variant="outline" @click="editOpen = false">Cancel</Button><Button :disabled="busy" @click="save">Save</Button></DialogFooter>
        </DialogContent>
    </Dialog>
</template>

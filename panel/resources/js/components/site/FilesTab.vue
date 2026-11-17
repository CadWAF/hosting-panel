<script setup lang="ts">
import { computed, onMounted, ref } from 'vue';
import {
    ArchiveIcon, CopyIcon, CornerLeftUpIcon, DownloadIcon, EyeIcon, FileIcon, FilePlusIcon, FolderIcon, FolderInputIcon, FolderPlusIcon, LockIcon,
    PackageOpenIcon, PencilIcon, SearchIcon, TextCursorInputIcon, Trash2Icon, UploadIcon, XIcon,
} from '@lucide/vue';
import { toast } from 'vue-sonner';
import EmptyState from '@/components/app/EmptyState.vue';
import { Button } from '@/components/ui/button';
import { Card } from '@/components/ui/card';
import { Checkbox } from '@/components/ui/checkbox';
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog';
import { Input } from '@/components/ui/input';
import { Skeleton } from '@/components/ui/skeleton';
import { Textarea } from '@/components/ui/textarea';
import { act, useBusy } from '@/composables/useActions';
import { rpc, upload } from '@/composables/useRpc';
import { useI18n } from '@/composables/useI18n';
import { ago, bytes } from '@/lib/format';

const { t } = useI18n();

interface Entry { name: string; dir: boolean; mode: string; size: number; mod_time: string; symlink: boolean }
interface Hit { path: string; dir: boolean; size: number }
const props = defineProps<{ domain: string }>();

const path = ref(new URLSearchParams(location.search).get('dir') ?? '');
const entries = ref<Entry[] | null>(null);
const failed = ref<string | null>(null);
const { busy, run } = useBusy();
const join = (n: string, base = path.value) => [base, n].filter(Boolean).join('/');
const crumbs = computed(() => path.value.split('/').filter(Boolean).map((name, i, a) => ({ name, path: a.slice(0, i + 1).join('/') })));
const sort = ref<'name' | 'size' | 'date'>('name');
const filter = ref('');

async function load() {
    const r = await rpc<Entry[]>('core', 'files', props.domain, path.value);
    failed.value = r.ok ? null : r.error;
    entries.value = r.ok ? (r.data ?? []) : [];
    selected.value = new Set();
}
onMounted(load);
function go(p: string) {
    path.value = p.replace(/^\/|\/$/g, '');
    const u = new URL(location.href);
    path.value ? u.searchParams.set('dir', path.value) : u.searchParams.delete('dir');
    history.replaceState(history.state, '', u);
    entries.value = null;
    filter.value = '';
    hits.value = null;
    load();
}
const up = () => go(path.value.split('/').slice(0, -1).join('/'));

const shown = computed(() => {
    const q = filter.value.trim().toLowerCase();
    const l = (entries.value ?? []).filter((e) => !q || e.name.toLowerCase().includes(q));
    const by = sort.value;
    return l.sort((a, b) => Number(b.dir) - Number(a.dir)
        || (by === 'size' ? b.size - a.size : by === 'date' ? b.mod_time.localeCompare(a.mod_time) : a.name.localeCompare(b.name, undefined, { numeric: true })));
});

// ---- selection ----
const selected = ref<Set<string>>(new Set());
const allSelected = computed(() => shown.value.length > 0 && shown.value.every((e) => selected.value.has(e.name)));
function toggle(n: string, on: boolean) {
    const s = new Set(selected.value);
    on ? s.add(n) : s.delete(n);
    selected.value = s;
}
function toggleAll(on: boolean) { selected.value = on ? new Set(shown.value.map((e) => e.name)) : new Set(); }
const picked = computed(() => [...selected.value]);

// ---- create ----
const createOpen = ref(false);
const createKind = ref<'file' | 'folder'>('file');
const createName = ref('');
function startCreate(k: 'file' | 'folder') { createKind.value = k; createName.value = ''; createOpen.value = true; }
async function create() {
    const n = createName.value.trim();
    if (!n) return;
    const r = createKind.value === 'file'
        ? await act(rpc('core', 'writeFile', props.domain, join(n), ''))
        : await act(rpc('core', 'fileOp', props.domain, 'mkdir', { path: join(n) }));
    if (r.ok) { createOpen.value = false; load(); }
}

// ---- upload (button or drag & drop, several files) ----
const progress = ref<{ done: number; total: number } | null>(null);
const dragging = ref(false);
async function uploadFiles(files: File[]) {
    if (!files.length) return;
    progress.value = { done: 0, total: files.length };
    let failedN = 0;
    for (const f of files) {
        const r = await upload(props.domain, path.value, f);
        if (!r.ok) { failedN++; toast.error(`${f.name}: ${r.error ?? t('Upload failed')}`); }
        progress.value = { done: progress.value.done + 1, total: files.length };
    }
    progress.value = null;
    if (failedN < files.length) toast.success(t('Uploaded {name}', { name: files.length === 1 ? files[0].name : `${files.length - failedN} files` }));
    load();
}
const onPick = (ev: Event) => { const i = ev.target as HTMLInputElement; uploadFiles([...(i.files ?? [])]); i.value = ''; };
function onDrop(ev: DragEvent) { dragging.value = false; uploadFiles([...(ev.dataTransfer?.files ?? [])]); }

// ---- rename ----
const renaming = ref<Entry | null>(null);
const renameOpen = ref(false);
const renameTo = ref('');
function startRename(e: Entry) { renaming.value = e; renameTo.value = e.name; renameOpen.value = true; }
async function rename() {
    if (!renaming.value) return;
    const r = await act(rpc('core', 'fileOp', props.domain, 'rename', { from: join(renaming.value.name), to: join(renameTo.value.trim()) }));
    if (r.ok) { renameOpen.value = false; load(); }
}

// ---- copy / move ----
const moveOpen = ref(false);
const moveMode = ref<'copy' | 'move'>('copy');
const moveDest = ref('');
function startMove(mode: 'copy' | 'move') { moveMode.value = mode; moveDest.value = path.value; moveOpen.value = true; }
async function doMove() {
    const dest = moveDest.value.trim().replace(/^\/|\/$/g, '');
    let ok = 0;
    for (const n of picked.value) {
        const to = join(n, dest);
        const r = await act(rpc('core', 'fileOp', props.domain, moveMode.value === 'copy' ? 'copy' : 'rename', { from: join(n), to }));
        if (!r.ok) break;
        ok++;
    }
    if (ok) { toast.success(moveMode.value === 'copy' ? t('Copied {n} items', { n: ok }) : t('Moved {n} items', { n: ok })); moveOpen.value = false; load(); }
}

// ---- delete / archive / extract ----
async function trashPicked() {
    for (const n of picked.value) if (!(await act(rpc('core', 'fileOp', props.domain, 'trash', { path: join(n) }))).ok) break;
    toast.success(t('Moved to trash'));
    load();
}
async function trash(e: Entry) {
    if ((await act(rpc('core', 'fileOp', props.domain, 'trash', { path: join(e.name) }), t('Moved to trash'))).ok) load();
}
const archiveOpen = ref(false);
const archiveName = ref('');
function startArchive() { archiveName.value = (picked.value.length === 1 ? picked.value[0] : 'archive') + '.zip'; archiveOpen.value = true; }
async function doArchive() {
    const r = await run(() => act(rpc('core', 'fileOp', props.domain, 'archive', { paths: picked.value.map((n) => join(n)), dest: join(archiveName.value.trim()) }), t('Archive created')));
    if (r.ok) { archiveOpen.value = false; load(); }
}
async function extract(e: Entry) {
    if ((await act(rpc('core', 'fileOp', props.domain, 'extract', { path: join(e.name), dest: path.value }), t('Extracted {name}', { name: e.name }))).ok) load();
}
const zipUrl = computed(() => `/sites/${encodeURIComponent(props.domain)}/zip?` + picked.value.map((n) => 'p[]=' + encodeURIComponent(join(n))).join('&'));

// ---- permissions ----
const chmodOpen = ref(false);
const chmodTarget = ref<Entry | null>(null);
const chmodMode = ref('0644');
function startChmod(e: Entry) { chmodTarget.value = e; chmodMode.value = e.mode; chmodOpen.value = true; }
async function chmod() {
    if (!chmodTarget.value) return;
    const r = await act(rpc('core', 'fileOp', props.domain, 'chmod', { path: join(chmodTarget.value.name), mode: chmodMode.value.trim() }), t('Permissions changed'));
    if (r.ok) { chmodOpen.value = false; load(); }
}

// ---- search ----
const searchQ = ref('');
const hits = ref<Hit[] | null>(null);
async function search() {
    if (searchQ.value.trim().length < 2) { hits.value = null; return; }
    const r = await act(rpc<Hit[]>('core', 'searchFiles', props.domain, path.value, searchQ.value.trim()));
    hits.value = r.ok ? (r.data ?? []) : null;
}
function openHit(h: Hit) {
    searchQ.value = '';
    go(h.dir ? h.path : h.path.split('/').slice(0, -1).join('/'));
}

// ---- editor / preview ----
const editPath = ref<string | null>(null);
const editOpen = ref(false);
const content = ref('');
const original = ref('');
async function edit(e: Entry) {
    const r = await act(rpc<string>('core', 'readFile', props.domain, join(e.name)));
    if (r.ok) { editPath.value = join(e.name); content.value = original.value = String(r.data ?? ''); editOpen.value = true; }
}
async function save() {
    if (!editPath.value) return;
    const r = await run(() => act(rpc('core', 'writeFile', props.domain, editPath.value, content.value), t('Saved {name}', { name: editPath.value!.split('/').pop() })));
    if (r.ok) { original.value = content.value; load(); }
}
function onEditorOpen(o: boolean) {
    if (!o && content.value !== original.value && !confirm(t('Discard unsaved changes?'))) { editOpen.value = true; return; }
    editOpen.value = o;
}
function onKey(ev: KeyboardEvent) {
    if ((ev.ctrlKey || ev.metaKey) && ev.key.toLowerCase() === 's') { ev.preventDefault(); save(); }
    if (ev.key === 'Tab' && !ev.shiftKey) {
        ev.preventDefault();
        const ta = ev.target as HTMLTextAreaElement;
        const s = ta.selectionStart;
        content.value = content.value.slice(0, s) + '    ' + content.value.slice(ta.selectionEnd);
        requestAnimationFrame(() => { ta.selectionStart = ta.selectionEnd = s + 4; });
    }
}
const previewable = (n: string) => /\.(png|jpe?g|gif|webp|pdf)$/i.test(n);
const isImage = (n: string) => /\.(png|jpe?g|gif|webp)$/i.test(n);
const previewPath = ref<string | null>(null);
const previewOpen = ref(false);
function preview(e: Entry) { previewPath.value = join(e.name); previewOpen.value = true; }
const dl = (e: Entry) => `/sites/${encodeURIComponent(props.domain)}/download?path=${encodeURIComponent(join(e.name))}`;
const inline = (p: string) => `/sites/${encodeURIComponent(props.domain)}/download?inline=1&path=${encodeURIComponent(p)}`;
</script>

<template>
    <div class="relative flex flex-col gap-4" @dragover.prevent="dragging = true" @dragleave.self="dragging = false" @drop.prevent="onDrop">
        <div v-if="dragging" class="border-primary bg-primary/5 text-primary pointer-events-none absolute inset-0 z-10 grid place-items-center rounded-lg border-2 border-dashed text-sm font-medium">{{ t('Drop files to upload here') }}</div>

        <div class="flex flex-wrap items-center justify-between gap-3">
            <nav class="flex flex-wrap items-center gap-1 text-sm" :aria-label="t('Path')">
                <Button variant="ghost" size="sm" @click="go('')">~</Button>
                <template v-for="c in crumbs" :key="c.path"><span class="text-muted-foreground">/</span><Button variant="ghost" size="sm" @click="go(c.path)">{{ c.name }}</Button></template>
            </nav>
            <div class="flex flex-wrap items-center gap-2">
                <form class="relative" @submit.prevent="search">
                    <SearchIcon class="text-muted-foreground pointer-events-none absolute top-1/2 left-2.5 size-4 -translate-y-1/2" />
                    <Input v-model="searchQ" class="w-44 pl-8" :placeholder="t('Search in this folder')" :aria-label="t('Search in this folder')" @input="hits = null" />
                </form>
                <Button variant="outline" size="sm" @click="startCreate('file')"><FilePlusIcon /> {{ t('File') }}</Button>
                <Button variant="outline" size="sm" @click="startCreate('folder')"><FolderPlusIcon /> {{ t('Folder') }}</Button>
                <Button as-child size="sm" :disabled="!!progress"><label class="cursor-pointer"><UploadIcon /> {{ progress ? `${progress.done}/${progress.total}` : t('Upload') }}<input type="file" multiple class="sr-only" @change="onPick" /></label></Button>
            </div>
        </div>

        <div v-if="selected.size" class="bg-card flex flex-wrap items-center justify-between gap-2 rounded-lg border px-4 py-2" role="toolbar" :aria-label="t('Selection')">
            <span class="text-sm font-medium">{{ t('{n} selected', { n: selected.size }) }}</span>
            <div class="flex flex-wrap gap-2">
                <Button variant="outline" size="sm" @click="startMove('copy')"><CopyIcon /> {{ t('Copy') }}</Button>
                <Button variant="outline" size="sm" @click="startMove('move')"><FolderInputIcon /> {{ t('Move') }}</Button>
                <Button variant="outline" size="sm" @click="startArchive"><ArchiveIcon /> {{ t('Zip here') }}</Button>
                <Button as-child variant="outline" size="sm"><a :href="zipUrl"><DownloadIcon /> {{ t('Download zip') }}</a></Button>
                <Button variant="outline" size="sm" class="text-destructive" @click="trashPicked"><Trash2Icon /> {{ t('Trash') }}</Button>
                <Button variant="ghost" size="icon-sm" :aria-label="t('Clear selection')" @click="selected = new Set()"><XIcon /></Button>
            </div>
        </div>

        <p v-if="failed" role="alert" class="text-destructive text-sm">{{ failed }}</p>

        <Card v-if="hits" class="gap-0 py-0">
            <p class="text-muted-foreground border-b px-4 py-2 text-sm">{{ t('{n} matches for "{q}"', { n: hits.length, q: searchQ }) }}</p>
            <ul class="divide-y">
                <li v-for="h in hits" :key="h.path" class="flex items-center gap-2 px-4 py-2">
                    <component :is="h.dir ? FolderIcon : FileIcon" class="text-muted-foreground size-4 shrink-0" />
                    <button class="truncate font-mono text-sm hover:underline" @click="openHit(h)">{{ h.path }}</button>
                </li>
            </ul>
            <p v-if="!hits.length" class="text-muted-foreground p-6 text-center text-sm">{{ t('Nothing found') }}</p>
        </Card>

        <Card v-if="entries === null" class="gap-2 p-4"><Skeleton v-for="i in 4" :key="i" class="h-8 w-full" /></Card>
        <Card v-else-if="!hits" class="gap-0 py-0">
            <div class="text-muted-foreground flex items-center gap-3 border-b px-4 py-2 text-xs">
                <Checkbox :model-value="allSelected" :aria-label="t('Select all')" @update:model-value="(v) => toggleAll(!!v)" />
                <button class="hover:text-foreground" :class="sort === 'name' && 'text-foreground font-medium'" @click="sort = 'name'">{{ t('Name') }}</button>
                <button class="hover:text-foreground" :class="sort === 'size' && 'text-foreground font-medium'" @click="sort = 'size'">{{ t('Size') }}</button>
                <button class="hover:text-foreground" :class="sort === 'date' && 'text-foreground font-medium'" @click="sort = 'date'">{{ t('Modified') }}</button>
                <Input v-model="filter" class="ml-auto h-7 w-40 text-xs" :placeholder="t('Filter')" :aria-label="t('Filter')" />
            </div>
            <ul class="divide-y">
                <li v-if="path" class="p-2"><Button variant="ghost" size="sm" @click="up"><CornerLeftUpIcon /> ..</Button></li>
                <li v-for="e in shown" :key="e.name" class="hover:bg-muted/40 flex flex-wrap items-center justify-between gap-2 px-4 py-2">
                    <div class="flex min-w-0 items-center gap-3">
                        <Checkbox :model-value="selected.has(e.name)" :aria-label="`${t('Select')} ${e.name}`" @update:model-value="(v) => toggle(e.name, !!v)" />
                        <component :is="e.dir ? FolderIcon : FileIcon" class="text-muted-foreground size-4 shrink-0" />
                        <button v-if="e.dir" class="truncate font-medium hover:underline" @click="go(join(e.name))">{{ e.name }}</button>
                        <button v-else-if="previewable(e.name)" class="truncate hover:underline" @click="preview(e)">{{ e.name }}</button>
                        <span v-else class="truncate">{{ e.name }}</span>
                        <span class="text-muted-foreground tabular hidden shrink-0 text-xs sm:inline"><template v-if="!e.dir">{{ bytes(e.size) }} · </template>{{ ago(e.mod_time) }}</span>
                    </div>
                    <div class="flex gap-1">
                        <Button v-if="!e.dir && previewable(e.name)" variant="ghost" size="icon-sm" :aria-label="`${t('Preview')} ${e.name}`" @click="preview(e)"><EyeIcon /></Button>
                        <Button v-else-if="!e.dir" variant="ghost" size="icon-sm" :aria-label="`${t('Edit')} ${e.name}`" @click="edit(e)"><PencilIcon /></Button>
                        <Button v-if="!e.dir" as-child variant="ghost" size="icon-sm"><a :href="dl(e)" :aria-label="`${t('Download')} ${e.name}`"><DownloadIcon /></a></Button>
                        <Button v-if="e.name.toLowerCase().endsWith('.zip')" variant="ghost" size="icon-sm" :aria-label="`${t('Extract')} ${e.name}`" @click="extract(e)"><PackageOpenIcon /></Button>
                        <Button variant="ghost" size="icon-sm" :aria-label="`${t('Permissions')} ${e.name}`" @click="startChmod(e)"><LockIcon /></Button>
                        <Button variant="ghost" size="icon-sm" :aria-label="`${t('Rename')} ${e.name}`" @click="startRename(e)"><TextCursorInputIcon /></Button>
                        <Button variant="ghost" size="icon-sm" class="text-destructive" :aria-label="t('Move {name} to trash', { name: e.name })" @click="trash(e)"><Trash2Icon /></Button>
                    </div>
                </li>
            </ul>
            <EmptyState v-if="!entries.length && !path" :title="t('This folder is empty')" :description="t('Drop files here or use Upload.')" class="border-0" />
            <p v-else-if="!shown.length" class="text-muted-foreground p-6 text-center text-sm">{{ t('This folder is empty') }}</p>
        </Card>
    </div>

    <Dialog v-model:open="createOpen">
        <DialogContent class="sm:max-w-sm">
            <form class="flex flex-col gap-4" @submit.prevent="create">
                <DialogHeader><DialogTitle>{{ createKind === 'file' ? t('New file') : t('New folder') }}</DialogTitle><DialogDescription>~/{{ path }}</DialogDescription></DialogHeader>
                <Input v-model="createName" :aria-label="t('Name')" required autofocus />
                <DialogFooter><Button type="button" variant="outline" @click="createOpen = false">{{ t('Cancel') }}</Button><Button type="submit">{{ t('Create') }}</Button></DialogFooter>
            </form>
        </DialogContent>
    </Dialog>

    <Dialog v-model:open="renameOpen">
        <DialogContent class="sm:max-w-sm">
            <form class="flex flex-col gap-4" @submit.prevent="rename">
                <DialogHeader><DialogTitle>{{ t('Rename') }}</DialogTitle><DialogDescription>{{ renaming?.name }}</DialogDescription></DialogHeader>
                <Input v-model="renameTo" :aria-label="t('New name')" required />
                <DialogFooter><Button type="button" variant="outline" @click="renameOpen = false">{{ t('Cancel') }}</Button><Button type="submit">{{ t('Rename') }}</Button></DialogFooter>
            </form>
        </DialogContent>
    </Dialog>

    <Dialog v-model:open="moveOpen">
        <DialogContent class="sm:max-w-md">
            <form class="flex flex-col gap-4" @submit.prevent="doMove">
                <DialogHeader><DialogTitle>{{ moveMode === 'copy' ? t('Copy to…') : t('Move to…') }}</DialogTitle><DialogDescription>{{ t('{n} selected', { n: selected.size }) }}. {{ t('Existing files are never overwritten.') }}</DialogDescription></DialogHeader>
                <Input v-model="moveDest" class="font-mono" :aria-label="t('Destination folder')" placeholder="public_html/uploads" />
                <DialogFooter><Button type="button" variant="outline" @click="moveOpen = false">{{ t('Cancel') }}</Button><Button type="submit">{{ moveMode === 'copy' ? t('Copy') : t('Move') }}</Button></DialogFooter>
            </form>
        </DialogContent>
    </Dialog>

    <Dialog v-model:open="archiveOpen">
        <DialogContent class="sm:max-w-sm">
            <form class="flex flex-col gap-4" @submit.prevent="doArchive">
                <DialogHeader><DialogTitle>{{ t('Create zip archive') }}</DialogTitle><DialogDescription>{{ t('{n} selected', { n: selected.size }) }}</DialogDescription></DialogHeader>
                <Input v-model="archiveName" class="font-mono" :aria-label="t('Archive name')" required />
                <DialogFooter><Button type="button" variant="outline" @click="archiveOpen = false">{{ t('Cancel') }}</Button><Button type="submit" :disabled="busy">{{ t('Create') }}</Button></DialogFooter>
            </form>
        </DialogContent>
    </Dialog>

    <Dialog v-model:open="chmodOpen">
        <DialogContent class="sm:max-w-sm">
            <form class="flex flex-col gap-4" @submit.prevent="chmod">
                <DialogHeader><DialogTitle>{{ t('Permissions') }}</DialogTitle><DialogDescription>{{ chmodTarget?.name }}</DialogDescription></DialogHeader>
                <Input v-model="chmodMode" class="font-mono" pattern="[0-7]{3,4}" :aria-label="t('Mode, for example 0644')" required />
                <div class="flex gap-2"><Button v-for="m in ['0644', '0755', '0600', '0700']" :key="m" type="button" variant="outline" size="sm" class="font-mono" @click="chmodMode = m">{{ m }}</Button></div>
                <DialogFooter><Button type="button" variant="outline" @click="chmodOpen = false">{{ t('Cancel') }}</Button><Button type="submit">{{ t('Save') }}</Button></DialogFooter>
            </form>
        </DialogContent>
    </Dialog>

    <Dialog v-model:open="previewOpen">
        <DialogContent class="flex max-h-[90svh] flex-col sm:max-w-4xl">
            <DialogHeader><DialogTitle class="truncate font-mono text-base">{{ previewPath }}</DialogTitle><DialogDescription>{{ t('Preview') }}</DialogDescription></DialogHeader>
            <div class="min-h-0 flex-1 overflow-auto">
                <img v-if="previewPath && isImage(previewPath)" :src="inline(previewPath)" :alt="previewPath" class="mx-auto max-h-[70svh] object-contain" />
                <iframe v-else-if="previewPath" :src="inline(previewPath)" sandbox="" class="h-[70svh] w-full rounded-md border" :title="previewPath" />
            </div>
        </DialogContent>
    </Dialog>

    <Dialog :open="editOpen" @update:open="onEditorOpen">
        <DialogContent class="flex h-[85svh] flex-col sm:max-w-4xl">
            <DialogHeader><DialogTitle class="truncate font-mono text-base">{{ editPath }}<span v-if="content !== original" class="text-warning ml-2 text-xs">● {{ t('unsaved') }}</span></DialogTitle><DialogDescription>{{ t('Ctrl+S saves. Tab inserts four spaces.') }}</DialogDescription></DialogHeader>
            <Textarea v-model="content" class="min-h-0 flex-1 resize-none font-mono text-sm" spellcheck="false" wrap="off" :aria-label="t('File contents')" @keydown="onKey" />
            <DialogFooter><Button variant="outline" @click="onEditorOpen(false)">{{ t('Close') }}</Button><Button :disabled="busy || content === original" @click="save">{{ t('Save') }}</Button></DialogFooter>
        </DialogContent>
    </Dialog>
</template>

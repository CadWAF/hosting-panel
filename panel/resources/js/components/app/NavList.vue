<script setup lang="ts">
import { computed } from 'vue';
import { Link, usePage } from '@inertiajs/vue3';
import {
    ArchiveIcon, CalendarClockIcon, CodeXmlIcon, ContainerIcon, DatabaseIcon, FolderUpIcon, GlobeIcon, LayoutDashboardIcon, ListChecksIcon,
    MailIcon, PackageIcon, RefreshCwIcon, ScrollTextIcon, ServerCogIcon, ShieldCheckIcon,
} from '@lucide/vue';
import { useI18n } from '@/composables/useI18n';

const emit = defineEmits<{ navigate: [] }>();
const { t } = useI18n();
const page = usePage();
const groups = [
    { label: 'Overview', items: [{ label: 'Dashboard', href: '/', icon: LayoutDashboardIcon }] },
    {
        label: 'Hosting',
        items: [
            { label: 'Sites', href: '/sites', icon: GlobeIcon },
            { label: 'Databases', href: '/databases', icon: DatabaseIcon },
            { label: 'Mail', href: '/mail', icon: MailIcon },
            { label: 'FTP', href: '/ftp', icon: FolderUpIcon },
            { label: 'Backups', href: '/backups', icon: ArchiveIcon },
            { label: 'PHP', href: '/php', icon: CodeXmlIcon },
        ],
    },
    {
        label: 'Server',
        items: [
            { label: 'Software', href: '/software', icon: PackageIcon },
            { label: 'Docker', href: '/docker', icon: ContainerIcon },
            { label: 'Security', href: '/security', icon: ShieldCheckIcon },
            { label: 'Scheduler', href: '/scheduler', icon: CalendarClockIcon },
            { label: 'Logs', href: '/logs', icon: ScrollTextIcon },
            { label: 'Updates', href: '/updates', icon: RefreshCwIcon },
        ],
    },
    {
        label: 'System',
        items: [
            { label: 'Services', href: '/services', icon: ServerCogIcon },
            { label: 'Tasks', href: '/tasks', icon: ListChecksIcon },
        ],
    },
];
const path = computed(() => page.url.split('?')[0]);
const isActive = (href: string) => (href === '/' ? path.value === '/' : path.value === href || path.value.startsWith(href + '/'));
</script>

<template>
    <nav class="flex flex-col gap-5" aria-label="Main">
        <div v-for="g in groups" :key="g.label" class="flex flex-col gap-0.5">
            <p class="text-sidebar-muted px-3 pb-1 text-[0.6875rem] font-semibold tracking-[0.06em] uppercase">{{ t(g.label) }}</p>
            <Link
                v-for="n in g.items" :key="n.href" :href="n.href" prefetch
                class="text-sidebar-foreground/75 hover:bg-sidebar-accent hover:text-sidebar-foreground relative flex items-center gap-2.5 rounded-md px-3 py-1.5 text-sm font-medium transition-colors"
                :class="{ 'bg-sidebar-accent !text-sidebar-foreground': isActive(n.href) }"
                :aria-current="isActive(n.href) ? 'page' : undefined"
                @click="emit('navigate')"
            >
                <span v-if="isActive(n.href)" class="bg-sidebar-primary absolute top-1.5 bottom-1.5 left-0 w-0.5 rounded-full" aria-hidden="true" />
                <component :is="n.icon" class="size-4 shrink-0" :class="isActive(n.href) ? 'text-sidebar-primary' : 'opacity-80'" />
                {{ t(n.label) }}
            </Link>
        </div>
    </nav>
</template>

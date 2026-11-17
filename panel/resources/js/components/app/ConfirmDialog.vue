<script setup lang="ts">
import { computed, ref, watch } from 'vue';
import {
    AlertDialog, AlertDialogAction, AlertDialogCancel, AlertDialogContent, AlertDialogDescription, AlertDialogFooter, AlertDialogHeader, AlertDialogTitle,
} from '@/components/ui/alert-dialog';
import { Field, FieldLabel } from '@/components/ui/field';
import { Input } from '@/components/ui/input';
import { buttonVariants } from '@/components/ui/button';

const props = defineProps<{
    title: string;
    description: string;
    confirmLabel?: string;
    /** When set, the user must type this text to enable the action (irreversible deletes). */
    typeName?: string;
}>();
const open = defineModel<boolean>('open', { default: false });
const emit = defineEmits<{ confirm: [] }>();

const typed = ref('');
watch(open, (v) => v && (typed.value = ''));
const ready = computed(() => !props.typeName || typed.value === props.typeName);
</script>

<template>
    <AlertDialog v-model:open="open">
        <AlertDialogContent>
            <AlertDialogHeader>
                <AlertDialogTitle>{{ title }}</AlertDialogTitle>
                <AlertDialogDescription>{{ description }}</AlertDialogDescription>
            </AlertDialogHeader>
            <Field v-if="typeName">
                <FieldLabel for="confirm-name">Type <code class="text-foreground">{{ typeName }}</code> to confirm</FieldLabel>
                <Input id="confirm-name" v-model="typed" autocomplete="off" />
            </Field>
            <AlertDialogFooter>
                <AlertDialogCancel>Cancel</AlertDialogCancel>
                <AlertDialogAction :class="buttonVariants({ variant: 'destructive' })" :disabled="!ready" @click="emit('confirm')">
                    {{ confirmLabel ?? 'Delete' }}
                </AlertDialogAction>
            </AlertDialogFooter>
        </AlertDialogContent>
    </AlertDialog>
</template>

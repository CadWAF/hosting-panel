<script setup lang="ts">
import { nextTick, ref } from 'vue';
import { Head } from '@inertiajs/vue3';
import { Alert, AlertDescription } from '@/components/ui/alert';
import { Button } from '@/components/ui/button';
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card';
import { Field, FieldGroup, FieldLabel } from '@/components/ui/field';
import { Input } from '@/components/ui/input';
import { Spinner } from '@/components/ui/spinner';
import Logo from '@/components/app/Logo.vue';
import { postJson } from '@/composables/useRpc';
import { useBusy } from '@/composables/useActions';
import { useI18n } from '@/composables/useI18n';

const { t } = useI18n();

const step = ref<'password' | 'code'>('password');
const email = ref('');
const password = ref('');
const code = ref('');
const error = ref<string | null>(null);
const { busy, run } = useBusy();
const codeInput = ref<InstanceType<typeof Input> | null>(null);

async function submit() {
    error.value = null;
    await run(async () => {
        if (step.value === 'password') {
            const { status, json } = await postJson('/login/password', { email: email.value, password: password.value });
            if (status === 200 && json.step === 'code') {
                step.value = 'code';
                await nextTick();
                (document.getElementById('code') as HTMLInputElement | null)?.focus();
            } else error.value = json.error ?? json.message ?? t('Sign in failed.');
        } else {
            const { status, json } = await postJson('/login/code', { code: code.value });
            if (status === 200 && json.redirect) window.location.href = new URL(json.redirect, location.origin).pathname;
            else {
                error.value = json.error ?? t('Sign in failed.');
                if (json.step === 'password') step.value = 'password';
                code.value = '';
            }
        }
    });
}
</script>

<template>
    <Head :title="t('Sign in')" />
    <div class="bg-sidebar text-sidebar-foreground relative grid min-h-svh place-items-center overflow-hidden p-4">
        <div class="pointer-events-none absolute inset-0 opacity-60 [background:radial-gradient(60rem_30rem_at_50%_-10%,oklch(0.55_0.2_262/.35),transparent)]" aria-hidden="true" />
        <div class="relative w-full max-w-sm">
            <div class="mb-6 flex items-center justify-center gap-3">
                <Logo class="size-10" />
                <div class="leading-tight"><p class="text-lg font-semibold">MyGameHost</p><p class="text-sidebar-muted text-sm">{{ t('Hosting Panel') }}</p></div>
            </div>
            <Card class="text-card-foreground">
                <CardHeader>
                    <CardTitle class="text-xl">{{ t('Sign in') }}</CardTitle>
                    <CardDescription>{{ step === 'code' ? t('Enter the 6-digit code from your authenticator app.') : t('Hosting Panel administrator') }}</CardDescription>
                </CardHeader>
                <CardContent>
                    <form class="flex flex-col gap-4" @submit.prevent="submit">
                        <Alert v-if="error" variant="destructive" role="alert"><AlertDescription>{{ error }}</AlertDescription></Alert>
                        <FieldGroup v-if="step === 'password'">
                            <Field>
                                <FieldLabel for="email">{{ t('Email') }}</FieldLabel>
                                <Input id="email" v-model="email" type="email" autocomplete="username" required autofocus />
                            </Field>
                            <Field>
                                <FieldLabel for="password">{{ t('Password') }}</FieldLabel>
                                <Input id="password" v-model="password" type="password" autocomplete="current-password" required />
                            </Field>
                        </FieldGroup>
                        <Field v-else>
                            <FieldLabel for="code">{{ t('Code') }}</FieldLabel>
                            <Input id="code" v-model="code" inputmode="numeric" autocomplete="one-time-code" maxlength="7" required class="text-center text-xl tracking-widest" />
                        </Field>
                        <Button type="submit" :disabled="busy"><Spinner v-if="busy" />{{ step === 'code' ? t('Sign in') : t('Continue') }}</Button>
                        <Button v-if="step === 'code'" type="button" variant="ghost" @click="step = 'password'; error = null">{{ t('Back') }}</Button>
                    </form>
                </CardContent>
            </Card>
        </div>
    </div>
</template>
